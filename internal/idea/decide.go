package idea

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/webhook"
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

// ReasonDecided — повод уведомления «по идее решили» (0085): автору
// и поддержавшим, кроме самого решившего.
const ReasonDecided = "idea_decided"

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
	ErrRequestInvalid = errors.New("идентификатор запроса решения должен быть UUID")
	ErrRequestReused  = errors.New("этот идентификатор запроса уже использован для другого решения")
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
	// RequestID переживает потерю ответа. Пусто — клиент прежней версии.
	RequestID  string `json:"requestId,omitempty"`
	Kind       string `json:"decision"`
	Note       string `json:"note"`
	BoardID    string `json:"boardId"`
	IntoIdeaID string `json:"intoIdeaId"`
}

// CardMaker заводит карточку в транзакции решения — пакет board
// сохраняет свои блокировки, лимиты и события, но не фиксирует
// карточку раньше, чем удалось записать само решение.
type CardMaker interface {
	CreateCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID, columnID, title, description string) (string, error)
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
// Карточка, описание, решение и уведомления фиксируются вместе.
// Блокировка идеи сериализует решение и перенос; после ожидания
// перечитываются и права, и последнее решение. RequestID узнаёт даже
// поздний повтор после другого решения; клиенту возвращается нынешнее
// состояние без новой записи. Без ID прежний клиент узнаёт только
// идентичный текущему ответ.
func (s *Service) Decide(ctx context.Context, orgID, actorID, ideaID string, req DecideRequest, cards CardMaker) (Idea, error) {
	var requestID *string
	if raw := strings.TrimSpace(req.RequestID); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return Idea{}, ErrRequestInvalid
		}
		canonical := id.String()
		requestID = &canonical
	}
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

	// Неиспользуемые поля не меняют смысл команды. Отпечаток хранит
	// исходную доску: карточка позже может переехать или исчезнуть.
	normalized := DecideRequest{Kind: kind, Note: note}
	if kind == Taken {
		normalized.BoardID = boardID
	}
	if kind == Duplicate {
		normalized.IntoIdeaID = into
	}
	raw, _ := json.Marshal(normalized)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(raw))
	var out Idea
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		i, err := one(ctx, tx, actorID, ideaID)
		if err != nil {
			return err
		}
		if !i.CanDecide {
			return ErrCannotDecide
		}
		if err := lockIdea(ctx, tx, ideaID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrCannotDecide
			}
			return err
		}
		i, err = one(ctx, tx, actorID, ideaID)
		if err != nil {
			return err
		}
		if !i.CanDecide {
			return ErrCannotDecide
		}
		if requestID != nil {
			var previous string
			err := tx.QueryRow(ctx, `
				select request_fingerprint from idea_decisions
				 where idea_id = $1 and author_id = $2 and request_id = $3`,
				ideaID, actorID, *requestID).Scan(&previous)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return err
			case previous != fingerprint:
				return ErrRequestReused
			default:
				// Возвращаем нынешнее состояние: поздний ответ не должен
				// показывать на экране решение, уже заменённое другим.
				out = i
				return nil
			}
		}
		if requestID == nil && i.Decision != nil {
			var previous string
			if err := tx.QueryRow(ctx, `select request_fingerprint from idea_decisions where id = $1`, i.Decision.ID).Scan(&previous); err != nil {
				return err
			}
			if previous == fingerprint || (previous == "" && sameLegacyDecision(i.Decision, normalized)) {
				out = i
				return nil
			}
		}
		var columnID string
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
			if err != nil {
				return err
			}
		case Duplicate:
			if _, err := one(ctx, tx, actorID, into); err != nil {
				return err
			}
		}
		var cardID *string
		if kind == Taken {
			origin := i18n.Name(ctx, fmt.Sprintf("Из идеи «%s»", i.Title))
			description := strings.TrimSpace(i.Body + "\n\n" + origin)
			id, err := cards.CreateCard(ctx, tx, orgID, actorID, boardID, columnID, i.Title, description)
			if err != nil {
				return err
			}
			cardID = &id
		}
		var intoID *string
		if kind == Duplicate {
			intoID = &into
		}
		var decisionID string
		// now() относится к началу транзакции, ещё до ожидания замка.
		// История должна идти в порядке записанных решений, а не старта
		// запросов, иначе поздно получивший замок ответ останется прошлым.
		err = tx.QueryRow(ctx, `
			insert into idea_decisions (org_id, idea_id, author_id, decision, note, card_id, into_idea_id, request_fingerprint, request_id, created_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, clock_timestamp())
			returning id`,
			orgID, ideaID, actorID, kind, note, cardID, intoID, fingerprint, requestID).Scan(&decisionID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42501" {
				return ErrCannotDecide
			}
			return err
		}
		if err := notifyDecided(ctx, tx, orgID, actorID, ideaID, decisionID); err != nil {
			return err
		}
		out, err = one(ctx, tx, actorID, ideaID)
		if err != nil {
			return err
		}
		return webhook.Enqueue(ctx, tx, orgID, EventDecided, map[string]any{"idea": out.Public()})
	})
	return out, err
}

// У старой версии отпечатка нет. Пока ссылки сохранились, одинаковый
// текущий ответ узнаётся по прежним полям — обновление не должно
// само по себе создавать вторую карточку при первом повторе.
func sameLegacyDecision(d *Decision, req DecideRequest) bool {
	if d.Kind != req.Kind || d.Note != req.Note {
		return false
	}
	switch req.Kind {
	case Taken:
		return d.Card != nil && d.Card.BoardID == req.BoardID
	case Duplicate:
		return d.Into != nil && d.Into.ID == req.IntoIdeaID
	default:
		return true
	}
}

// notifyDecided пишет «по идее решили» автору и поддержавшим — в той же
// транзакции, что и решение, как уведомления доски (board/notify.go):
// не теряется при записанном решении и не появляется без него. Самому
// решившему и выключившим этот повод не пишется. Источник — запись
// решения: повтора у неё нет, уникальность таблицы — страховка.
func notifyDecided(ctx context.Context, tx pgx.Tx, orgID, actorID, ideaID, decisionID string) error {
	rows, err := tx.Query(ctx, `
		select u.id from users u
		 where u.id in (select author_id from ideas where id = $1
		                union select user_id from idea_votes where idea_id = $1)
		   and u.id <> $2::uuid
		   and not ($3 = any (u.muted_notifications))`,
		ideaID, actorID, ReasonDecided)
	if err != nil {
		return err
	}
	notified, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(notified) == 0 {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into notifications (org_id, recipient_id, idea_id, actor_id, reason, source)
		select $1, r, $2, $3, $4, $5 from unnest($6::uuid[]) r`,
		orgID, ideaID, actorID, ReasonDecided, "decision:"+decisionID, notified); err != nil {
		return err
	}
	return realtime.Notify(ctx, tx, realtime.Change{Notified: notified})
}
