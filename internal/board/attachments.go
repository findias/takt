package board

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Вложения к карточке (ROADMAP 36.1): небольшие файлы — снимок экрана,
// документ, таблица. Лежат в базе (см. миграцию 0079); предел одного
// файла и общий предел карточки задаёт установка, сервис получает их
// готовыми числами и не знает, откуда они.

type Attachment struct {
	ID        string    `json:"id"`
	CardID    string    `json:"cardId"`
	Name      string    `json:"name"`
	MediaType string    `json:"mediaType"`
	Size      int64     `json:"size"`
	Author    *string   `json:"author"`
	AuthorID  *string   `json:"authorId"`
	CreatedAt time.Time `json:"createdAt"`
	// Deletable — может ли спрашивающий убрать файл: тот, кто приложил,
	// или владелец организации. Клиент не показывает кнопку, ведущую в отказ.
	Deletable bool `json:"deletable"`
	// Body едет только при скачивании; в списке его нет.
	Body []byte `json:"-"`
}

var (
	// ErrAttachmentType — файлы вне списка разрешённых. Список — по
	// расширению, а не по заголовку из браузера: заголовок присылает
	// клиент, и ему веры столько же, сколько самому файлу.
	ErrAttachmentType = errors.New(
		"файлы такого вида не прикладываются: подойдут изображения, PDF, текст и документы офисных форматов")
	ErrAttachmentNotFound = errors.New("вложение не найдено")
	ErrNotAttachmentOwner = errors.New("убрать вложение может тот, кто его приложил, или владелец организации")
)

// AttachmentTooBig — файл больше предела на один файл.
type AttachmentTooBig struct{ Limit int64 }

func (e AttachmentTooBig) Error() string {
	return fmt.Sprintf("файл больше предела в %s на одно вложение", HumanBytes(e.Limit))
}

// AttachmentsFull — к карточке уже приложено столько, что новый файл
// не помещается в общий предел.
type AttachmentsFull struct{ Used, Limit int64 }

func (e AttachmentsFull) Error() string {
	return fmt.Sprintf("к карточке уже приложено %s из %s — новый файл не поместится, уберите лишнее",
		HumanBytes(e.Used), HumanBytes(e.Limit))
}

// HumanBytes — размер словами для отказов и подсказок: «5 МБ», «512 КБ».
// Целые мегабайты без дроби, остальное — с одной.
func HumanBytes(n int64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d МБ", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f МБ", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d КБ", (n+512)>>10)
	default:
		return fmt.Sprintf("%d Б", n)
	}
}

// attachmentTypes — что прикладывается, по расширению. Исполняемых,
// скриптов и архивов нет намеренно: доска — не файлообменник, а то, что
// скачивается и запускается, не должно приезжать из карточки.
var attachmentTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".bmp": "image/bmp",
	".pdf": "application/pdf",
	".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8",
	".csv": "text/csv; charset=utf-8", ".log": "text/plain; charset=utf-8",
	".json": "application/json", ".xml": "text/xml; charset=utf-8",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".rtf":  "application/rtf",
}

// AttachmentExtensions — список для подсказки и для `accept` у поля
// выбора файла, в постоянном порядке.
func AttachmentExtensions() []string {
	out := make([]string, 0, len(attachmentTypes))
	for ext := range attachmentTypes {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// attachmentName чистит имя файла: только последний сегмент пути —
// браузеры путь не присылают, а подделанный «../» не должен попасть
// ни в имя, ни в заголовок ответа; без управляющих знаков и кавычек.
func attachmentName(raw string) (name, mediaType string, err error) {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		return "", "", badRequestf("у файла нет имени")
	}
	mediaType, ok := attachmentTypes[strings.ToLower(path.Ext(name))]
	if !ok {
		return "", "", ErrAttachmentType
	}
	return name, mediaType, nil
}

// Attachments отдаёт список вложений карточки, от старых к новым, без тел.
func (s *Service) Attachments(ctx context.Context, orgID, userID, cardID string) ([]Attachment, error) {
	out := []Attachment{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select a.id, a.card_id, a.name, a.media_type, a.size, a.author_id, u.name, a.created_at,
			       a.author_id = $2 or exists (select 1 from memberships m
			                                    where m.org_id = $3 and m.user_id = $2 and m.role = 'owner')
			  from card_attachments a
			  left join users u on u.id = a.author_id
			 where a.card_id = $1
			 order by a.created_at, a.id`, cardID, userID, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Attachment
			if err := rows.Scan(&a.ID, &a.CardID, &a.Name, &a.MediaType, &a.Size, &a.AuthorID, &a.Author,
				&a.CreatedAt, &a.Deletable); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// Attach прикладывает файл к карточке. Пределы — числами от вызывающего:
// на один файл и на карточку целиком. Отказ называет оба числа —
// человеку решать, что убрать.
func (s *Service) Attach(ctx context.Context, orgID, actorID, boardID, cardID, rawName string,
	body []byte, maxFile, maxCard int64,
) (Attachment, error) {
	name, mediaType, err := attachmentName(rawName)
	if err != nil {
		return Attachment{}, err
	}
	if len(body) == 0 {
		return Attachment{}, badRequestf("файл пуст")
	}
	if int64(len(body)) > maxFile {
		return Attachment{}, AttachmentTooBig{Limit: maxFile}
	}
	var a Attachment
	err = s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Карточка должна быть на этой доске, как и у обсуждения: иначе
		// файл можно приписать к чужой карточке по идентификатору.
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from cards
			                where id = $1 and board_id = $2 and archived_at is null)`,
			cardID, boardID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var used int64
		if err := tx.QueryRow(ctx,
			`select coalesce(sum(size), 0) from card_attachments where card_id = $1`, cardID).
			Scan(&used); err != nil {
			return err
		}
		if used+int64(len(body)) > maxCard {
			return AttachmentsFull{Used: used, Limit: maxCard}
		}
		err := tx.QueryRow(ctx, `
			insert into card_attachments (org_id, board_id, card_id, author_id, name, media_type, size, body)
			values ($1, $2, $3, $4, $5, $6, $7, $8)
			returning id, card_id, name, media_type, size, author_id, created_at`,
			orgID, boardID, cardID, actorID, name, mediaType, len(body), body).
			Scan(&a.ID, &a.CardID, &a.Name, &a.MediaType, &a.Size, &a.AuthorID, &a.CreatedAt)
		if err != nil {
			return err
		}
		a.Deletable = true
		return logEvent(ctx, tx, orgID, boardID, cardID, actorID, "attached",
			nil, nil, map[string]any{"attachmentId": a.ID, "name": a.Name, "size": a.Size})
	})
	return a, translateComment(err)
}

// Attachment читает вложение целиком — для скачивания. Недоступная доска
// и чужое вложение отвечают одинаково: существование недоступного мы
// не подтверждаем.
func (s *Service) Attachment(ctx context.Context, orgID, userID, attachmentID string) (Attachment, error) {
	var a Attachment
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			select id, card_id, name, media_type, size, author_id, created_at, body
			  from card_attachments where id = $1`, attachmentID).
			Scan(&a.ID, &a.CardID, &a.Name, &a.MediaType, &a.Size, &a.AuthorID, &a.CreatedAt, &a.Body)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAttachmentNotFound
		}
		return err
	})
	return a, err
}

// Detach убирает вложение. Может тот, кто приложил, и владелец
// организации: файл висит под чужой работой, и убрать его у коллеги —
// то же, что стереть его реплику.
func (s *Service) Detach(ctx context.Context, orgID, actorID, attachmentID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var boardID, cardID, name string
		var author *string
		err := tx.QueryRow(ctx, `
			select board_id, card_id, name, author_id from card_attachments where id = $1 for update`,
			attachmentID).Scan(&boardID, &cardID, &name, &author)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAttachmentNotFound
		}
		if err != nil {
			return err
		}
		if author == nil || *author != actorID {
			var owner bool
			if err := tx.QueryRow(ctx, `
				select exists (select 1 from memberships
				                where org_id = $1 and user_id = $2 and role = 'owner')`,
				orgID, actorID).Scan(&owner); err != nil {
				return err
			}
			if !owner {
				return ErrNotAttachmentOwner
			}
		}
		if _, err := tx.Exec(ctx, `delete from card_attachments where id = $1`, attachmentID); err != nil {
			return err
		}
		return logEvent(ctx, tx, orgID, boardID, cardID, actorID, "detached",
			nil, nil, map[string]any{"attachmentId": attachmentID, "name": name})
	})
}
