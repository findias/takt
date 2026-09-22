package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/auth"
)

// Смена почты (ROADMAP 23.6). Почта — имя для входа, поэтому
// проверяется то, ради чего она осторожна: прежним адресом больше
// не входят, новым входят; чужой адрес не занять; владелец меняет
// почту только тому, кто состоит лишь у него; вписанный самому себе
// адрес не привязывает корпоративный вход.

func freshEmail(prefix string) string {
	return prefix + "-" + uuid.NewString()[:8] + "@example.test"
}

func TestPersonChangesOwnEmail(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Своя почта")
	next := freshEmail("novaya")

	raw := owner.mustDo("PUT", "/api/me/email", map[string]any{
		"current": "parol12345", "email": "  " + strings.ToUpper(next) + " "}, http.StatusOK)
	if got, _ := field(t, raw, "email").(string); got != next {
		t.Fatalf("почта после смены %q, ожидалась %q", got, next)
	}
	if code, _, _ := a.login(owner.email, "parol12345"); code != http.StatusUnauthorized {
		t.Errorf("прежним адресом всё ещё входят: код %d", code)
	}
	if code, raw, _ := a.login(next, "parol12345"); code != http.StatusOK {
		t.Errorf("новым адресом не входят: код %d, %s", code, raw)
	}

	// Владелец видит смену в журнале — «почта: было → стало».
	// Читается от имени владельца: журнал закрыт политиками.
	me := owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	orgID, _ := field(t, me, "orgId").(string)
	var payload string
	if err := a.impl.db.InTenant(context.Background(), orgID, owner.userID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
			select payload::text from audit_events
			 where subject = 'users' and subject_id = $1 order by id desc limit 1`,
			owner.userID).Scan(&payload)
	}); err != nil {
		t.Fatalf("смены нет в журнале: %v", err)
	}
	if !strings.Contains(payload, owner.email) || !strings.Contains(payload, next) {
		t.Errorf("в журнале не названы оба адреса: %s", payload)
	}
}

func TestEmailChangeRefusesWhatItShould(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отказы почты")
	other := a.registerOrg("Чужая")

	cases := []struct {
		body map[string]any
		want int
		code string
	}{
		{map[string]any{"current": "не тот", "email": freshEmail("x")}, http.StatusForbidden, "password_wrong"},
		{map[string]any{"current": "parol12345", "email": "не почта"}, http.StatusBadRequest, "email_invalid"},
		{map[string]any{"current": "parol12345", "email": "Анна <a@b.test>"}, http.StatusBadRequest, "email_invalid"},
		{map[string]any{"current": "parol12345", "email": owner.email}, http.StatusBadRequest, "email_same"},
		{map[string]any{"current": "parol12345", "email": strings.ToUpper(other.email)}, http.StatusConflict, "email_taken"},
	}
	for _, c := range cases {
		raw := owner.mustDo("PUT", "/api/me/email", c.body, c.want)
		if got, _ := field(t, raw, "code").(string); got != c.code {
			t.Errorf("%v: код отказа %q, ожидался %q", c.body, got, c.code)
		}
	}
	if code, _, _ := a.login(owner.email, "parol12345"); code != http.StatusOK {
		t.Errorf("после отказов прежний адрес перестал пускать: код %d", code)
	}
}

// Почту корпоративной учётной записи ведёт провайдер: сменённая здесь,
// она вернулась бы при следующем входе.
func TestFederatedEmailIsTheProvidersToChange(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Почта у провайдера")
	if _, err := a.impl.db.Pool.Exec(context.Background(),
		`update users set oidc_issuer = 'https://provider.test', oidc_subject = $2 where id = $1`,
		owner.userID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	me := owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	if managed, _ := field(t, me, "emailManaged").(bool); !managed {
		t.Errorf("профиль не знает, что почту ведёт провайдер: %s", me)
	}
	raw := owner.mustDo("PUT", "/api/me/email", map[string]any{
		"current": "parol12345", "email": freshEmail("x")}, http.StatusConflict)
	if got, _ := field(t, raw, "code").(string); got != "email_managed" {
		t.Errorf("код отказа %q, ожидался email_managed", got)
	}
}

// Адрес, вписанный самому себе, не подтверждён ничем — и корпоративный
// вход по нему запись не привязывает: иначе, вписав чужой адрес заранее,
// можно было бы получить того, кто придёт с ним позже.
func TestSelfSetEmailDoesNotBindCorporateSignIn(t *testing.T) {
	a := newAPI(t)
	squatter := a.registerOrg("Занял заранее")
	victim := freshEmail("chuzhoy")
	squatter.mustDo("PUT", "/api/me/email", map[string]any{
		"current": "parol12345", "email": victim}, http.StatusOK)

	_, err := auth.FederatedLogin(context.Background(), a.impl.db.Pool,
		"https://provider.test", uuid.NewString(), victim, true, "Настоящий", "nevazhno", auth.RoleMember)
	if !errors.Is(err, auth.ErrEmailHeld) {
		t.Fatalf("вход с адресом, вписанным другим самому себе: %v, ожидался ErrEmailHeld", err)
	}
	var subject *string
	if err := a.impl.db.Pool.QueryRow(context.Background(),
		`select oidc_subject from users where id = $1`, squatter.userID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if subject != nil {
		t.Fatal("корпоративный вход привязан к записи, где почту вписали самому себе")
	}
}

func TestOwnerFixesMemberEmail(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Опечатка")
	member := owner.join("member")
	next := freshEmail("ispravleno")

	team := owner.mustDo("GET", "/api/team", nil, http.StatusOK)
	if !strings.Contains(string(team), `"emailEditable":true`) {
		t.Errorf("в составе не сказано, кому почту можно сменить: %s", team)
	}

	raw := owner.mustDo("PUT", "/api/members/"+member.userID+"/email",
		map[string]any{"email": next}, http.StatusOK)
	if got, _ := field(t, raw, "email").(string); got != next {
		t.Fatalf("почта после смены %q", got)
	}
	if code, raw, _ := a.login(next, "parol12345"); code != http.StatusOK {
		t.Errorf("исправленным адресом не входят: код %d, %s", code, raw)
	}
	// Вписанная владельцем почта подтверждена его словом.
	var unconfirmed bool
	if err := a.impl.db.Pool.QueryRow(context.Background(),
		`select email_unconfirmed from users where id = $1`, member.userID).Scan(&unconfirmed); err != nil {
		t.Fatal(err)
	}
	if unconfirmed {
		t.Error("почта, вписанная владельцем, помечена неподтверждённой")
	}

	// Участник меняет почту не другим, а владелец себе — не отсюда.
	member.mustDo("PUT", "/api/members/"+owner.userID+"/email",
		map[string]any{"email": freshEmail("x")}, http.StatusForbidden)
	raw = owner.mustDo("PUT", "/api/members/"+owner.userID+"/email",
		map[string]any{"email": freshEmail("x")}, http.StatusConflict)
	if got, _ := field(t, raw, "code").(string); got != "email_own" {
		t.Errorf("своя почта через «Команду»: код %q, ожидался email_own", got)
	}
}

// Человек из двух организаций: почта — имя для входа в обе, и владелец
// одной ею не распоряжается.
func TestOwnerCannotChangeEmailOfSomeoneFromElsewhere(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Здесь")
	elsewhere := a.registerOrg("Там")
	token := owner.invite(elsewhere.email, "member")
	elsewhere.mustDo("POST", "/api/invites/accept", map[string]any{"token": token}, http.StatusOK)

	raw := owner.mustDo("PUT", "/api/members/"+elsewhere.userID+"/email",
		map[string]any{"email": freshEmail("x")}, http.StatusConflict)
	if got, _ := field(t, raw, "code").(string); got != "email_elsewhere" {
		t.Errorf("код отказа %q, ожидался email_elsewhere", got)
	}
	if code, _, _ := a.login(elsewhere.email, "parol12345"); code != http.StatusOK {
		t.Errorf("после отказа прежний адрес не пускает: код %d", code)
	}

	// И чужого, кто здесь не состоит, не найти вовсе.
	stranger := a.registerOrg("Никак")
	owner.mustDo("PUT", "/api/members/"+stranger.userID+"/email",
		map[string]any{"email": freshEmail("x")}, http.StatusNotFound)
}
