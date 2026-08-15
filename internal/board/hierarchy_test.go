package board

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Иерархия подразделений живёт в триггере и в политиках, прикладного кода
// на неё пока нет. Проверяется она тем же способом, что и видимость:
// одними и теми же вопросами от имени разных людей.

// path читает путь команды от корня до неё самой.
func (f *fixture) path(teamID string) []string {
	f.t.Helper()
	var out []string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select ancestor_ids::text[] from teams where id = $1`, teamID).Scan(&out)
	})
	return out
}

// move переносит команду под другого родителя, возвращая ошибку базы.
func (f *fixture) move(teamID string, parent *string) error {
	f.t.Helper()
	return f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update teams set parent_id = $2 where id = $1`, teamID, parent)
		return err
	})
}

func TestTeamPathFollowsParent(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)
	platform := f.team("Платформа", &dev)

	if got := f.path(company); len(got) != 1 || got[0] != company {
		t.Errorf("путь корневой команды %v, ожидался только её собственный идентификатор", got)
	}
	want := []string{company, dev, platform}
	got := f.path(platform)
	if len(got) != len(want) {
		t.Fatalf("путь вложенной команды %v, ожидалось три уровня", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("путь %v не совпал с ожидаемым %v на позиции %d", got, want, i)
		}
	}
}

// Предел не из осторожности: он превращает «сколько угодно предков»
// в массив известной длины, по которому работает индекс.
func TestNestingDeeperThanFiveIsRejected(t *testing.T) {
	f := newFixture(t)

	var parent *string
	for i := 1; i <= 5; i++ {
		id := f.team("Уровень", parent)
		parent = &id
	}

	err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`insert into teams (org_id, name, parent_id) values ($1, 'Шестой', $2)`,
			f.orgID, parent)
		return err
	})
	if err == nil {
		t.Fatal("шестой уровень вложенности принят")
	}
	if !strings.Contains(err.Error(), "глубина вложенности") {
		t.Errorf("отказ не объясняет причину: %v", err)
	}
}

// База сама от цикла не защищает: обход просто зациклился бы, а путь
// перестал бы существовать как понятие.
func TestTeamCannotBeNestedInItsOwnDescendant(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)

	if err := f.move(company, &dev); err == nil {
		t.Fatal("команда стала потомком собственного потомка")
	}
	if err := f.move(company, &company); err == nil {
		t.Fatal("команда стала собственным родителем")
	}
	if got := f.path(company); len(got) != 1 {
		t.Errorf("отвергнутый перенос всё-таки изменил путь: %v", got)
	}
}

// Роль наследуется вниз: состоящий в направлении состоит и во всех отделах
// под ним. Наблюдение ограничено поддеревом — этим оно и отличается от
// «видит всё»: соседнее направление наблюдателю не видно.
func TestMembershipAndObservationFollowTheTree(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)
	platform := f.team("Платформа", &dev)
	sales := f.team("Продажи", &company)

	f.assignBoard(platform)

	head := addMember(t, f.svc.db, f.orgID, "member") // состоит в направлении
	f.joins(head, dev)
	stranger := addMember(t, f.svc.db, f.orgID, "member") // состоит в соседнем
	f.joins(stranger, sales)

	devObserver := addMember(t, f.svc.db, f.orgID, "member")
	f.observes(devObserver, &dev)
	salesObserver := addMember(t, f.svc.db, f.orgID, "member")
	f.observes(salesObserver, &sales)

	if !f.sees(head) {
		t.Error("участник направления не видит доску отдела под ним")
	}
	if f.sees(stranger) {
		t.Error("участник соседнего направления видит чужую доску")
	}
	if !f.sees(devObserver) {
		t.Error("наблюдатель направления не видит доску отдела под ним")
	}
	if f.sees(salesObserver) {
		t.Error("наблюдатель соседнего направления видит чужую доску")
	}
}

// Перенос поддерева переписывает путь всем потомкам — и вместе с ним
// меняется видимость. Это главная проверка триггера: путь хранится
// денормализованным, и рассинхронизация здесь означала бы доступ,
// не соответствующий структуре.
func TestMovingSubtreeRewritesDescendantsAndAccess(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)
	platform := f.team("Платформа", &dev)
	sales := f.team("Продажи", &company)

	f.assignBoard(platform)
	inDev := addMember(t, f.svc.db, f.orgID, "member")
	f.joins(inDev, dev)
	inSales := addMember(t, f.svc.db, f.orgID, "member")
	f.joins(inSales, sales)

	if !f.sees(inDev) || f.sees(inSales) {
		t.Fatal("исходное распределение доступа неверно")
	}

	if err := f.move(platform, &sales); err != nil {
		t.Fatalf("перенос отдела: %v", err)
	}

	if got := f.path(platform); len(got) != 3 || got[1] != sales {
		t.Errorf("путь перенесённого отдела %v, ожидался через «Продажи»", got)
	}
	if f.sees(inDev) {
		t.Error("доступ остался у прежнего направления после переноса")
	}
	if !f.sees(inSales) {
		t.Error("доступ не появился у нового направления после переноса")
	}
}

// Перенос двигает не один узел, а всё, что под ним.
func TestMovingSubtreeRewritesGrandchildren(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)
	platform := f.team("Платформа", &dev)
	squad := f.team("Группа", &platform)
	sales := f.team("Продажи", &company)

	if err := f.move(platform, &sales); err != nil {
		t.Fatalf("перенос отдела: %v", err)
	}

	got := f.path(squad)
	want := []string{company, sales, platform, squad}
	if len(got) != len(want) {
		t.Fatalf("путь внука %v, ожидалось четыре уровня %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("путь внука %v не совпал с ожидаемым %v", got, want)
		}
	}
}

// Перенос может утопить поддерево глубже предела, даже если сам узел
// в него укладывается.
func TestMoveIsRejectedWhenSubtreeWouldExceedDepth(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	a := f.team("А", &company)
	b := f.team("Б", &a)
	deep := f.team("В", &b) // четвёртый уровень
	f.team("Г", &deep)      // пятый — предел исчерпан

	other := f.team("Отдельная", nil)
	sub := f.team("Под ней", &other)

	// Поддерево «А» имеет высоту четыре; под второй уровень оно не влезает.
	if err := f.move(a, &sub); err == nil {
		t.Fatal("перенос увёл поддерево глубже пяти уровней")
	}
	if got := f.path(deep); len(got) != 4 || got[0] != company {
		t.Errorf("отвергнутый перенос изменил путь потомка: %v", got)
	}
}
