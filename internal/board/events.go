package board

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Чтение журнала переходов.
//
// Журнал append-only и растёт неограниченно, поэтому листается курсором,
// а не номером страницы: смещение по номеру на растущей ленте показывает
// одно и то же дважды и пропускает то, что вклинилось между запросами.
// Курсор — идентификатор последней показанной записи, а он монотонный.

// FeedLimit — сколько записей отдаётся за раз. Лента читается сверху вниз
// и почти никогда не до конца.
const FeedLimit = 50

type Event struct {
	ID     int64  `json:"id"`
	CardID string `json:"cardId"`
	// Название карточки на момент запроса, а не на момент события:
	// в ленте доски иначе не понять, о чём речь.
	CardTitle string `json:"cardTitle"`
	// Пусто, если действие сделано без установленной личности —
	// миграцией или служебной задачей.
	Actor   *string         `json:"actor"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}

type Feed struct {
	Events []Event `json:"events"`
	// Курсор для следующей страницы; пусто, когда лента кончилась.
	Next *int64 `json:"next"`
}

// Events отдаёт события доски или одной карточки, от свежих к старым.
//
// Политика видимости уже отсекает чужие доски, поэтому отдельной проверки
// доступа здесь нет: недоступная доска просто вернёт пустую ленту. Это то
// же правило, что и везде — существование недоступного не подтверждается.
// Events читает ленту доски. mine оставляет только то, что относится
// к спрашивающему: события карточек, где он исполнитель, и реплики,
// в которых его упомянули.
//
// Отбор именно такой, потому что лента отвечает на вопрос «что случилось
// с моей работой». Свои же действия из неё не вычитаются: человек,
// вернувшийся из отпуска, хочет видеть и то, что делал сам до отъезда,
// а «кто» в каждой строке и так написано.
func (s *Service) Events(ctx context.Context, orgID, userID, boardID string, cardID string, before *int64, mine bool) (Feed, error) {
	feed := Feed{Events: []Event{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select e.id, e.card_id, c.title, u.name, e.type, e.payload, e.at
			  from card_events e
			  join cards c on c.id = e.card_id
			  left join users u on u.id = e.actor_id
			 where e.board_id = $1
			   and ($2 = '' or e.card_id = $2::uuid)
			   and ($3::bigint is null or e.id < $3)
			   and (not $5::bool
			        or exists (select 1 from card_assignees a
			                    where a.card_id = e.card_id and a.user_id = $6)
			        or (e.type = 'commented' and exists (
			              select 1 from card_comment_mentions m
			               where m.user_id = $6
			                 and m.comment_id = (e.payload ->> 'commentId')::uuid)))
			 order by e.id desc
			 limit $4`, boardID, cardID, before, FeedLimit+1, mine, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.ID, &e.CardID, &e.CardTitle, &e.Actor,
				&e.Type, &e.Payload, &e.At); err != nil {
				return err
			}
			feed.Events = append(feed.Events, e)
		}
		return rows.Err()
	})
	if err != nil {
		return Feed{}, err
	}

	// Просим на одну запись больше, чем отдаём: так видно, есть ли
	// продолжение, и не нужен отдельный запрос на подсчёт.
	if len(feed.Events) > FeedLimit {
		feed.Events = feed.Events[:FeedLimit]
		last := feed.Events[len(feed.Events)-1].ID
		feed.Next = &last
	}
	return feed, nil
}
