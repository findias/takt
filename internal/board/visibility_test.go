package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/konkov/agile/internal/store"
)

// Видимость досок целиком живёт в политиках базы: прикладного кода на неё
// ещё нет, и единственный способ её проверить — ходить за одними и теми же
// данными от имени разных людей.

// addMember заводит ещё одного человека в уже существующей организации.
// users и memberships под RLS не попадают, поэтому пишем напрямую.
func addMember(t *testing.T, db *store.Store, orgID, role string) string {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()

	var userID string
	err := db.Pool.QueryRow(ctx, `
		insert into users (email, name, password_hash)
		values ($1, 'Участник', 'x') returning id`,
		suffix+"@example.test").Scan(&userID)
	if err != nil {
		t.Fatalf("создание пользователя: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from users where id = $1`, userID)
	})

	_, err = db.Pool.Exec(ctx, `
		insert into memberships (org_id, user_id, role) values ($1, $2, $3)`,
		orgID, userID, role)
	if err != nil {
		t.Fatalf("создание членства: %v", err)
	}
	return userID
}

// observes ставит человека наблюдателем: над одним поддеревом, если задана
// команда, или над всей организацией, если нет. Раздаёт наблюдение только
// владелец, поэтому запись идёт от имени владельца фикстуры.
func (f *fixture) observes(userID string, teamID *string) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`insert into observers (org_id, user_id, team_id) values ($1, $2, $3)`,
			f.orgID, userID, teamID)
		return err
	})
}

// team заводит команду, при необходимости внутри другой.
func (f *fixture) team(name string, parent *string) string {
	f.t.Helper()
	var id string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`insert into teams (org_id, name, parent_id) values ($1, $2, $3)
			 returning id`, f.orgID, name, parent).Scan(&id)
	})
	return id
}

// joins вписывает человека в команду.
func (f *fixture) joins(userID, teamID string) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`insert into team_members (org_id, team_id, user_id) values ($1, $2, $3)`,
			f.orgID, teamID, userID)
		return err
	})
}

// assignBoard отдаёт доску фикстуры команде.
func (f *fixture) assignBoard(teamID string) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update boards set team_id = $2, visibility = 'team' where id = $1`,
			f.boardID, teamID)
		return err
	})
}

// sees отвечает, видит ли человек доску фикстуры — и списком, и по прямому
// идентификатору. Расхождение между этими двумя ответами само по себе
// дефект: список и снимок обязаны опираться на одно правило.
func (f *fixture) sees(userID string) bool {
	f.t.Helper()
	list, err := f.svc.List(f.ctx, f.orgID, userID)
	if err != nil {
		f.t.Fatalf("список досок: %v", err)
	}
	inList := false
	for _, b := range list {
		if b.ID == f.boardID {
			inList = true
		}
	}

	_, err = f.svc.Snapshot(f.ctx, f.orgID, userID, f.boardID)
	byID := err == nil
	if !byID && !errors.Is(err, ErrNotFound) {
		f.t.Fatalf("снимок доски: %v", err)
	}
	if inList != byID {
		f.t.Errorf("доска %s в списке, но %s по идентификатору",
			map[bool]string{true: "видна", false: "не видна"}[inList],
			map[bool]string{true: "видна", false: "не видна"}[byID])
	}
	return inList && byID
}

// Политика boards заглядывает в team_members и board_members, а к самим
// доскам ходят политики карточек, журнала и связей. Стоит любой из них
// сослаться на себя или обратно на boards — Postgres отвечает 42P17,
// и падает не чтение, а создание первой же доски.
//
// Ровно это и случилось с политикой board_members, поэтому цепочка
// проверяется целиком, а не в одном месте.
func TestBoardVisibilityPoliciesDoNotRecurse(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Задача", f.columnA)
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": id, "reason": "ждём"})

	tables := []string{
		"projects", "boards", "board_columns", "cards", "card_events",
		"operations", "card_links", "card_blocks",
		"teams", "team_members", "board_members",
	}
	for _, table := range tables {
		var n int
		err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `select count(*) from `+table).Scan(&n)
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P17" {
			t.Errorf("политика %s зациклилась: %s", table, pgErr.Message)
			continue
		}
		if err != nil {
			t.Errorf("чтение %s: %v", table, err)
		}
	}
}

// Политика считается один раз на запрос, а не один раз на строку.
//
// Разница не косметическая: на двадцати тысячах карточек прямой вызов
// функции в предикате давал 3942 мс против 4 мс у той же политики,
// записанной через подзапрос. Форма записи и есть производительность,
// поэтому она закреплена тестом — иначе вернуть её назад можно случайно,
// одной «упрощающей» правкой, и заметить это на живых объёмах, а не здесь.
//
// Проверяем по плану запроса: имя функции в строке Filter означает вызов
// на каждую строку. Вынесенный расчёт выглядит как InitPlan или SubPlan.
func TestPoliciesAreEvaluatedOncePerQuery(t *testing.T) {
	f := newFixture(t)
	f.createCard("Задача", f.columnA)

	perRow := []string{
		"app_visible_boards()", "app_writable_boards()",
		"app_current_org()", "app_current_user()",
		"app_is_owner()", "app_can_write()", "app_view_all()",
	}
	for _, table := range []string{"cards", "board_columns", "boards", "card_events"} {
		var plan []string
		f.inTenant(func(tx pgx.Tx) error {
			rows, err := tx.Query(f.ctx, `explain select count(*) from `+table)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					return err
				}
				plan = append(plan, line)
			}
			return rows.Err()
		})

		for _, line := range plan {
			if !strings.Contains(line, "Filter:") {
				continue
			}
			for _, call := range perRow {
				if strings.Contains(line, call) {
					t.Errorf("%s: %s вызывается на каждую строку\n  %s",
						table, call, strings.TrimSpace(line))
				}
			}
			// Хешированный подплан в предикате — это фильтр: строка
			// сначала читается, потом отбрасывается. Массив-константа
			// (`= ANY ($1)`) годится в индексное условие, и тогда
			// читается только видимое. Разница линейная по размеру
			// таблицы, поэтому на тестовых объёмах её видно только так.
			if strings.Contains(line, "SubPlan") {
				t.Errorf("%s: предикат политики стал фильтром вместо индексного условия\n  %s",
					table, strings.TrimSpace(line))
			}
		}
	}
}

func TestTeamBoardIsVisibleOnlyToItsTeamAndObservers(t *testing.T) {
	f := newFixture(t)
	insider := addMember(t, f.svc.db, f.orgID, "member")
	outsider := addMember(t, f.svc.db, f.orgID, "member")
	observer := addMember(t, f.svc.db, f.orgID, "member")
	f.observes(observer, nil)

	// До перевода в командную доска открыта всей организации — иначе
	// миграция прятала бы задним числом то, что уже было видно.
	if !f.sees(outsider) {
		t.Fatal("доска по умолчанию не видна участнику организации")
	}

	teamID := f.team("Найм", nil)
	f.joins(insider, teamID)
	f.assignBoard(teamID)

	if f.sees(outsider) {
		t.Error("командная доска осталась видна постороннему в организации")
	}
	if !f.sees(insider) {
		t.Error("командная доска не видна своей команде")
	}
	// «Видит всё» — признак, а не роль: наблюдатель не состоит в команде.
	if !f.sees(observer) {
		t.Error("командная доска не видна наблюдателю всех команд")
	}
	if !f.sees(f.actorID) {
		t.Error("командная доска не видна владельцу организации")
	}
}

func TestPrivateBoardIsHiddenEvenFromObserverAndOwner(t *testing.T) {
	f := newFixture(t)
	named := addMember(t, f.svc.db, f.orgID, "member")
	observer := addMember(t, f.svc.db, f.orgID, "member")
	f.observes(observer, nil)

	// Порядок здесь не произвольный: закрыть доску можно только вокруг
	// себя, поэтому владелец вписывает в неё и себя тоже.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into board_members (org_id, board_id, user_id)
			values ($1, $2, $3), ($1, $2, $4)`,
			f.orgID, f.boardID, named, f.actorID)
		return err
	})
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update boards set visibility = 'private' where id = $1`, f.boardID)
		return err
	})

	if !f.sees(named) {
		t.Error("закрытая доска не видна тому, кто в неё вписан")
	}
	// Единственное исключение из «видит всё», и оно намеренное: закрытая
	// доска открывается поимённо, иначе слово «закрытая» ничего не значит.
	if f.sees(observer) {
		t.Error("закрытая доска видна наблюдателю всех команд")
	}

	// Владелец организации выходит из доски — и перестаёт её видеть.
	// Должность даёт право раздавать доступ, а не читать закрытое.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`delete from board_members where board_id = $1 and user_id = $2`,
			f.boardID, f.actorID)
		return err
	})
	if f.sees(f.actorID) {
		t.Error("закрытая доска видна владельцу организации, не вписанному в неё")
	}
}

// Правило, из-за которого предыдущий тест устроен именно так: перевести
// доску в состояние, в котором сам её не видишь, нельзя. Postgres проверяет
// политику select и на новом ряде, поэтому доска, закрытая вокруг чужих
// людей, стала бы неисправимой — редактировать невидимую доску не может
// никто, включая владельца организации.
func TestBoardCannotBeClosedAroundSomeoneElse(t *testing.T) {
	f := newFixture(t)
	named := addMember(t, f.svc.db, f.orgID, "member")

	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`insert into board_members (org_id, board_id, user_id) values ($1, $2, $3)`,
			f.orgID, f.boardID, named)
		return err
	})

	err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update boards set visibility = 'private' where id = $1`, f.boardID)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("доска закрыта в обход себя: %v", err)
	}
	if !f.sees(f.actorID) {
		t.Error("отказанное изменение всё-таки применилось")
	}
}

// Состав доски — сам по себе сведения: по списку участников закрытой доски
// читается, чем занята организация.
func TestBoardRosterIsNotReadableByOutsiders(t *testing.T) {
	f := newFixture(t)
	named := addMember(t, f.svc.db, f.orgID, "member")
	outsider := addMember(t, f.svc.db, f.orgID, "member")

	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`insert into board_members (org_id, board_id, user_id) values ($1, $2, $3)`,
			f.orgID, f.boardID, named)
		return err
	})

	rows := func(userID string) int {
		f.t.Helper()
		var n int
		err := f.svc.db.InTenant(f.ctx, f.orgID, userID, func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `select count(*) from board_members`).Scan(&n)
		})
		if err != nil {
			t.Fatalf("чтение состава доски: %v", err)
		}
		return n
	}

	if got := rows(named); got != 1 {
		t.Errorf("участник видит %d своих строк в составе доски, ожидалась одна", got)
	}
	if got := rows(outsider); got != 0 {
		t.Errorf("посторонний видит %d строк в составе доски", got)
	}
	if got := rows(f.actorID); got != 1 {
		t.Errorf("владелец организации видит %d строк в составе доски", got)
	}
}
