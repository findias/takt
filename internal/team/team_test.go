package team

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/store/testdb"
)

// Тесты идут против настоящей базы: почти всё здесь — перевод отказов
// политик и триггеров, и подменять их заглушками значит проверять заглушки.

type fixture struct {
	svc   *Service
	db    *store.Store
	ctx   context.Context
	t     *testing.T
	orgID string
	owner string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testdb.Shared(t)

	f := &fixture{svc: New(db), db: db, ctx: ctx, t: t}
	suffix := uuid.NewString()
	err := db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Тест "+suffix[:8], "team-"+suffix[:8]).Scan(&f.orgID)
	if err != nil {
		t.Fatalf("создание организации: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, f.orgID)
	})
	f.owner = f.user("owner")
	return f
}

// user заводит человека в организации фикстуры.
func (f *fixture) user(role string) string {
	f.t.Helper()
	suffix := uuid.NewString()
	var id string
	err := f.db.Pool.QueryRow(f.ctx, `
		insert into users (email, name, password_hash)
		values ($1, 'Тестовый', 'x') returning id`, suffix+"@example.test").Scan(&id)
	if err != nil {
		f.t.Fatalf("создание пользователя: %v", err)
	}
	f.t.Cleanup(func() {
		_, _ = f.db.Pool.Exec(context.Background(), `delete from users where id = $1`, id)
	})
	_, err = f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, $3)`,
		f.orgID, id, role)
	if err != nil {
		f.t.Fatalf("создание членства: %v", err)
	}
	return id
}

func (f *fixture) create(name string, parent *string) Team {
	f.t.Helper()
	t, err := f.svc.Create(f.ctx, f.orgID, f.owner, name, parent)
	if err != nil {
		f.t.Fatalf("создание команды %q: %v", name, err)
	}
	return t
}

func TestTreeIsListedParentsFirstWithDepth(t *testing.T) {
	f := newFixture(t)
	company := f.create("Компания", nil)
	dev := f.create("Разработка", &company.ID)
	f.create("Платформа", &dev.ID)

	list, err := f.svc.List(f.ctx, f.orgID, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("в списке %d команд, ожидалось три", len(list))
	}
	if list[0].ID != company.ID || list[0].Depth != 1 || list[0].ParentID != nil {
		t.Errorf("первой должна идти корневая команда, получено %+v", list[0])
	}
	if list[2].Depth != 3 {
		t.Errorf("глубина самой вложенной команды %d, ожидалось 3", list[2].Depth)
	}
	for i := 1; i < len(list); i++ {
		if list[i].ParentID == nil {
			t.Errorf("потомок %q без родителя", list[i].Name)
		}
	}
}

// Правила дерева держит база. Здесь проверяется, что отказ доходит
// до вызывающего разборчивым, а не пятисоткой.
func TestTreeLimitsAreReportedReadably(t *testing.T) {
	f := newFixture(t)

	var parent *string
	for i := 0; i < 5; i++ {
		t := f.create("Уровень", parent)
		parent = &t.ID
	}
	_, err := f.svc.Create(f.ctx, f.orgID, f.owner, "Шестой", parent)
	var tree *TreeError
	if !errors.As(err, &tree) {
		t.Fatalf("шестой уровень: ожидался TreeError, получено %v", err)
	}
	if tree.Reason == "" {
		t.Error("отказ пришёл без объяснения")
	}

	root := f.create("Корень", nil)
	child := f.create("Потомок", &root.ID)
	if err := f.svc.Move(f.ctx, f.orgID, f.owner, root.ID, &child.ID); !errors.As(err, &tree) {
		t.Errorf("цикл: ожидался TreeError, получено %v", err)
	}

	// Несуществующий родитель — не найдено, а не сбой.
	missing := uuid.NewString()
	if _, err := f.svc.Create(f.ctx, f.orgID, f.owner, "Сирота", &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий родитель: ожидалось ErrNotFound, получено %v", err)
	}
}

// Узел с живыми потомками архивировать нельзя: иначе доска осталась бы
// у команды, которой больше нет, и перестала быть видна кому бы то ни было.
func TestArchiveRefusesNodeWithChildren(t *testing.T) {
	f := newFixture(t)
	root := f.create("Корень", nil)
	child := f.create("Потомок", &root.ID)

	// Отказ приходит из базы и несёт её слова: правило живёт там
	// (миграция 0045), потому что дверей к архивации две — эта
	// и удаление группы каталогом.
	var tree *TreeError
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, root.ID); !errors.As(err, &tree) ||
		tree.Reason != ErrNotEmpty.Error() {
		t.Fatalf("архивация узла с потомком: ожидалось %q, получено %v", ErrNotEmpty, err)
	}

	if err := f.svc.Move(f.ctx, f.orgID, f.owner, child.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, root.ID); err != nil {
		t.Fatalf("архивация опустевшего узла: %v", err)
	}

	list, err := f.svc.List(f.ctx, f.orgID, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == root.ID {
			t.Error("архивная команда осталась в списке")
		}
	}
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, root.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторная архивация: ожидалось ErrNotFound, получено %v", err)
	}
}

func TestMembersComeOnlyFromTheOrganisation(t *testing.T) {
	f := newFixture(t)
	team := f.create("Найм", nil)
	inside := f.user("member")

	if err := f.svc.AddMember(f.ctx, f.orgID, f.owner, team.ID, inside); err != nil {
		t.Fatalf("добавление в команду: %v", err)
	}
	members, err := f.svc.Members(f.ctx, f.orgID, f.owner, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].UserID != inside {
		t.Fatalf("состав команды %+v", members)
	}
	// Ведущий — тот, у кого есть запись администратора на этом узле,
	// а не пометка рядом с именем.
	if members[0].Lead {
		t.Errorf("рядовой участник помечен ведущим: %+v", members[0])
	}
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, inside, team.ID); err != nil {
		t.Fatal(err)
	}
	if members, _ = f.svc.Members(f.ctx, f.orgID, f.owner, team.ID); !members[0].Lead {
		t.Errorf("администратор узла не показан ведущим: %+v", members[0])
	}

	// Повторное добавление не ломается.
	if err := f.svc.AddMember(f.ctx, f.orgID, f.owner, team.ID, inside); err != nil {
		t.Fatalf("повторное добавление: %v", err)
	}

	// Посторонний — тот, кого нет в организации.
	var outsider string
	err = f.db.Pool.QueryRow(f.ctx, `
		insert into users (email, name, password_hash)
		values ($1, 'Чужой', 'x') returning id`,
		uuid.NewString()+"@example.test").Scan(&outsider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.db.Pool.Exec(context.Background(), `delete from users where id = $1`, outsider)
	})
	if err := f.svc.AddMember(f.ctx, f.orgID, f.owner, team.ID, outsider); !errors.Is(err, ErrNotOrgMember) {
		t.Errorf("посторонний принят в команду: %v", err)
	}

	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.owner, team.ID, inside); err != nil {
		t.Fatalf("исключение из команды: %v", err)
	}
	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.owner, team.ID, inside); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное исключение: ожидалось ErrNotFound, получено %v", err)
	}
}

func TestObservationIsGrantedPerSubtreeAndRevoked(t *testing.T) {
	f := newFixture(t)
	dev := f.create("Разработка", nil)
	watcher := f.user("member")

	o, err := f.svc.Grant(f.ctx, f.orgID, f.owner, watcher, &dev.ID)
	if err != nil {
		t.Fatalf("выдача наблюдения: %v", err)
	}
	if o.TeamName == nil || *o.TeamName != "Разработка" {
		t.Errorf("наблюдение выдано без указания подразделения: %+v", o)
	}

	// Повтор — конфликт, а не второе такое же наблюдение.
	var tree *TreeError
	if _, err := f.svc.Grant(f.ctx, f.orgID, f.owner, watcher, &dev.ID); !errors.As(err, &tree) {
		t.Errorf("повторная выдача: ожидался TreeError, получено %v", err)
	}

	// Наблюдение за всей организацией — отдельное от наблюдения
	// за поддеревом, и одно другому не мешает.
	if _, err := f.svc.Grant(f.ctx, f.orgID, f.owner, watcher, nil); err != nil {
		t.Fatalf("наблюдение за организацией: %v", err)
	}

	list, err := f.svc.Observers(f.ctx, f.orgID, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("наблюдателей в списке %d, ожидалось два", len(list))
	}

	if err := f.svc.Revoke(f.ctx, f.orgID, f.owner, o.ID); err != nil {
		t.Fatalf("отзыв наблюдения: %v", err)
	}
	if err := f.svc.Revoke(f.ctx, f.orgID, f.owner, o.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторный отзыв: ожидалось ErrNotFound, получено %v", err)
	}
}

// Право менять структуру проверяют и маршрут, и политика. Здесь — политика:
// если появится путь в обход проверки роли, ответом должен быть запрет.
func TestOnlyOwnerChangesStructure(t *testing.T) {
	f := newFixture(t)
	root := f.create("Корень", nil)
	member := f.user("member")

	if _, err := f.svc.Create(f.ctx, f.orgID, member, "Своя", nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("рядовой участник завёл команду: %v", err)
	}
	if err := f.svc.Rename(f.ctx, f.orgID, member, root.ID, "Переименована"); err == nil {
		t.Error("рядовой участник переименовал команду")
	}
	if _, err := f.svc.Grant(f.ctx, f.orgID, member, member, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("рядовой участник выдал себе наблюдение: %v", err)
	}

	// Читать структуру при этом может любой: кто с кем работает — не тайна.
	if list, err := f.svc.List(f.ctx, f.orgID, member); err != nil || len(list) != 1 {
		t.Errorf("рядовой участник не видит структуру: %v, %d", err, len(list))
	}
}

// Администратор подразделения — главный пробел, ради которого всё это
// заводилось: до него раздавать доступ мог только владелец организации,
// а в дереве из пяти уровней это неработоспособно.
func TestSubtreeAdminManagesOnlyItsOwnBranch(t *testing.T) {
	f := newFixture(t)
	company := f.create("Компания", nil)
	dev := f.create("Разработка", &company.ID)
	platform := f.create("Платформа", &dev.ID)
	sales := f.create("Продажи", &company.ID)

	head := f.user("member")
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, head, dev.ID); err != nil {
		t.Fatalf("назначение администратора: %v", err)
	}

	// В своей области: завести отдел, вписать человека, выдать наблюдение.
	inside, err := f.svc.Create(f.ctx, f.orgID, head, "Ядро", &platform.ID)
	if err != nil {
		t.Fatalf("администратор не завёл отдел в своей области: %v", err)
	}
	if err := f.svc.AddMember(f.ctx, f.orgID, head, inside.ID, head); err != nil {
		t.Errorf("администратор не вписал человека в свой отдел: %v", err)
	}
	if _, err := f.svc.Grant(f.ctx, f.orgID, head, head, &dev.ID); err != nil {
		t.Errorf("администратор не выдал наблюдение за своей областью: %v", err)
	}

	// В соседней — ничего.
	if _, err := f.svc.Create(f.ctx, f.orgID, head, "Свой отдел", &sales.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("администратор завёл отдел в соседнем направлении: %v", err)
	}
	if err := f.svc.Rename(f.ctx, f.orgID, head, sales.ID, "Перехвачено"); err == nil {
		t.Error("администратор переименовал соседнее направление")
	}
	if err := f.svc.AddMember(f.ctx, f.orgID, head, sales.ID, head); err == nil {
		t.Error("администратор вписал себя в соседнее направление")
	}

	// Корень заводит только владелец: у нового корня нет предка, а значит
	// нет и того, кто мог бы за него отвечать.
	if _, err := f.svc.Create(f.ctx, f.orgID, head, "Новое направление", nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("администратор завёл корневое подразделение: %v", err)
	}

	// И не раздаёт полномочия дальше: иначе власть размножала бы сама себя.
	other := f.user("member")
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, head, other, platform.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("администратор назначил администратора: %v", err)
	}

	// Наблюдение за всей организацией шире любой области и остаётся
	// за владельцем.
	if _, err := f.svc.Grant(f.ctx, f.orgID, head, head, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("администратор выдал наблюдение за всей организацией: %v", err)
	}
}

func TestAdminGrantIsListedAndRevoked(t *testing.T) {
	f := newFixture(t)
	dev := f.create("Разработка", nil)
	head := f.user("member")

	a, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, head, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.TeamName != "Разработка" {
		t.Errorf("полномочие без подразделения: %+v", a)
	}

	var tree *TreeError
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, head, dev.ID); !errors.As(err, &tree) {
		t.Errorf("повторное назначение: %v", err)
	}

	list, err := f.svc.Admins(f.ctx, f.orgID, f.owner)
	if err != nil || len(list) != 1 {
		t.Fatalf("список администраторов: %v, %d", err, len(list))
	}

	if err := f.svc.RevokeAdmin(f.ctx, f.orgID, f.owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RevokeAdmin(f.ctx, f.orgID, f.owner, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное снятие: %v", err)
	}
}
