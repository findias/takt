package demo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

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
