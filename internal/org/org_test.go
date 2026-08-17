package org

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/store"
)

// Организации, состав и приглашения.
//
// Ошибка здесь не падает, а тихо ломает доступ: сняли последнего
// владельца — организация неуправляема, и чинится это руками в базе.
// Поэтому проверяются не счастливые пути, а границы.

type fixture struct {
	svc *Service
	db  *store.Store
	ctx context.Context
	t   *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("не задан TEST_DATABASE_URL, интеграционные тесты пропущены")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("подключение к тестовой базе: %v", err)
	}
	t.Cleanup(db.Close)
	return &fixture{svc: New(db), db: db, ctx: ctx, t: t}
}

// inOrg выполняет запрос в области организации. Без неё политики
// не пропустят ни чтения приглашений, ни записи: приглашение видно
// либо своей организации, либо тому, кто предъявил токен.
func (f *fixture) inOrg(orgID string, fn func(pgx.Tx) error) {
	f.t.Helper()
	if err := f.db.InOrg(f.ctx, orgID, fn); err != nil {
		f.t.Fatal(err)
	}
}

// user заводит личность без организации.
func (f *fixture) user(name string) (id, email string) {
	f.t.Helper()
	email = uuid.NewString() + "@example.test"
	err := f.db.Pool.QueryRow(f.ctx, `
		insert into users (email, name, password_hash)
		values ($1, $2, 'x') returning id`, email, name).Scan(&id)
	if err != nil {
		f.t.Fatalf("создание пользователя: %v", err)
	}
	f.t.Cleanup(func() {
		_, _ = f.db.Pool.Exec(context.Background(), `delete from users where id = $1`, id)
	})
	return id, email
}

// org заводит организацию с владельцем.
func (f *fixture) org(name string) (m auth.Membership, ownerID string) {
	f.t.Helper()
	ownerID, _ = f.user("Владелец")
	m, err := f.svc.Create(f.ctx, name, ownerID)
	if err != nil {
		f.t.Fatalf("создание организации: %v", err)
	}
	f.t.Cleanup(func() {
		_, _ = f.db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, m.OrgID)
	})
	return m, ownerID
}

// token достаёт токен из ссылки: наружу отдаётся именно ссылка.
func token(link string) string {
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	return parts[len(parts)-1]
}

func TestCreatedOrgHasOwnerAndUniqueSlug(t *testing.T) {
	f := newFixture(t)
	first, ownerID := f.org("Моя команда")

	if first.Role != auth.RoleOwner {
		t.Errorf("создатель получил роль %q, ожидался владелец", first.Role)
	}
	if first.OrgSlug == "" {
		t.Error("организация заведена без адреса")
	}

	members, err := f.svc.Members(f.ctx, first.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].UserID != ownerID {
		t.Fatalf("состав новой организации: %+v", members)
	}

	// Одинаковые названия — обычное дело, и адреса обязаны разойтись.
	second, _ := f.org("Моя команда")
	if second.OrgSlug == first.OrgSlug {
		t.Errorf("две организации получили один адрес %q", first.OrgSlug)
	}
}

// Организация без владельца неуправляема, и починить это можно только
// руками в базе. Поэтому последнего владельца не снимают и не исключают.
func TestLastOwnerIsProtected(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")

	if err := f.svc.SetRole(f.ctx, org.OrgID, ownerID, ownerID, auth.RoleMember); !errors.Is(err, ErrLastOwner) {
		t.Errorf("понижение единственного владельца: %v", err)
	}
	if err := f.svc.Remove(f.ctx, org.OrgID, ownerID, ownerID); !errors.Is(err, ErrLastOwner) {
		t.Errorf("исключение единственного владельца: %v", err)
	}

	// Со вторым владельцем первый волен уйти.
	secondID, _ := f.user("Второй")
	if _, err := f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org.OrgID, secondID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetRole(f.ctx, org.OrgID, secondID, ownerID, auth.RoleMember); err != nil {
		t.Errorf("понижение при втором владельце: %v", err)
	}

	// И теперь уже второй стал последним.
	if err := f.svc.Remove(f.ctx, org.OrgID, secondID, secondID); !errors.Is(err, ErrLastOwner) {
		t.Errorf("исключение ставшего последним владельца: %v", err)
	}
}

func TestRoleChangesAreValidated(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")
	memberID, _ := f.user("Участник")
	if _, err := f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		org.OrgID, memberID); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.SetRole(f.ctx, org.OrgID, ownerID, memberID, "начальник"); err == nil {
		t.Error("выдуманная роль принята")
	}

	stranger, _ := f.user("Посторонний")
	if err := f.svc.SetRole(f.ctx, org.OrgID, ownerID, stranger, auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Errorf("смена роли постороннему: %v", err)
	}
	if err := f.svc.Remove(f.ctx, org.OrgID, ownerID, stranger); !errors.Is(err, ErrNotFound) {
		t.Errorf("исключение постороннего: %v", err)
	}

	if err := f.svc.SetRole(f.ctx, org.OrgID, ownerID, memberID, auth.RoleViewer); err != nil {
		t.Fatal(err)
	}
	members, _ := f.svc.Members(f.ctx, org.OrgID)
	for _, m := range members {
		if m.UserID == memberID && m.Role != auth.RoleViewer {
			t.Errorf("роль не сменилась: %+v", m)
		}
	}
}

// Исключённый не должен остаться в организации активной сессией: иначе
// он продолжит работать до истечения куки.
func TestRemovedMemberLosesActiveOrg(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")
	memberID, _ := f.user("Участник")
	if _, err := f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		org.OrgID, memberID); err != nil {
		t.Fatal(err)
	}

	var sessionID string
	if err := f.db.Pool.QueryRow(f.ctx, `
		insert into sessions (user_id, expires_at, active_org_id)
		values ($1, now() + interval '1 day', $2) returning id`,
		memberID, org.OrgID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Remove(f.ctx, org.OrgID, ownerID, memberID); err != nil {
		t.Fatal(err)
	}

	var active *string
	if err := f.db.Pool.QueryRow(f.ctx,
		`select active_org_id::text from sessions where id = $1`, sessionID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Errorf("сессия исключённого осталась в организации: %v", *active)
	}
}

// Ссылка показывается один раз: в базе лежит только хеш токена.
func TestInviteLinkIsShownOnceAndOnlyHashIsStored(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")

	invite, err := f.svc.Invite(f.ctx, org.OrgID, ownerID,
		"НовыЙ@Example.test", auth.RoleMember, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if invite.Link == "" {
		t.Fatal("приглашение не вернуло ссылку")
	}
	// Почта приводится к нижнему регистру: иначе один и тот же человек
	// заводится дважды.
	if invite.Email != "новый@example.test" {
		t.Errorf("почта сохранена как %q", invite.Email)
	}

	raw := token(invite.Link)
	var stored string
	f.inOrg(org.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select token_hash from invites where id = $1`, invite.ID).Scan(&stored)
	})
	if stored == raw {
		t.Error("токен приглашения лежит в базе открытым")
	}

	// В списке ожидающих ссылки уже нет.
	pending, err := f.svc.PendingInvites(f.ctx, org.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Link != "" {
		t.Errorf("список ожидающих: %+v", pending)
	}
}

func TestInviteIsValidated(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")

	if _, err := f.svc.Invite(f.ctx, org.OrgID, ownerID, "не почта", auth.RoleMember, ""); err == nil {
		t.Error("приглашение без почты принято")
	}
	if _, err := f.svc.Invite(f.ctx, org.OrgID, ownerID, "кто@то.test", "начальник", ""); err == nil {
		t.Error("приглашение с выдуманной ролью принято")
	}

	// Того, кто уже в команде, звать незачем.
	var ownerEmail string
	if err := f.db.Pool.QueryRow(f.ctx,
		`select email from users where id = $1`, ownerID).Scan(&ownerEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Invite(f.ctx, org.OrgID, ownerID, ownerEmail, auth.RoleMember, ""); !errors.Is(err, ErrAlreadyMember) {
		t.Errorf("повторное приглашение участника: %v", err)
	}
}

// Знание секрета и есть право: приглашение открывается токеном, когда
// организация ещё неизвестна.
func TestInviteIsAcceptedOnceAndTellsWhoItIsFor(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")
	guestID, guestEmail := f.user("Гость")

	invite, err := f.svc.Invite(f.ctx, org.OrgID, ownerID, guestEmail, auth.RoleViewer, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	raw := token(invite.Link)

	info, err := f.svc.LookupInvite(f.ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.OrgName != "Компания" || info.Role != auth.RoleViewer {
		t.Errorf("сведения о приглашении: %+v", info)
	}
	// Аккаунт у гостя есть — значит форму заводить не надо.
	if info.NeedsAccount {
		t.Error("приглашение просит завести аккаунт тому, у кого он есть")
	}

	membership, err := f.svc.Accept(f.ctx, raw, guestID)
	if err != nil {
		t.Fatal(err)
	}
	if membership.OrgID != org.OrgID || membership.Role != auth.RoleViewer {
		t.Errorf("принятое приглашение дало: %+v", membership)
	}

	// Второй переход по той же ссылке недействителен: приглашение
	// одноразовое.
	if _, err := f.svc.Accept(f.ctx, raw, guestID); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("повторный приём: %v", err)
	}
	if _, err := f.svc.LookupInvite(f.ctx, raw); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("сведения по использованной ссылке: %v", err)
	}
	if _, err := f.svc.Accept(f.ctx, "выдуманный токен", guestID); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("выдуманный токен принят: %v", err)
	}
}

// Повторный переход по ссылке не должен понижать роль тому, кто уже
// в команде: человек мог открыть письмо дважды.
func TestAcceptDoesNotDowngradeExistingMember(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")
	memberID, memberEmail := f.user("Участник")
	if _, err := f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org.OrgID, memberID); err != nil {
		t.Fatal(err)
	}

	// Приглашение выписано на почту, которой в организации ещё не было
	// в момент выписки; к моменту приёма человек уже владелец.
	//
	// Токен свой на каждый прогон: хеш уникален в таблице, а тестовая
	// база между прогонами не чистится.
	raw := "прямой-" + uuid.NewString()
	var inviteID string
	f.inOrg(org.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
			values ($1, $2, 'viewer', $3, $4, now() + interval '1 day')
			returning id`,
			org.OrgID, memberEmail, hashToken(raw), ownerID).Scan(&inviteID)
	})

	membership, err := f.svc.Accept(f.ctx, raw, memberID)
	if err != nil {
		t.Fatal(err)
	}
	if membership.Role != auth.RoleOwner {
		t.Errorf("приём приглашения понизил владельца до %q", membership.Role)
	}
}

func TestRevokedAndExpiredInvitesDoNotWork(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("Компания")
	guestID, guestEmail := f.user("Гость")

	invite, err := f.svc.Invite(f.ctx, org.OrgID, ownerID, guestEmail, auth.RoleMember, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RevokeInvite(f.ctx, org.OrgID, ownerID, invite.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Accept(f.ctx, token(invite.Link), guestID); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("отозванное приглашение принято: %v", err)
	}
	if err := f.svc.RevokeInvite(f.ctx, org.OrgID, ownerID, invite.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторный отзыв: %v", err)
	}

	// Просроченное — тоже недействительно, и проверяет это база, а не код.
	expired := "протухший-" + uuid.NewString()
	var expiredID string
	f.inOrg(org.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
			values ($1, $2, 'member', $3, $4, now() - interval '1 hour')
			returning id`,
			org.OrgID, guestEmail, hashToken(expired), ownerID).Scan(&expiredID)
	})
	if _, err := f.svc.Accept(f.ctx, expired, guestID); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("просроченное приглашение принято: %v", err)
	}

	if invite.ExpiresAt.Sub(time.Now()) > InviteTTL {
		t.Errorf("срок приглашения дольше объявленного: %v", invite.ExpiresAt)
	}
}

// Приглашение адресуется секретом, а не организацией, — и открывает
// ровно одну строку. Проверка того, что область по токену не протекает.
func TestInviteTokenOpensOnlyItsOwnRow(t *testing.T) {
	f := newFixture(t)
	first, firstOwner := f.org("Первая")
	second, secondOwner := f.org("Вторая")
	_, guestEmail := f.user("Гость")

	mine, err := f.svc.Invite(f.ctx, first.OrgID, firstOwner, guestEmail, auth.RoleMember, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Invite(f.ctx, second.OrgID, secondOwner,
		uuid.NewString()+"@example.test", auth.RoleMember, "http://example.test"); err != nil {
		t.Fatal(err)
	}

	var visible int
	err = f.db.InScope(f.ctx, store.Scope{InviteToken: hashToken(token(mine.Link))},
		func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `select count(*) from invites`).Scan(&visible)
		})
	if err != nil {
		t.Fatal(err)
	}
	if visible != 1 {
		t.Errorf("по токену видно %d приглашений, ожидалось ровно одно", visible)
	}
}

// --- обезличивание (этап 11.5) ---

// Требование «удалите мои данные» исполняется обезличиванием, а не
// удалением строки: на личность ссылаются подписи под работой.
func TestEraseKeepsTheIdentityAndDropsThePerson(t *testing.T) {
	f := newFixture(t)
	o, ownerID := f.org("Компания")
	memberID, email := f.user("Иван Петров")
	if _, err := f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		o.OrgID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(f.ctx, `
		insert into sessions (user_id, expires_at) values ($1, now() + interval '1 day')`,
		memberID); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Erase(f.ctx, o.OrgID, ownerID, memberID); err != nil {
		t.Fatalf("обезличивание: %v", err)
	}

	var name, newEmail, hash string
	var issuer *string
	var anonymized *time.Time
	if err := f.db.Pool.QueryRow(f.ctx, `
		select name, email, password_hash, oidc_issuer, anonymized_at
		  from users where id = $1`, memberID).
		Scan(&name, &newEmail, &hash, &issuer, &anonymized); err != nil {
		t.Fatalf("личность исчезла из базы: %v", err)
	}
	if anonymized == nil {
		t.Error("отметка об обезличивании не поставлена")
	}
	if name == "Иван Петров" || strings.Contains(newEmail, strings.Split(email, "@")[0]) {
		t.Errorf("персональные данные остались: %q / %q", name, newEmail)
	}
	if hash != "" || issuer != nil {
		t.Error("вход остался возможен")
	}

	// Участие снято, сессии оборваны.
	var members, sessions int
	if err := f.db.Pool.QueryRow(f.ctx,
		`select (select count(*) from memberships where user_id = $1),
		        (select count(*) from sessions where user_id = $1)`,
		memberID).Scan(&members, &sessions); err != nil {
		t.Fatal(err)
	}
	if members != 0 || sessions != 0 {
		t.Errorf("участий %d, сессий %d — ожидался ноль и там и там", members, sessions)
	}

	// В журнале организации остался след: кто и над кем.
	// Читаем журнал от владельца: его политика открыта тем, кто видит
	// организацию целиком, а безличной областью — никому.
	var n int
	if err := f.db.InTenant(f.ctx, o.OrgID, ownerID, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select count(*) from audit_events
			 where subject = 'users' and subject_id = $1 and action = 'delete'`,
			memberID).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("записей об обезличивании в журнале: %d, ожидалась одна", n)
	}
}

// Ключ состоит в организации ровно как человек — и в этом польза, — но
// список людей обязан их различать: до этого ключу предлагали роль,
// «Исключить» и «Удалить данные», причём последнее отвечало отказом.
func TestServiceIdentityIsNotAPerson(t *testing.T) {
	f := newFixture(t)
	org, ownerID := f.org("С интеграцией")
	botID, _ := f.user("Обмен со складом")
	if _, err := f.db.Pool.Exec(f.ctx,
		`update users set kind = 'service' where id = $1`, botID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		org.OrgID, botID); err != nil {
		t.Fatal(err)
	}

	members, err := f.svc.Members(f.ctx, org.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		want := "person"
		if m.UserID == botID {
			want = KindService
		}
		if m.Kind != want {
			t.Errorf("вид личности %q: %q, ожидался %q", m.Name, m.Kind, want)
		}
	}

	// Исключение ключа оставило бы действующий токен без доступа, и обмен
	// сломался бы молча. Отказ называет, что делать вместо этого.
	if err := f.svc.Remove(f.ctx, org.OrgID, ownerID, botID); !errors.Is(err, ErrServiceIdentity) {
		t.Fatalf("ожидался отказ по служебной личности, получено %v", err)
	}
	if err := f.svc.Erase(f.ctx, org.OrgID, ownerID, botID); !errors.Is(err, ErrServiceIdentity) {
		t.Fatalf("обезличивание ключа: ожидался отказ, получено %v", err)
	}
	var still int
	if err := f.db.Pool.QueryRow(f.ctx,
		`select count(*) from memberships where org_id = $1 and user_id = $2`,
		org.OrgID, botID).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if still != 1 {
		t.Error("ключ исключён вопреки отказу")
	}
}

// Личность глобальна, а требование приходит в одну организацию: стереть
// человека там, где о его удалении не просили, нельзя.
func TestEraseRefusesSharedIdentity(t *testing.T) {
	f := newFixture(t)
	here, ownerID := f.org("Здесь")
	elsewhere, _ := f.org("Там")
	memberID, _ := f.user("Двойной житель")
	for _, orgID := range []string{here.OrgID, elsewhere.OrgID} {
		if _, err := f.db.Pool.Exec(f.ctx,
			`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
			orgID, memberID); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.svc.Erase(f.ctx, here.OrgID, ownerID, memberID); !errors.Is(err, ErrSharedIdentity) {
		t.Fatalf("ожидался отказ по общей личности, получено %v", err)
	}
	var name string
	if err := f.db.Pool.QueryRow(f.ctx,
		`select name from users where id = $1`, memberID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Двойной житель" {
		t.Error("личность обезличена вопреки отказу")
	}
}

// Последнего владельца обезличить нельзя — по той же причине, по которой
// его нельзя исключить: организация станет неуправляемой.
func TestEraseKeepsTheLastOwner(t *testing.T) {
	f := newFixture(t)
	o, ownerID := f.org("Компания")
	if err := f.svc.Erase(f.ctx, o.OrgID, ownerID, ownerID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("ожидался отказ по последнему владельцу, получено %v", err)
	}
}
