package demo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/store/testdb"
)

// Демонстрационные данные проверяются потому, что на них стоит всё,
// чем интерфейс смотрят: стенд, снимки экранов и проход глазами.
// Пакет в 665 строк не проверялся ничем — а «почти всякая ошибка вёрстки
// видна только на настоящей длине текста и настоящем числе меток»,
// то есть данные и есть измерительный прибор.
//
// Проверяется не строка в строку, а обещания из README.md: три доски
// трёх видимостей, четыре роли, дерево в три уровня, блокировка
// с причиной, подзадача на доске соседей, ветка в обсуждении, закрытая
// и идущая итерации, архив и история за прошлые недели.
//
// Первый же прогон нашёл поломку, прожившую незамеченной: журнал
// переходов не сдвигался в прошлое. `update card_events` не проходил
// политики (у журнала нет разрешения на изменение — он только
// дописывается) и молча обновлял ноль строк. Лента доски показывала,
// что вся её история случилась в одну минуту.
func TestDemoDataKeepsItsPromises(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	// База стенда общая с базой проверок: демонстрационные данные
	// в ней обычно уже есть, и заводить вторую такую организацию
	// нельзя. Тогда проверяем те, что есть, — это и нужно: они и есть
	// стенд, на который смотрят.
	err := Fill(ctx, db)
	if err != nil && !errors.Is(err, ErrAlreadyFilled) {
		t.Fatalf("наполнение: %v", err)
	}

	if err := Verify(ctx, db); err != nil {
		t.Errorf("%v", err)
	}
}

// Английская организация стенда — по ней снимают английские снимки
// для README и документации (ROADMAP 30.5). Обещания те же, что
// у русской: снимок, на котором нет блокировки или закрытой итерации,
// показывал бы продукт беднее, чем он есть. Что в ней не осталось
// русского, проверяет английская песочница — наполнение то же.
func TestEnglishStandKeepsItsPromises(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	err := FillEnglish(ctx, db)
	if err != nil && !errors.Is(err, ErrAlreadyFilled) {
		t.Fatalf("наполнение: %v", err)
	}
	if err := VerifyEnglish(ctx, db); err != nil {
		t.Errorf("%v", err)
	}
}

// Блокировки со сроком ставятся от момента наполнения — «через двое
// суток», «через пять часов», — и снимаются сами. Через двое суток стенд
// не проходил собственную сверку, хотя его никто не трогал: 25.09.2026
// так упала выкладка стенда, наполненного 22.09. Долив обязан вернуть
// то, что отняло время.
func TestTopUpRenewsBlockDeadlinesThatRanOut(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	err := Fill(ctx, db)
	if err != nil && !errors.Is(err, ErrAlreadyFilled) {
		t.Fatalf("наполнение: %v", err)
	}
	var orgID, ownerID string
	if err := db.Pool.QueryRow(ctx, `
		select o.id, u.id from orgs o
		  join memberships m on m.org_id = o.id
		  join users u on u.id = m.user_id
		 where o.name = $1 and lower(u.email) = $2`, OrgName, People[0].Email).
		Scan(&orgID, &ownerID); err != nil {
		t.Fatalf("организация стенда: %v", err)
	}
	// Время прошло: все сроки вышли, блокировки снялись.
	if err := db.InTenant(ctx, orgID, ownerID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			update card_blocks set unblocked_at = now()
			 where unblocked_at is null and blocked_until is not null`)
		return err
	}); err != nil {
		t.Fatalf("истечение сроков: %v", err)
	}
	if err := Verify(ctx, db); err == nil {
		t.Fatal("сверка прошла без блокировки со сроком — проверка ничего не проверяет")
	}

	if err := TopUp(ctx, db); err != nil {
		t.Fatalf("долив: %v", err)
	}
	if err := Verify(ctx, db); err != nil {
		t.Errorf("после долива: %v", err)
	}
}

// Идущая итерация демо кончается через пару дней после наполнения
// и закрывается сама (ROADMAP 34.10). Долив возвращает идущую
// и переносит в неё незакрытое — иначе стенд не прошёл бы сверку.
func TestTopUpRenewsTheRunningIterationThatEnded(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	err := Fill(ctx, db)
	if err != nil && !errors.Is(err, ErrAlreadyFilled) {
		t.Fatalf("наполнение: %v", err)
	}
	var orgID, ownerID string
	if err := db.Pool.QueryRow(ctx, `
		select o.id, u.id from orgs o
		  join memberships m on m.org_id = o.id
		  join users u on u.id = m.user_id
		 where o.name = $1 and lower(u.email) = $2`, OrgName, People[0].Email).
		Scan(&orgID, &ownerID); err != nil {
		t.Fatalf("организация стенда: %v", err)
	}
	// Время прошло: идущая итерация кончилась позавчера, заведена
	// до своего конца, а карточки в неё положили тогда же.
	if err := db.InTenant(ctx, orgID, ownerID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			update iteration_cards ic set added_at = now() - interval '9 days'
			  from iterations i
			 where i.id = ic.iteration_id and i.closed_at is null`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			update iterations set starts_on = current_date - 8, ends_on = current_date - 2,
			       created_at = now() - interval '10 days'
			 where closed_at is null`)
		return err
	}); err != nil {
		t.Fatalf("конец итерации: %v", err)
	}
	// Сервер её закрыл — и стенд остался без идущей.
	if _, err := board.New(db).CloseDueIterations(ctx); err != nil {
		t.Fatalf("закрытие по календарю: %v", err)
	}
	if err := Verify(ctx, db); err == nil {
		t.Fatal("сверка прошла без идущей итерации — проверка ничего не проверяет")
	}

	if err := TopUp(ctx, db); err != nil {
		t.Fatalf("долив: %v", err)
	}
	if err := Verify(ctx, db); err != nil {
		t.Errorf("после долива: %v", err)
	}
	var carried int
	if err := db.InTenant(ctx, orgID, ownerID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select count(*) from iteration_cards ic
			  join iterations i on i.id = ic.iteration_id
			 where i.closed_at is null and ic.removed_at is null`).Scan(&carried)
	}); err != nil {
		t.Fatal(err)
	}
	if carried == 0 {
		t.Error("незакрытое из кончившейся итерации в новую не перенесено")
	}
}
