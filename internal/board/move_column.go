package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/rank"
)

// Перенос колонки (ROADMAP 34.12, замечание владельца со стенда).
//
// Колонку можно было завести и переименовать, но не переставить:
// забытая «Проверка» между «В работе» и «Готово» заводилась в конец,
// и оставалось только завести все заново. Карточки принадлежат колонке,
// а не месту на доске, поэтому едут вместе с ней сами — их трогать
// незачем, и их история не меняется: переход между колонками остаётся
// тем же переходом, как бы колонки ни стояли.
//
// Место задаётся тем же способом, что у карточки: в начало, в конец или
// сразу за другой колонкой. Одна новая позиция между соседями — никакой
// перенумерации остальных.

type moveColumnPayload struct {
	ColumnID      string  `json:"columnId"`
	Place         string  `json:"place"`
	AfterColumnID *string `json:"afterColumnId"`
}

func moveColumn(ctx context.Context, tx pgx.Tx, _, boardID string, raw json.RawMessage) (Patch, error) {
	var p moveColumnPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор MOVE_COLUMN: %v", err)
	}
	if _, err := loadColumn(ctx, tx, boardID, p.ColumnID); err != nil {
		return Patch{}, err
	}

	// Соседи — среди остальных колонок: своя позиция места не занимает.
	var prev, next string
	switch p.Place {
	case "start":
		err := tx.QueryRow(ctx, `
			select position from board_columns
			 where board_id = $1 and archived_at is null and id <> $2
			 order by position limit 1`, boardID, p.ColumnID).Scan(&next)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, err
		}
	case "", "end":
		err := tx.QueryRow(ctx, `
			select position from board_columns
			 where board_id = $1 and archived_at is null and id <> $2
			 order by position desc limit 1`, boardID, p.ColumnID).Scan(&prev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, err
		}
	case "after":
		if p.AfterColumnID == nil || *p.AfterColumnID == p.ColumnID {
			return Patch{}, badRequestf("для place=after нужна другая колонка в afterColumnId")
		}
		err := tx.QueryRow(ctx, `
			select position from board_columns
			 where id = $1 and board_id = $2 and archived_at is null`,
			*p.AfterColumnID, boardID).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, conflictf("", "колонка, после которой ставим, уже удалена")
		}
		if err != nil {
			return Patch{}, err
		}
		err = tx.QueryRow(ctx, `
			select position from board_columns
			 where board_id = $1 and archived_at is null and id <> $2 and position > $3
			 order by position limit 1`, boardID, p.ColumnID, prev).Scan(&next)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, err
		}
	default:
		return Patch{}, badRequestf("place колонки: start, end или after, а не %q", p.Place)
	}

	pos, err := rank.Between(prev, next)
	if err != nil {
		return Patch{}, fmt.Errorf("вычисление позиции колонки: %w", err)
	}
	c, err := scanColumn(tx.QueryRow(ctx, `
		update board_columns set position = $2
		 where id = $1
		 returning `+columnFields, p.ColumnID, pos))
	if err != nil {
		return Patch{}, err
	}
	return Patch{Columns: []Column{c}}, nil
}
