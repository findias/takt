package board

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Срок блокировки (ROADMAP 28.1).
//
// «Ждём поставку до четверга» не должно быть поводом вернуться
// в карточку в четверг, а забытая блокировка — это открытый интервал,
// и время в блоке копится: врут метрики потока, ради которых
// блокировка и сделана интервалом. Проверяется то, из-за чего
// устройство именно такое: срок — момент, и в прошлом он отвергается;
// снимает проход в самом сервере — моментом срока, а не моментом
// прохода; второй проход не пишет второго события; проход работает
// поверх всех организаций, и политики строк его не съедают.

func (f *fixture) blockUntil(cardID string, until time.Time) {
	f.t.Helper()
	f.mustApply("BLOCK_CARD", map[string]any{
		"cardId": cardID, "reason": "ждём поставку", "until": until.Format(time.RFC3339),
	})
}

// backdate переносит срок открытой блокировки в прошлое. Сервер такой
// срок не примет — и правильно, — а ждать настоящего истечения
// в проверке незачем.
func (f *fixture) backdate(cardID string, until time.Time) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			update card_blocks set blocked_until = $2
			 where card_id = $1 and unblocked_at is null`, cardID, until)
		return err
	})
}

func (f *fixture) expire() {
	f.t.Helper()
	if _, err := f.svc.ExpireBlocks(f.ctx); err != nil {
		f.t.Fatalf("проход по срокам блокировок: %v", err)
	}
}

// closedAt — когда закрыт последний интервал блокировки карточки; нуль —
// интервал ещё открыт.
func (f *fixture) closedAt(cardID string) (time.Time, *string) {
	f.t.Helper()
	var at *time.Time
	var by *string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select unblocked_at, unblocked_by::text from card_blocks
			 where card_id = $1 order by blocked_at desc limit 1`, cardID).Scan(&at, &by)
	})
	if at == nil {
		return time.Time{}, by
	}
	return *at, by
}

func (f *fixture) eventsOf(cardID, kind string) (count int, actors []*string) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `
			select actor_id::text from card_events
			 where card_id = $1 and type = $2`, cardID, kind)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a *string
			if err := rows.Scan(&a); err != nil {
				return err
			}
			actors = append(actors, a)
		}
		return rows.Err()
	})
	return len(actors), actors
}

func TestBlockUntilInThePastIsRefused(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Ждём поставку", f.columnA)
	_, err := f.apply("BLOCK_CARD", map[string]any{
		"cardId": id, "reason": "ждём", "until": time.Now().Add(-time.Hour).Format(time.RFC3339),
	})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("срок в прошлом принят: %v", err)
	}
	// Отказ говорит, что делать: снять сейчас — это «Снять блокировку».
	if !strings.Contains(err.Error(), "Снять блокировку") {
		t.Errorf("отказ не объясняет, что делать: %v", err)
	}
	if f.card(id).Blocked != nil {
		t.Error("карточка заблокирована, хотя срок отвергнут")
	}
}

func TestBlockUntilIsShownAndChanged(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Ждём поставку", f.columnA)
	until := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	f.blockUntil(id, until)

	b := f.card(id).Blocked
	if b == nil || b.Until == nil || !b.Until.Equal(until) {
		t.Fatalf("срок не отражён на карточке: %+v", b)
	}

	// Срок правится отдельной операцией, причину она не трогает.
	later := until.Add(24 * time.Hour)
	res := f.mustApply("SET_BLOCK_UNTIL", map[string]any{"cardId": id, "until": later.Format(time.RFC3339)})
	if got := res.Patch.Cards[0].Blocked; got == nil || got.Until == nil || !got.Until.Equal(later) ||
		got.Reason != "ждём поставку" {
		t.Errorf("правка срока: %+v", got)
	}
	if n, _ := f.eventsOf(id, "block_until"); n != 1 {
		t.Errorf("правка срока не записана в журнал: %d событий", n)
	}

	// Пустое — бессрочная.
	res = f.mustApply("SET_BLOCK_UNTIL", map[string]any{"cardId": id, "until": nil})
	if got := res.Patch.Cards[0].Blocked; got == nil || got.Until != nil {
		t.Errorf("бессрочная блокировка сохранила срок: %+v", got)
	}

	// Срок в прошлом не проходит и через правку.
	if _, err := f.apply("SET_BLOCK_UNTIL", map[string]any{
		"cardId": id, "until": time.Now().Add(-time.Minute).Format(time.RFC3339),
	}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("срок в прошлом принят правкой: %v", err)
	}

	// Срок у незаблокированной — не о чем говорить.
	f.mustApply("UNBLOCK_CARD", map[string]any{"cardId": id})
	var conflict *ConflictError
	if _, err := f.apply("SET_BLOCK_UNTIL", map[string]any{
		"cardId": id, "until": later.Format(time.RFC3339),
	}); !errors.As(err, &conflict) {
		t.Errorf("срок у незаблокированной карточки принят: %v", err)
	}
}

func TestExpiredBlockIsClosedAtItsDeadline(t *testing.T) {
	f := newFixture(t)
	expired := f.createCard("Срок вышел", f.columnA)
	endless := f.createCard("Бессрочная", f.columnA)
	future := f.createCard("Срок впереди", f.columnA)

	f.blockUntil(expired, time.Now().Add(time.Hour))
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": endless, "reason": "пока не снимут"})
	f.blockUntil(future, time.Now().Add(72*time.Hour))

	deadline := time.Now().Add(-10 * time.Minute).Truncate(time.Microsecond)
	f.backdate(expired, deadline)
	version := f.boardVersion(f.boardID)

	f.expire()

	// Закрыто моментом срока, а не моментом прохода: иначе опоздание
	// прохода попадало бы во время блока, и метрика зависела бы от того,
	// когда крутился фон.
	at, by := f.closedAt(expired)
	if !at.Equal(deadline) {
		t.Errorf("интервал закрыт в %v, а срок был %v", at, deadline)
	}
	if by != nil {
		t.Errorf("снятие по сроку приписано человеку: %v", *by)
	}
	if f.card(expired).Blocked != nil {
		t.Error("карточка со вышедшим сроком всё ещё заблокирована")
	}

	// Бессрочная и будущая не тронуты.
	if f.card(endless).Blocked == nil || f.card(future).Blocked == nil {
		t.Error("проход снял блокировку, срок которой не вышел")
	}

	// Событие своим типом и без автора.
	if n, actors := f.eventsOf(expired, "block_expired"); n != 1 || actors[0] != nil {
		t.Errorf("событий снятия по сроку %d, авторы %v", n, actors)
	}
	if n, _ := f.eventsOf(expired, "unblocked"); n != 0 {
		t.Error("снятие по сроку записано как снятие человеком")
	}

	// Открытая доска узнаёт о снятии: версия растёт, как от любой
	// операции, и клиент перечитывает снимок.
	if after := f.boardVersion(f.boardID); after <= version {
		t.Errorf("версия доски не выросла: было %d, стало %d", version, after)
	}

	// Второй проход не пишет второго события.
	f.expire()
	if n, _ := f.eventsOf(expired, "block_expired"); n != 1 {
		t.Errorf("второй проход записал ещё событие: %d", n)
	}

	// Индекс «одна открытая блокировка» освободился: карточку можно
	// заблокировать снова, без отказа «уже заблокирована».
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": expired, "reason": "снова ждём"})
}

// Проход идёт без человека и поверх всех организаций. Политики строк
// по умолчанию отдали бы ему ноль строк, и он отработал бы молча
// и впустую — поэтому проверяется результат в двух организациях
// и на закрытой доске, которую без поимённого доступа не видит никто.
func TestSweepClosesInEveryOrganisationAndOnPrivateBoards(t *testing.T) {
	first := newFixture(t)
	second := newFixture(t)

	a := first.createCard("Первая организация", first.columnA)
	b := second.createCard("Вторая организация", second.columnA)
	first.blockUntil(a, time.Now().Add(time.Hour))
	second.blockUntil(b, time.Now().Add(time.Hour))
	if err := second.svc.SetAccess(second.ctx, second.orgID, second.actorID, second.boardID,
		VisibilityPrivate, nil); err != nil {
		t.Fatal(err)
	}
	first.backdate(a, time.Now().Add(-time.Minute))
	second.backdate(b, time.Now().Add(-time.Minute))

	first.expire()

	if first.card(a).Blocked != nil {
		t.Error("в первой организации блокировка не снята")
	}
	if second.card(b).Blocked != nil {
		t.Error("во второй организации (закрытая доска) блокировка не снята")
	}
}
