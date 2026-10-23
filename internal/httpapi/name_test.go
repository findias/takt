package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Своё имя человек меняет сам (ROADMAP 36.2): до этого оно задавалось
// один раз и опечатка жила вечно.
func TestPersonChangesOwnName(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Своё имя")

	raw := owner.mustDo("PUT", "/api/me/name", map[string]any{"name": "  Анна   Петрова\n"}, http.StatusOK)
	if got, _ := field(t, raw, "name").(string); got != "Анна Петрова" {
		t.Fatalf("имя после смены %q, ожидалось «Анна Петрова» — края и двойные пробелы убраны", got)
	}
	me := owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	if got, _ := field(t, me, "name").(string); got != "Анна Петрова" {
		t.Errorf("«кто я» отвечает прежним именем: %q", got)
	}

	// Смена видна в журнале — «имя: было → стало».
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
	if !strings.Contains(payload, `"Анна Петрова"`) || !strings.Contains(payload, `"old"`) {
		t.Errorf("в журнале не названы оба имени: %s", payload)
	}
}

func TestNameChangeRefusesWhatItShould(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Имя с отказами")

	for _, tc := range []struct{ name, code string }{
		{"   ", "name_invalid"},
		{strings.Repeat("я", 101), "name_invalid"},
	} {
		raw := owner.mustDo("PUT", "/api/me/name", map[string]any{"name": tc.name}, http.StatusBadRequest)
		if got, _ := field(t, raw, "code").(string); got != tc.code {
			t.Errorf("имя %q: код %q, ожидался %s", tc.name, got, tc.code)
		}
	}
	me := owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	current, _ := field(t, me, "name").(string)
	raw := owner.mustDo("PUT", "/api/me/name", map[string]any{"name": current}, http.StatusBadRequest)
	if got, _ := field(t, raw, "code").(string); got != "name_same" {
		t.Errorf("то же имя: код %q, ожидался name_same", got)
	}
}

// Имя, которое ведёт провайдер, он же перепишет при следующем входе —
// правка здесь выглядела бы как несохранившаяся.
func TestFederatedNameIsTheProvidersToChange(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Имя у провайдера")
	if _, err := a.impl.db.Pool.Exec(context.Background(),
		`update users set oidc_issuer = 'https://provider.test', oidc_subject = $2 where id = $1`,
		owner.userID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	raw := owner.mustDo("PUT", "/api/me/name", map[string]any{"name": "Другое Имя"}, http.StatusConflict)
	if got, _ := field(t, raw, "code").(string); got != "name_managed" {
		t.Errorf("код отказа %q, ожидался name_managed", got)
	}
}

func TestOwnerRenamesMember(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Опечатка в имени")
	member := owner.join("member")

	team := owner.mustDo("GET", "/api/team", nil, http.StatusOK)
	if !strings.Contains(string(team), `"nameEditable":true`) {
		t.Errorf("в составе не сказано, кому имя можно сменить: %s", team)
	}

	raw := owner.mustDo("PUT", "/api/members/"+member.userID+"/name",
		map[string]any{"name": "Борис Исправленный"}, http.StatusOK)
	if got, _ := field(t, raw, "name").(string); got != "Борис Исправленный" {
		t.Fatalf("имя после смены %q", got)
	}
	me := member.mustDo("GET", "/api/me", nil, http.StatusOK)
	if got, _ := field(t, me, "name").(string); got != "Борис Исправленный" {
		t.Errorf("участник видит прежнее имя: %q", got)
	}

	// Участник не меняет имя другим, а владелец себе — не отсюда.
	member.mustDo("PUT", "/api/members/"+owner.userID+"/name",
		map[string]any{"name": "Кто-то"}, http.StatusForbidden)
	raw = owner.mustDo("PUT", "/api/members/"+owner.userID+"/name",
		map[string]any{"name": "Кто-то"}, http.StatusConflict)
	if got, _ := field(t, raw, "code").(string); got != "name_own" {
		t.Errorf("своё имя через «Команду»: код %q, ожидался name_own", got)
	}
}

// Человек из двух организаций: имя одно на обе, и владелец одной
// им не распоряжается.
func TestOwnerCannotRenameSomeoneFromElsewhere(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Здесь имя")
	elsewhere := a.registerOrg("Там имя")
	token := owner.invite(elsewhere.email, "member")
	elsewhere.mustDo("POST", "/api/invites/accept", map[string]any{"token": token}, http.StatusOK)

	raw := owner.mustDo("PUT", "/api/members/"+elsewhere.userID+"/name",
		map[string]any{"name": "Чужое"}, http.StatusConflict)
	if got, _ := field(t, raw, "code").(string); got != "name_elsewhere" {
		t.Errorf("код отказа %q, ожидался name_elsewhere", got)
	}
	stranger := a.registerOrg("Никак имя")
	owner.mustDo("PUT", "/api/members/"+stranger.userID+"/name",
		map[string]any{"name": "Чужое"}, http.StatusNotFound)
}
