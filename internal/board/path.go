package board

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Путь карточки до корня дерева (этап 32.2): «Эпик › Фича» над
// названием в панели. Панель знает только прямого родителя, а эпик
// над ним может лежать на другой доске, куда снимок не заглядывает.

// PathStep — звено пути. Недоступное звено не пропадает: оно названо
// «недоступной карточкой» на экране, а здесь у него только идентификатор.
// Выше недоступного подъём не идёт — связи над ним не видны, и это
// честный конец пути, а не обрыв.
type PathStep struct {
	ID        string `json:"id"`
	Visible   bool   `json:"visible"`
	Number    string `json:"number,omitempty"`
	Title     string `json:"title,omitempty"`
	BoardID   string `json:"boardId,omitempty"`
	BoardName string `json:"boardName,omitempty"`
}

// CardPath — предки карточки от корня к родителю. Пусто — карточка
// сама корень. Невидимая карточка — ErrNotFound: существование
// недоступного не подтверждается.
func (s *Service) CardPath(ctx context.Context, orgID, userID, cardID string) ([]PathStep, error) {
	out := []PathStep{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var seen bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from cards where id = $1)`, cardID).
			Scan(&seen); err != nil {
			return err
		}
		if !seen {
			return ErrNotFound
		}
		// Один рекурсивный запрос с пределом глубины: цикл в связях,
		// если он возник, не уведёт подъём в бесконечность.
		rows, err := tx.Query(ctx, `
			with recursive up(card, depth) as (
				select from_card, 1 from card_links where to_card = $1 and kind = 'subtask'
				union all
				select l.from_card, u.depth + 1
				  from up u join card_links l on l.to_card = u.card and l.kind = 'subtask'
				 where u.depth < $2
			)
			select u.card, c.id is not null, coalesce(c.number, ''), coalesce(c.title, ''),
			       coalesce(b.id::text, ''), coalesce(b.name, '')
			  from up u
			  left join cards c on c.id = u.card
			  left join boards b on b.id = c.board_id
			 order by u.depth desc`, cardID, MaxSubtaskDepth)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PathStep])
		return err
	})
	return out, err
}
