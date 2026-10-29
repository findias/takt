package board

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Статус эпика словами (ROADMAP 42.3, срез 10): «идёт», «под угрозой»,
// «сорван» — с автором, датой и пояснением, и с историей. Полоса и
// прогноз считаются из частей; статус — то, что об эпике говорит
// ведущий его человек, и из чисел этого не вывести. Записи только
// добавляются (см. миграцию 0082): последняя — текущий статус,
// остальные — история.

const (
	StatusOnTrack  = "on_track"
	StatusAtRisk   = "at_risk"
	StatusOffTrack = "off_track"
)

// EpicStatusKinds — все статусы, в порядке от спокойного к тревожному.
// Список нужен контракту и клиенту: сервер отказывает по нему.
var EpicStatusKinds = []string{StatusOnTrack, StatusAtRisk, StatusOffTrack}

// MaxStatusNote — предел пояснения в знаках; тот же, что в проверке
// таблицы, но отказ отсюда называет число человеку, а не базе.
const MaxStatusNote = 2000

type EpicStatus struct {
	ID       string  `json:"id"`
	CardID   string  `json:"cardId"`
	Status   string  `json:"status"`
	Note     string  `json:"note"`
	Author   *string `json:"author"`
	AuthorID *string `json:"authorId"`
	// At — когда сказано. Имя короче created_at: в ответе рядом стоят
	// автор и слова, и «когда» читается как часть той же фразы.
	At time.Time `json:"at"`
}

var (
	// ErrNotEpic — статус словами есть только у карточки портфеля: у задачи
	// команды состояние говорит колонка, второго слова о том же не нужно.
	ErrNotEpic = errors.New("статус словами есть только у эпика на доске-портфеле")
	// ErrUnknownStatus — слово не из трёх. Отказ перечисляет допустимые.
	ErrUnknownStatus = errors.New("статус — одно из: on_track, at_risk, off_track")
)

// EpicStatuses — история статусов карточки, от нового к старому.
// Карточка на невидимой доске — «не найдено», а не пустая история:
// пустой список означал бы «статус ещё не ставили».
func (s *Service) EpicStatuses(ctx context.Context, orgID, userID, cardID string) ([]EpicStatus, error) {
	out := []EpicStatus{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from cards where id = $1)`, cardID).
			Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `
			select e.id, e.card_id, e.status, e.note, e.author_id, u.name, e.created_at
			  from epic_statuses e
			  left join users u on u.id = e.author_id
			 where e.card_id = $1
			 order by e.created_at desc, e.id desc`, cardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e EpicStatus
			if err := rows.Scan(&e.ID, &e.CardID, &e.Status, &e.Note, &e.AuthorID, &e.Author, &e.At); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// SetEpicStatus добавляет запись статуса. Кто вправе — решает политика
// доски: пишет тот, кому портфель открыт на запись, наблюдателю
// политика отказывает, и отказ переводится в «только для чтения».
func (s *Service) SetEpicStatus(ctx context.Context, orgID, actorID, boardID, cardID, status, note string) (EpicStatus, error) {
	status = strings.TrimSpace(status)
	known := false
	for _, k := range EpicStatusKinds {
		known = known || k == status
	}
	if !known {
		return EpicStatus{}, ErrUnknownStatus
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxStatusNote {
		return EpicStatus{}, badRequestf("пояснение к статусу — не длиннее %d знаков", MaxStatusNote)
	}
	var e EpicStatus
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Карточка должна быть на этой доске, и доска — портфелем:
		// иначе статус можно приписать чужой карточке по идентификатору
		// или повесить слово на задачу команды.
		var level string
		err := tx.QueryRow(ctx, `
			select b.level from cards c
			  join boards b on b.id = c.board_id
			 where c.id = $1 and c.board_id = $2 and c.archived_at is null`,
			cardID, boardID).Scan(&level)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if level != LevelPortfolio {
			return ErrNotEpic
		}
		err = tx.QueryRow(ctx, `
			insert into epic_statuses (org_id, board_id, card_id, author_id, status, note)
			values ($1, $2, $3, $4, $5, $6)
			returning id, card_id, status, note, author_id, created_at`,
			orgID, boardID, cardID, actorID, status, note).
			Scan(&e.ID, &e.CardID, &e.Status, &e.Note, &e.AuthorID, &e.At)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42501" {
				return ErrReadOnlyBoard
			}
			return err
		}
		if err := tx.QueryRow(ctx, `select name from users where id = $1`, actorID).Scan(&e.Author); err != nil &&
			!errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := logEvent(ctx, tx, orgID, boardID, cardID, actorID, "status_set",
			nil, nil, map[string]any{"status": status, "note": note}); err != nil {
			return err
		}
		// Версия доски двигается, как у реплики: статус лежит на карточке
		// портфеля, и открытые экраны обязаны его перечитать.
		return touchBoard(ctx, tx, boardID, actorID)
	})
	return e, err
}

// latestStatuses — текущий статус карточек портфеля: по доске или по
// одной карточке, как epics и teams, — снимку и патчу негде разойтись.
func latestStatuses(ctx context.Context, tx pgx.Tx, boardID, cardID *string) (map[string]*EpicStatus, error) {
	out := map[string]*EpicStatus{}
	rows, err := tx.Query(ctx, `
		select distinct on (e.card_id) e.id, e.card_id, e.status, e.note, e.author_id, u.name, e.created_at
		  from epic_statuses e
		  left join users u on u.id = e.author_id
		 where ($1::uuid is null or e.board_id = $1)
		   and ($2::uuid is null or e.card_id = $2)
		 order by e.card_id, e.created_at desc, e.id desc`, boardID, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e EpicStatus
		if err := rows.Scan(&e.ID, &e.CardID, &e.Status, &e.Note, &e.AuthorID, &e.Author, &e.At); err != nil {
			return nil, err
		}
		out[e.CardID] = &e
	}
	return out, rows.Err()
}
