// Package realtime — оповещение о том, что доска изменилась.
//
// Канал — LISTEN/NOTIFY той же базы: брокера нет и не нужно, а сообщение
// доставляется ровно тем же коммитом, который изменил данные. Уведомление,
// отправленное отдельно от транзакции, рано или поздно уходит без
// изменения или изменение проходит без уведомления.
//
// По проводу едет не патч, а «доска такая-то доехала до такой-то версии».
// Патч пришлось бы сливать с очередью неподтверждённых команд клиента,
// а это тот самый случай, когда сложность сливается не там и не тогда.
// Клиент, увидев версию новее своей, перечитывает снимок — на нашем
// масштабе это дешевле любой хитрости и, главное, не может разъехаться.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
)

// Channel — имя канала в базе. Один на всё: раскладывать по доскам
// значило бы подписываться и отписываться на каждое открытие доски,
// а фильтровать в памяти — одна строка кода.
const Channel = "board_changed"

// Change — что случилось. Ровно столько, сколько нужно клиенту, чтобы
// решить, перечитывать ли ему доску.
type Change struct {
	BoardID string `json:"boardId"`
	Version int64  `json:"version"`
	// ActorID — кто изменил. Клиент своего же изменения уже ждёт и может
	// не дёргаться: оно придёт ответом на операцию.
	ActorID string `json:"actorId"`
}

// Notify сообщает об изменении доски. Вызывается изнутри транзакции
// операции: уведомление уходит подписчикам при коммите и не уходит вовсе,
// если транзакция откатилась.
func Notify(ctx context.Context, tx pgx.Tx, change Change) error {
	body, err := json.Marshal(change)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `select pg_notify($1, $2)`, Channel, string(body))
	return err
}

type subscriber struct {
	boardID string
	changes chan Change
}

// Hub держит одно соединение с базой на весь процесс и раздаёт
// услышанное подписчикам.
type Hub struct {
	db  *store.Store
	log *slog.Logger

	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

func NewHub(db *store.Store, log *slog.Logger) *Hub {
	return &Hub{db: db, log: log, subs: map[*subscriber]struct{}{}}
}

// Subscribe возвращает канал изменений одной доски и способ отписаться.
//
// Канал с запасом: если читатель задумался, мы предпочтём выбросить
// уведомление, а не задержать всех остальных. Потеря уведомления здесь
// не страшна — клиент всё равно перечитывает снимок целиком, а очередное
// изменение придёт следом.
func (h *Hub) Subscribe(boardID string) (<-chan Change, func()) {
	s := &subscriber{boardID: boardID, changes: make(chan Change, 8)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()

	return s.changes, func() {
		h.mu.Lock()
		delete(h.subs, s)
		h.mu.Unlock()
		close(s.changes)
	}
}

// Run слушает базу, пока не отменят контекст. Обрыв соединения — обычное
// дело при перезапуске базы, поэтому цикл переподключается сам.
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := h.listen(ctx); err != nil && ctx.Err() == nil {
			h.log.Error("канал оповещений оборвался", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
}

func (h *Hub) listen(ctx context.Context) error {
	// Соединение держим своё, не из общего оборота: слушающее соединение
	// занято ожиданием и в пул вернуться не может.
	conn, err := h.db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "listen "+Channel); err != nil {
		return err
	}

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var change Change
		if err := json.Unmarshal([]byte(notification.Payload), &change); err != nil {
			h.log.Error("непонятное уведомление", "тело", notification.Payload)
			continue
		}
		h.dispatch(change)
	}
}

func (h *Hub) dispatch(change Change) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		if s.boardID != change.BoardID {
			continue
		}
		select {
		case s.changes <- change:
		default:
			// Читатель не успевает. Выбрасываем: следующее изменение
			// принесёт ту же новость, а задерживать остальных нельзя.
		}
	}
}
