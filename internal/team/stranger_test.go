package team

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Человека из чужой организации нельзя назвать ни в одном действии
// над людьми.
//
// Таблицы личности под RLS не попадают намеренно, и границу у них держит
// код. Значит, отсутствие проверки не отказывает, а молчит: строка
// заводится, и следом список отдаёт имя и почту постороннего — join
// к `users` возвращает того, кого ему назвали, политики его не остановят.
//
// Именно так и было до 22 августа: состав подразделения проверял
// членство, а наблюдение и права администратора — нет. Обе дыры отдавали
// наружу имя и почту человека из соседней организации, и обе прожили
// незамеченными, потому что проверки на это не было вовсе.
func TestStrangerFromAnotherOrgIsRefused(t *testing.T) {
	f := newFixture(t)
	stranger := f.strangerElsewhere()
	dept := f.create("Разработка", nil)

	if _, err := f.svc.Grant(f.ctx, f.orgID, f.owner, stranger, nil); !errors.Is(err, ErrNotOrgMember) {
		t.Errorf("наблюдение постороннему: %v, ожидалось %v", err, ErrNotOrgMember)
	}
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, stranger, dept.ID); !errors.Is(err, ErrNotOrgMember) {
		t.Errorf("администратор из постороннего: %v, ожидалось %v", err, ErrNotOrgMember)
	}
	if err := f.svc.AddMember(f.ctx, f.orgID, f.owner, dept.ID, stranger); !errors.Is(err, ErrNotOrgMember) {
		t.Errorf("посторонний в составе подразделения: %v, ожидалось %v", err, ErrNotOrgMember)
	}

	// Списки — то самое место, где утечка была бы видна: они соединяются
	// с `users`, у которой политик нет.
	observers, err := f.svc.Observers(f.ctx, f.orgID, f.owner)
	if err != nil || len(observers) != 0 {
		t.Errorf("наблюдатели: %d записей (err=%v), ожидалось пусто", len(observers), err)
	}
	admins, err := f.svc.Admins(f.ctx, f.orgID, f.owner)
	if err != nil || len(admins) != 0 {
		t.Errorf("администраторы: %d записей (err=%v), ожидалось пусто", len(admins), err)
	}
}

// strangerElsewhere заводит человека в другой организации: чужой,
// а не безродный. Безродный ловится и слабой проверкой «есть ли он
// вообще», а нужна проверка «состоит ли он здесь».
func (f *fixture) strangerElsewhere() string {
	f.t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()
	var orgID, userID string
	err := f.db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Соседи "+suffix[:8], "alien-"+suffix[:8]).Scan(&orgID)
	if err != nil {
		f.t.Fatalf("создание чужой организации: %v", err)
	}
	f.t.Cleanup(func() { _, _ = f.db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, orgID) })

	err = f.db.Pool.QueryRow(ctx, `
		insert into users (email, name, password_hash)
		values ($1, 'Посторонний Соседов', 'x') returning id`,
		"alien-"+suffix+"@example.test").Scan(&userID)
	if err != nil {
		f.t.Fatalf("создание постороннего: %v", err)
	}
	f.t.Cleanup(func() { _, _ = f.db.Pool.Exec(context.Background(), `delete from users where id = $1`, userID) })

	if _, err := f.db.Pool.Exec(ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		orgID, userID); err != nil {
		f.t.Fatalf("создание чужого членства: %v", err)
	}
	return userID
}
