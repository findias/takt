package board

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SearchLimit — сколько карточек отдаёт поиск по организации. Им
// выбирают одну карточку для связи, а не листают: двадцать строк
// хватает, чтобы дописать запрос, если нужной среди них нет.
const SearchLimit = 20

// FoundCard — карточка из поиска по организации: чтобы выбрать её для
// связи, нужны номер, название и чья она. Уровень доски отличает эпик
// портфеля от задачи команды с тем же названием.
type FoundCard struct {
	ID         string `json:"id"`
	Number     string `json:"number"`
	Title      string `json:"title"`
	BoardID    string `json:"boardId"`
	BoardName  string `json:"boardName"`
	BoardLevel string `json:"boardLevel"`
}

// SearchCards ищет карточки по номеру и названию на всех досках, которые
// видит спрашивающий.
//
// Нужен связи через доски: сервер связывал задачу команды с эпиком
// портфеля всегда, а выбрать эпик было не из чего — выбор предлагал
// карточки только своей доски (замечено владельцем 25.09.2026). Что
// видно, решают политики базы, как и везде: чужая закрытая доска в
// поиск не попадает, и отдельной проверки прав здесь нет намеренно.
//
// Номер, совпавший целиком, идёт первым: по номеру ищут, когда он
// известен, и тогда нужна ровно эта карточка. Дальше — свежие: связывают
// обычно то, что завели недавно. Подстрока ищется `strpos`, а не `ilike`:
// знаки `%` и `_` в запросе остаются буквами, и экранировать нечего.
//
// Прежний номер перенесённой карточки (ROADMAP 40) находит её тоже, но
// только целиком: на старый номер ссылаются переписка и внешние
// системы, и найти по нему нужно ровно ту карточку, а не всё похожее.
func (s *Service) SearchCards(ctx context.Context, orgID, userID, query string) ([]FoundCard, error) {
	out := []FoundCard{}
	query = strings.TrimSpace(query)
	if query == "" {
		return out, nil
	}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select c.id, c.number, c.title, b.id, b.name, b.level
			  from cards c
			  join boards b on b.id = c.board_id
			 where c.archived_at is null and b.archived_at is null
			   and (strpos(lower(c.number), lower($1)) > 0
			        or strpos(lower(c.title), lower($1)) > 0
			        or exists (select 1 from card_events e
			                    where e.card_id = c.id and e.type = 'moved_in'
			                      and lower(e.payload ->> 'fromNumber') = lower($1)))
			 order by lower(c.number) = lower($1) desc, c.created_at desc
			 limit $2`, query, SearchLimit)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (FoundCard, error) {
			var c FoundCard
			return c, r.Scan(&c.ID, &c.Number, &c.Title, &c.BoardID, &c.BoardName, &c.BoardLevel)
		})
		return err
	})
	return out, err
}
