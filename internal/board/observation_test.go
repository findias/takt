package board

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Наблюдение открывает глаза, а не руки — и это не зависит от роли.
//
// На экране «Наблюдение» написано: «наблюдатель видит доски
// подразделения и всех отделов под ним, но ничего в них не меняет».
// Обещание выглядело хрупким: роль и наблюдение — разные вещи, роль
// `viewer` запрещает писать, а запись в `observers` открывает
// видимость, — и участнику с ролью `member` наблюдение, казалось бы,
// должно давать и то и другое.
//
// Прогон 22 августа 2026 показал, что нет: `app_writable_boards`
// про наблюдение не знает вовсе. Запись достаётся по видимости доски
// всей организации, по участию в её подразделении, по имени в составе,
// по роли владельца и по области администратора — наблюдения в этом
// списке нет. Обещание держится при любой роли, и проверка закрепляет
// это, потому что первым же расширением того списка его можно потерять
// молча.
func TestObservationOpensEyesNotHands(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)
	f.assignBoard(dev)

	viewer := addMember(t, f.svc.db, f.orgID, "viewer")
	member := addMember(t, f.svc.db, f.orgID, "member")
	f.observes(viewer, nil)
	f.observes(member, nil)
	plain := addMember(t, f.svc.db, f.orgID, "member")

	if !f.sees(viewer) || !f.sees(member) {
		t.Fatal("наблюдатель за организацией не видит доску подразделения")
	}
	if f.sees(plain) {
		t.Fatal("доску подразделения видит тот, кто в нём не состоит и не наблюдает")
	}

	card := f.createCard("Чужая карточка", f.columnA)
	write := func(who string) error {
		_, err := f.svc.Apply(f.ctx, f.orgID, who, f.boardID, Request{
			OperationID: uuid.NewString(), Type: "UPDATE_CARD",
			Payload: mustJSON(t, map[string]any{"cardId": card, "title": "Переписано"}),
		})
		return err
	}
	for _, c := range []struct {
		who  string
		name string
	}{{viewer, "наблюдатель с ролью наблюдателя"}, {member, "наблюдатель с ролью участника"}} {
		if err := write(c.who); err == nil || !strings.Contains(err.Error(), "только для чтения") {
			t.Errorf("%s правит карточку: %v", c.name, err)
		}
	}
	if err := f.svc.SetAccess(f.ctx, f.orgID, member, f.boardID, VisibilityOrg, nil); err == nil {
		t.Error("наблюдатель сменил видимость доски")
	}
	if err := f.svc.Archive(f.ctx, f.orgID, member, f.boardID); err == nil {
		t.Error("наблюдатель убрал доску в архив")
	}
}

// Наблюдение себе не выдают, и держит это база.
//
// Проверяется запросом мимо службы — прямо в таблицу от имени рядового
// участника. Если бы правило жило в обработчике, проверка бы его
// не увидела и прошла; она и написана так затем, чтобы не проверять
// обёртку вместо правила. Отказ приходит от политики: «new row violates
// row-level security policy».
func TestObservationIsNotSelfGranted(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	// Доска отдана подразделению: доску, открытую всей организации,
	// рядовой видит и без наблюдения, и последняя проверка ничего
	// не значила бы.
	f.assignBoard(dev)
	plain := addMember(t, f.svc.db, f.orgID, "member")

	grant := func(team *string) error {
		return f.svc.db.InTenant(f.ctx, f.orgID, plain, func(tx pgx.Tx) error {
			_, err := tx.Exec(f.ctx,
				`insert into observers (org_id, user_id, team_id) values ($1, $2, $3)`,
				f.orgID, plain, team)
			return err
		})
	}
	if err := grant(nil); err == nil {
		t.Error("рядовой выдал себе наблюдение за всей организацией")
	}
	if err := grant(&dev); err == nil {
		t.Error("рядовой выдал себе наблюдение за подразделением")
	}
	if f.sees(plain) {
		t.Error("после отказов доска всё-таки видна")
	}
}
