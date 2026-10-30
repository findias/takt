package idea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/findias/takt/internal/i18n"
)

// Решение по идее словами (ROADMAP 43, срез 2). Четыре исхода, записи
// только добавляются (0084): последняя — нынешнее решение, остальные —
// история, и ошибку исправляют новой записью.

const (
	// Taken — берём: из идеи появилась карточка на доске.
	Taken = "taken"
	// Declined — не берём, и почему: без причины отказ не принимается.
	Declined = "declined"
	// Duplicate — уже есть: голоса этой идеи считаются у той.
	Duplicate = "duplicate"
	// Later — позже: видели, вернёмся.
	Later = "later"
)

// Kinds — все исходы, в порядке от решительного к отложенному.
var Kinds = []string{Taken, Declined, Duplicate, Later}

// MaxNote — предел пояснения, как у статуса эпика.
const MaxNote = 2000

var (
	ErrKindUnknown    = errors.New("решение — одно из: taken, declined, duplicate, later")
	ErrNoteLong       = fmt.Errorf("пояснение к решению — не длиннее %d знаков", MaxNote)
	ErrReasonRequired = errors.New("скажите, почему не берём: автору нужен ответ")
	ErrBoardRequired  = errors.New("выберите доску, на которой появится карточка")
	ErrBoardReadOnly  = errors.New("на эту доску вы не пишете — выберите другую")
	ErrBoardEmpty     = errors.New("на доске нет колонок — карточку поставить некуда")
	ErrTargetRequired = errors.New("укажите, какая идея уже есть")
	ErrTargetSelf     = errors.New("идея не может быть двойником самой себя")
	ErrCannotDecide   = errors.New("решать идею может владелец организации или владелец подразделения, которому она адресована")
)

// Decision — запись решения.
type Decision struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Note string `json:"note"`
	// Author пуст, если решавшего стёрли.
	AuthorID *string `json:"authorId"`
	Author   *string `json:"author"`
	// At — когда сказано; имя короче created_at, как у статуса эпика.
	At time.Time `json:"at"`
	// Card — карточка, в которую идея превратилась («берём»). Пусто,
	// если карточку убрали или её доска спрашивающему не видна.
	Card *CardRef `json:"card,omitempty"`
	// Into — идея, в которую влились голоса этой («уже есть»).
	Into *IdeaRef `json:"into,omitempty"`
}

// CardRef — карточка из идеи и где она сейчас.
type CardRef struct {
	ID         string `json:"id"`
	BoardID    string `json:"boardId"`
	Number     string `json:"number"`
	BoardName  string `json:"boardName"`
	Column     string `json:"column"`
	ColumnKind string `json:"columnKind"`
	Done       bool   `json:"done"`
}

// IdeaRef — ссылка на другую идею.
type IdeaRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DecideRequest — что решили. BoardID нужен «берём», IntoIdeaID —
// «уже есть», Note обязателен «не берём».
type DecideRequest struct {
	Kind       string `json:"decision"`
	Note       string `json:"note"`
	BoardID    string `json:"boardId"`
	IntoIdeaID string `json:"intoIdeaId"`
}

// CardMaker заводит карточку на доске от имени человека — это делает
// пакет board своей операцией, со своей блокировкой доски, лимитами
// и событиями; здесь только просят.
type CardMaker interface {
	CreateCard(ctx context.Context, orgID, actorID, boardID, columnID, title, description string) (string, error)
}

// Decisions — история решений по идее, от нового к старому. Идея,
// которой нет или которая не видна, — «не найдено», а не пустая история.
func (s *Service) Decisions(ctx context.Context, orgID, userID, ideaID string) ([]Decision, error) {
	out := []Decision{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from ideas where id = $1)`, ideaID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `
			select d.id, d.decision, d.note, d.author_id, u.name, d.created_at,
			       c.id, c.board_id, c.number, cb.name, cc.name, cc.kind,
			       c.id is not null and (c.outcome is not null or c.done_at is not null),
			       di.id, di.title
			  from idea_decisions d
			  left join users u on u.id = d.author_id
			  left join cards c on c.id = d.card_id and c.archived_at is null
			  left join boards cb on cb.id = c.board_id
			  left join board_columns cc on cc.id = c.column_id
			  left join ideas di on di.id = d.into_idea_id
			 where d.idea_id = $1
			 order by d.created_at desc, d.id desc`, ideaID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Decision
			var cID, cBoard, cNumber, cBoardName, cColumn, cKind, intoID, intoTitle *string
			var cDone bool
			if err := rows.Scan(&d.ID, &d.Kind, &d.Note, &d.AuthorID, &d.Author, &d.At,
				&cID, &cBoard, &cNumber, &cBoardName, &cColumn, &cKind, &cDone, &intoID, &intoTitle); err != nil {
				return err
			}
			if cID != nil {
				d.Card = &CardRef{ID: *cID, BoardID: *cBoard, Number: *cNumber, BoardName: *cBoardName,
					Column: *cColumn, ColumnKind: *cKind, Done: cDone}
			}
			if intoID != nil {
				d.Into = &IdeaRef{ID: *intoID, Title: *intoTitle}
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// Decide добавляет запись решения. Кто вправе — проверяется словами
// до работы и политикой базы при записи (0084): владелец организации
// или владелец подразделения, которому идея адресована.
//
// «Берём» заводит карточку до записи решения, отдельной операцией
// доски: не вышло — решения нет. Обратное — решение есть, карточки
// нет — хуже: автор прочтёт «берём» и не найдёт, куда делось.
func (s *Service) Decide(ctx context.Context, orgID, actorID, ideaID string, req DecideRequest, cards CardMaker) (Idea, error) {
	kind := strings.TrimSpace(req.Kind)
	known := false
	for _, k := range Kinds {
		known = known || k == kind
	}
	if !known {
		return Idea{}, ErrKindUnknown
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > MaxNote {
		return Idea{}, ErrNoteLong
	}
	boardID, into := strings.TrimSpace(req.BoardID), strings.TrimSpace(req.IntoIdeaID)
	switch kind {
	case Declined:
		if note == "" {
			return Idea{}, ErrReasonRequired
		}
	case Taken:
		if boardID == "" {
			return Idea{}, ErrBoardRequired
		}
	case Duplicate:
		if into == "" {
			return Idea{}, ErrTargetRequired
		}
		if into == ideaID {
			return Idea{}, ErrTargetSelf
		}
	}

	// Проверки — до карточки: заведённая карточка при отказе в записи
	// решения осталась бы висеть на доске без идеи.
	var title, body, columnID string
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		i, err := one(ctx, tx, actorID, ideaID)
		if err != nil {
			return err
		}
		if !i.CanDecide {
			return ErrCannotDecide
		}
		title, body = i.Title, i.Body
		switch kind {
		case Taken:
			var writable bool
			if err := tx.QueryRow(ctx, `select $1 = any (app_writable_boards())`, boardID).Scan(&writable); err != nil {
				return err
			}
			if !writable {
				return ErrBoardReadOnly
			}
			err := tx.QueryRow(ctx, `
				select id from board_columns
				 where board_id = $1 and archived_at is null
				 order by position limit 1`, boardID).Scan(&columnID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrBoardEmpty
			}
			return err
		case Duplicate:
			if _, err := one(ctx, tx, actorID, into); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Idea{}, err
	}

	var cardID *string
	if kind == Taken {
		// Карточка получает описание идеи и строку, откуда она: автор
		// идеи найдёт её по словам, а команда — поймёт, зачем она.
		origin := i18n.Name(ctx, fmt.Sprintf("Из идеи «%s»", title))
		description := strings.TrimSpace(body + "\n\n" + origin)
		id, err := cards.CreateCard(ctx, orgID, actorID, boardID, columnID, title, description)
		if err != nil {
			return Idea{}, err
		}
		cardID = &id
	}

	var out Idea
	err = s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var intoID *string
		if kind == Duplicate {
			intoID = &into
		}
		_, err := tx.Exec(ctx, `
			insert into idea_decisions (org_id, idea_id, author_id, decision, note, card_id, into_idea_id)
			values ($1, $2, $3, $4, $5, $6, $7)`,
			orgID, ideaID, actorID, kind, note, cardID, intoID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42501" {
				return ErrCannotDecide
			}
			return err
		}
		out, err = one(ctx, tx, actorID, ideaID)
		return err
	})
	return out, err
}
