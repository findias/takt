package report

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Именованные срезы (этап 24.2): отбор «Отчётов» под именем, свой
// у каждого. Хранится строкой запроса экрана — той, что стоит в адресе,
// с периодом словом или датами (миграция 0067).

var (
	// ErrSliceExists — срез с таким названием у этого человека уже есть.
	ErrSliceExists = errors.New("срез с таким названием уже есть — выберите другое")
	// ErrSliceNotFound — среза нет или он чужой: чужие неотличимы
	// от несуществующих, их не видно вовсе.
	ErrSliceNotFound = errors.New("такого среза нет — возможно, его уже удалили")
)

type Slice struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

func (s *Service) Slices(ctx context.Context, orgID, userID string) ([]Slice, error) {
	out := []Slice{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, name, query from report_slices order by lower(name)`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Slice])
		return err
	})
	return out, err
}

func (s *Service) SaveSlice(ctx context.Context, orgID, userID, name, query string) (Slice, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Slice{}, &BadFilter{"у среза должно быть название"}
	}
	if len([]rune(name)) > 200 {
		return Slice{}, &BadFilter{"название среза длиннее 200 знаков — сократите его"}
	}
	// Ведущий вопросительный знак — деталь адреса, а не отбора: с ним
	// одинаковые срезы различались бы одним знаком.
	query = strings.TrimPrefix(strings.TrimSpace(query), "?")
	if len(query) > 8000 {
		return Slice{}, &BadFilter{"отбор среза слишком длинный — выберите меньше досок и людей"}
	}
	v := Slice{Name: name, Query: query}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			insert into report_slices (org_id, user_id, name, query)
			values ($1, $2, $3, $4)
			returning id`, orgID, userID, name, query).Scan(&v.ID)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "report_slices_name_idx" {
		return Slice{}, ErrSliceExists
	}
	return v, err
}

func (s *Service) DeleteSlice(ctx context.Context, orgID, userID, id string) error {
	return s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from report_slices where id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrSliceNotFound
		}
		return nil
	})
}
