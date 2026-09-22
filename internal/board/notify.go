package board

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Уведомления внутри приложения (ROADMAP, этап 29).
//
// Пишутся в той же транзакции, что и действие, — как доставка подписки
// на события: не теряются при записанном действии и не появляются без
// него. Своё действие самому себе не уведомляется никогда: кто сделал,
// тот знает.

// Поводы уведомлений. Те же строки стоят в ограничении таблицы
// (0057_notifications.sql) — новый повод требует и миграции.
const (
	ReasonMentioned    = "mentioned"
	ReasonAssigned     = "assigned"
	ReasonBlocked      = "blocked"
	ReasonBlockExpired = "block_expired"
)

// notify заводит уведомление каждому получателю, кроме действующего.
// Источник — то, что вызвало уведомление: у каждого действия он свой
// (комментарий, запись журнала, момент назначения), а повтор операции
// сервер не выполняет заново — отдаёт записанный результат. Поэтому
// двойнику взяться неоткуда, и уникальный ключ таблицы — страховка,
// которая сделает ошибку видной, а не проглотит её.
//
// `on conflict do nothing` здесь нельзя: под политиками он требует
// права прочитать вставляемую строку, а читать уведомление вправе
// только получатель, не тот, кто его пишет. Открыть пишущему чтение
// значило бы показать ему, прочитал ли другой, — этого продукт не
// показывает никому.
func notify(ctx context.Context, tx pgx.Tx, orgID, boardID, cardID, actorID, reason, source string, recipients []string) error {
	if len(recipients) == 0 {
		return nil
	}
	var actor any = actorID
	if actorID == "" {
		actor = nil
	}
	_, err := tx.Exec(ctx, `
		insert into notifications (org_id, recipient_id, board_id, card_id, actor_id, reason, source)
		select $1, r, $2, $3, $4, $5, $6
		  from unnest($7::uuid[]) r
		 where r is distinct from $4::uuid`,
		orgID, boardID, cardID, actor, reason, source, recipients)
	return err
}

// eventSource — источник уведомления, вызванного событием журнала.
func eventSource(id int64) string { return "event:" + strconv.FormatInt(id, 10) }

// Notification — уведомление, как его видит получатель.
type Notification struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	BoardID   string    `json:"boardId"`
	BoardName string    `json:"boardName"`
	CardID    string    `json:"cardId"`
	CardNo    string    `json:"cardNumber"`
	CardTitle string    `json:"cardTitle"`
	ActorName *string   `json:"actorName"`
	CreatedAt time.Time `json:"createdAt"`
	Read      bool      `json:"read"`
}

// NotificationsPage — последние уведомления и счётчик непрочитанного.
// Счётчик — отдельно от списка: непрочитанных бывает больше, чем
// помещается в список, и число на колокольчике не должно врать.
type NotificationsPage struct {
	Items  []Notification `json:"items"`
	Unread int            `json:"unread"`
}

// NotificationsLimit — сколько последних показывать.
const NotificationsLimit = 50

// Notifications — свои уведомления, свежие сверху. Видимость доски
// перепроверяет политика таблицы: уведомление о доске, которую больше
// не видно, не приходит вовсе — ни в списке, ни в счётчике.
func (s *Service) Notifications(ctx context.Context, orgID, userID string) (NotificationsPage, error) {
	page := NotificationsPage{Items: []Notification{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`select count(*) from notifications where recipient_id = $1 and read_at is null`,
			userID).Scan(&page.Unread); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			select n.id, n.reason, n.board_id, b.name, n.card_id, c.number, c.title,
			       u.name, n.created_at, n.read_at is not null
			  from notifications n
			  join boards b on b.id = n.board_id
			  join cards c on c.id = n.card_id
			  left join users u on u.id = n.actor_id
			 where n.recipient_id = $1
			 order by n.created_at desc
			 limit $2`, userID, NotificationsLimit)
		if err != nil {
			return err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
			var n Notification
			err := row.Scan(&n.ID, &n.Reason, &n.BoardID, &n.BoardName, &n.CardID, &n.CardNo,
				&n.CardTitle, &n.ActorName, &n.CreatedAt, &n.Read)
			return n, err
		})
		if err != nil {
			return err
		}
		page.Items = items
		return nil
	})
	return page, err
}

// MarkNotificationsRead отмечает прочитанными перечисленные — или все,
// если список пуст. Чужие и невидимые политика не даст тронуть.
func (s *Service) MarkNotificationsRead(ctx context.Context, orgID, userID string, ids []string) error {
	return s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if len(ids) == 0 {
			_, err := tx.Exec(ctx, `
				update notifications set read_at = now()
				 where recipient_id = $1 and read_at is null`, userID)
			return err
		}
		_, err := tx.Exec(ctx, `
			update notifications set read_at = now()
			 where recipient_id = $1 and read_at is null and id = any($2::uuid[])`, userID, ids)
		return err
	})
}
