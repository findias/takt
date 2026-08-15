package board

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Сохранённые виды доски.
//
// Вид — это сохранённая ссылка: строка запроса, в которой уже лежат
// фильтры и группировка. Разбирать её на колонки в базе значило бы
// менять схему при каждом новом фильтре, а список фильтров будет расти.
//
// Вид принадлежит человеку и доске: «моё на неделе» у каждого своё,
// а фильтр по колонкам одной доски на другой бессмыслен.

// ErrViewExists — вид с таким названием у этого человека на этой доске
// уже есть. Два одинаковых названия — опечатка, а не замысел.
var ErrViewExists = errors.New("вид с таким названием уже есть")

type View struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

func (s *Service) Views(ctx context.Context, orgID, userID, boardID string) ([]View, error) {
	out := []View{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, name, query from board_views
			 where board_id = $1
			 order by lower(name)`, boardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v View
			if err := rows.Scan(&v.ID, &v.Name, &v.Query); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) SaveView(ctx context.Context, orgID, userID, boardID, name, query string) (View, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return View{}, badRequestf("у вида должно быть название")
	}
	// Ведущий вопросительный знак — деталь адреса, а не условия: хранить
	// его значит получить два вида, отличающихся только им.
	query = strings.TrimPrefix(strings.TrimSpace(query), "?")

	v := View{Name: name, Query: query}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		// Доска должна быть видна: сохранять вид на чужую доску незачем,
		// а отказ политики выглядел бы внутренней ошибкой.
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from boards
			                where id = $1 and archived_at is null)`, boardID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return tx.QueryRow(ctx, `
			insert into board_views (org_id, board_id, user_id, name, query)
			values ($1, $2, $3, $4, $5)
			returning id, name, query`,
			orgID, boardID, userID, name, query).Scan(&v.ID, &v.Name, &v.Query)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "board_views_name_idx" {
		return View{}, ErrViewExists
	}
	return v, err
}

func (s *Service) DeleteView(ctx context.Context, orgID, userID, viewID string) error {
	return s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from board_views where id = $1`, viewID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}
