// Package board — доменная модель доски и применение операций над ней.
package board

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/konkov/agile/internal/rank"
)

var ErrNotFound = errors.New("доска не найдена")

type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
}

type Column struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
	WIPLimit *int   `json:"wipLimit"`
}

type Card struct {
	ID          string `json:"id"`
	ColumnID    string `json:"columnId"`
	Position    string `json:"position"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
}

// Snapshot — полный слепок доски. На нашем масштабе доска отдаётся одним
// ответом: колонка в сотни карточек — организационная патология, а не
// сценарий, ради которого стоит усложнять клиент постраничной загрузкой.
type Snapshot struct {
	Board   Info     `json:"board"`
	Columns []Column `json:"columns"`
	Cards   []Card   `json:"cards"`
}

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// List возвращает доски организации.
func (s *Service) List(ctx context.Context, orgID string) ([]Info, error) {
	rows, err := s.pool.Query(ctx, `
		select id, name, version
		  from boards
		 where org_id = $1 and archived_at is null
		 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Info{}
	for rows.Next() {
		var b Info
		if err := rows.Scan(&b.ID, &b.Name, &b.Version); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Create заводит доску с тремя колонками потока по умолчанию.
func (s *Service) Create(ctx context.Context, orgID, name string) (Info, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Info{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // откат после успешного Commit безвреден

	var projectID string
	err = tx.QueryRow(ctx, `
		select id from projects
		 where org_id = $1 and archived_at is null
		 order by created_at limit 1`, orgID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx,
			`insert into projects (org_id, name) values ($1, $2) returning id`,
			orgID, "Проекты").Scan(&projectID)
	}
	if err != nil {
		return Info{}, err
	}

	var b Info
	err = tx.QueryRow(ctx, `
		insert into boards (org_id, project_id, name) values ($1, $2, $3)
		returning id, name, version`, orgID, projectID, name).
		Scan(&b.ID, &b.Name, &b.Version)
	if err != nil {
		return Info{}, err
	}

	defaults := []string{"Очередь", "В работе", "Готово"}
	positions, err := rank.NBetween("", "", len(defaults))
	if err != nil {
		return Info{}, err
	}
	for i, name := range defaults {
		_, err = tx.Exec(ctx, `
			insert into board_columns (org_id, board_id, name, position)
			values ($1, $2, $3, $4)`, orgID, b.ID, name, positions[i])
		if err != nil {
			return Info{}, err
		}
	}

	return b, tx.Commit(ctx)
}

// Snapshot читает доску целиком вместе с её версией.
func (s *Service) Snapshot(ctx context.Context, orgID, boardID string) (Snapshot, error) {
	var snap Snapshot
	err := s.pool.QueryRow(ctx, `
		select id, name, version from boards
		 where id = $1 and org_id = $2 and archived_at is null`, boardID, orgID).
		Scan(&snap.Board.ID, &snap.Board.Name, &snap.Board.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return snap, ErrNotFound
	}
	if err != nil {
		return snap, err
	}

	colRows, err := s.pool.Query(ctx, `
		select id, name, position, wip_limit
		  from board_columns
		 where board_id = $1 and archived_at is null
		 order by position`, boardID)
	if err != nil {
		return snap, err
	}
	defer colRows.Close()
	snap.Columns = []Column{}
	for colRows.Next() {
		var c Column
		if err := colRows.Scan(&c.ID, &c.Name, &c.Position, &c.WIPLimit); err != nil {
			return snap, err
		}
		snap.Columns = append(snap.Columns, c)
	}
	if err := colRows.Err(); err != nil {
		return snap, err
	}

	cardRows, err := s.pool.Query(ctx, `
		select id, column_id, position, title, description, version
		  from cards
		 where board_id = $1 and archived_at is null
		 order by column_id, position`, boardID)
	if err != nil {
		return snap, err
	}
	defer cardRows.Close()
	snap.Cards = []Card{}
	for cardRows.Next() {
		var c Card
		if err := cardRows.Scan(&c.ID, &c.ColumnID, &c.Position, &c.Title, &c.Description, &c.Version); err != nil {
			return snap, err
		}
		snap.Cards = append(snap.Cards, c)
	}
	return snap, cardRows.Err()
}

// CardOrder — текущий порядок колонки, который возвращается клиенту
// вместе с ответом 409, чтобы он пересобрался без полной перезагрузки доски.
type CardOrder struct {
	ID       string `json:"id"`
	Position string `json:"position"`
}

// Placement — куда положить карточку в колонке.
//
// Клиент присылает намерение, а не вычисленную позицию: если опорная карточка
// к моменту обработки сдвинулась, посчитанный клиентом ключ был бы неверным.
// Место указывается явно, без умолчаний «по отсутствию поля» — из-за них
// одна и та же запись означала бы «в начало» в одной операции и «в конец»
// в другой.
type Placement struct {
	// Place: start — в начало колонки, end — в конец, after — сразу
	// за карточкой AfterCardID. Пустое значение читается как end.
	Place       string  `json:"place"`
	AfterCardID *string `json:"afterCardId"`
}

const (
	PlaceStart = "start"
	PlaceEnd   = "end"
	PlaceAfter = "after"
)

// neighbours находит соседей будущей позиции карточки.
//
// exclude пуст при создании карточки (исключать нечего) и содержит id при
// перемещении: соседа ищем среди остальных карточек, иначе перемещаемая
// карточка окажется сама себе границей.
func neighbours(ctx context.Context, tx pgx.Tx, columnID string, pl Placement, exclude string) (prev, next string, err error) {
	var excludeID *string
	if exclude != "" {
		excludeID = &exclude
	}

	place := pl.Place
	if place == "" {
		place = PlaceEnd
	}

	switch place {
	case PlaceEnd:
		err = tx.QueryRow(ctx, `
			select position from cards
			 where column_id = $1 and archived_at is null
			   and ($2::uuid is null or id <> $2::uuid)
			 order by position desc limit 1`, columnID, excludeID).Scan(&prev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
		return prev, "", nil

	case PlaceStart:
		err = tx.QueryRow(ctx, `
			select position from cards
			 where column_id = $1 and archived_at is null
			   and ($2::uuid is null or id <> $2::uuid)
			 order by position limit 1`, columnID, excludeID).Scan(&next)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
		return "", next, nil

	case PlaceAfter:
		if pl.AfterCardID == nil {
			return "", "", badRequestf("для place = %q нужен afterCardId", PlaceAfter)
		}
		err = tx.QueryRow(ctx, `
			select position from cards
			 where id = $1 and column_id = $2 and archived_at is null`,
			*pl.AfterCardID, columnID).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", conflictf(columnID, "карточка, после которой вставляем, уже перемещена или удалена")
		}
		if err != nil {
			return "", "", err
		}
		err = tx.QueryRow(ctx, `
			select position from cards
			 where column_id = $1 and archived_at is null
			   and ($2::uuid is null or id <> $2::uuid)
			   and position > $3
			 order by position limit 1`, columnID, excludeID, prev).Scan(&next)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
		return prev, next, nil

	default:
		return "", "", badRequestf("недопустимое значение place: %q, ожидалось start, end или after", pl.Place)
	}
}

func nextPosition(ctx context.Context, tx pgx.Tx, columnID string, pl Placement, exclude string) (string, error) {
	prev, next, err := neighbours(ctx, tx, columnID, pl, exclude)
	if err != nil {
		return "", err
	}
	pos, err := rank.Between(prev, next)
	if err != nil {
		return "", fmt.Errorf("вычисление позиции: %w", err)
	}
	return pos, nil
}
