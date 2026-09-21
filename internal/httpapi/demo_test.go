package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/demo"
)

// Публичное демо (ROADMAP 30.1). Проверяется прежде всего то, чего
// у установки заказчика быть не должно: песочницы не заводятся,
// пока демо не включили явно.

func demoAPI(t *testing.T) *api {
	return newAPIWith(t, func(c *config.Config) {
		c.Demo = true
		c.Signup = config.SignupClosed
	})
}

func TestSandboxDoesNotExistOutsideTheDemo(t *testing.T) {
	a := newAPI(t)
	code, _ := a.session().do("POST", "/api/demo/sandbox", nil)
	if code != http.StatusNotFound {
		t.Errorf("без DEMO=on песочница ответила %d, а ручки быть не должно вовсе", code)
	}

	var methods map[string]struct{ Enabled bool }
	raw := a.session().mustDo("GET", "/api/auth/methods", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatal(err)
	}
	if methods["demo"].Enabled {
		t.Error("экран входа предлагает «Попробовать» на установке без демо")
	}
}

func TestSandboxSignsTheVisitorIntoTheirOwnOrganisation(t *testing.T) {
	a := demoAPI(t)

	var methods map[string]struct{ Enabled bool }
	raw := a.session().mustDo("GET", "/api/auth/methods", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatal(err)
	}
	if !methods["demo"].Enabled {
		t.Fatal("в демо экран входа не узнаёт, что можно «Попробовать»")
	}

	visitor := a.session()
	visitor.mustDo("POST", "/api/demo/sandbox", nil, http.StatusOK)

	var me struct {
		ID               string
		OrgID            string
		Role             string
		SandboxExpiresAt *time.Time
	}
	if err := json.Unmarshal(visitor.mustDo("GET", "/api/me", nil, http.StatusOK), &me); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = demo.RemoveSandbox(context.Background(), a.impl.db, me.OrgID)
	})
	if me.Role != "owner" {
		t.Errorf("посетитель вошёл с ролью %q, а смотреть должен всё — владельцем", me.Role)
	}
	if me.SandboxExpiresAt == nil || time.Until(*me.SandboxExpiresAt) < 23*time.Hour {
		t.Errorf("срок песочницы %v, а экран обещает сутки", me.SandboxExpiresAt)
	}

	// Второй посетитель — в другой организации: чужих правок не видно.
	other := a.session()
	other.mustDo("POST", "/api/demo/sandbox", nil, http.StatusOK)
	var theirs struct{ OrgID string }
	if err := json.Unmarshal(other.mustDo("GET", "/api/me", nil, http.StatusOK), &theirs); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = demo.RemoveSandbox(context.Background(), a.impl.db, theirs.OrgID)
	})
	if theirs.OrgID == me.OrgID {
		t.Error("два посетителя попали в одну песочницу")
	}

	// Подписки в демо не заводятся: доставка ходила бы по любому адресу.
	code, body := visitor.do("POST", "/api/webhooks", map[string]any{
		"name": "Наружу", "url": "https://example.test/hook", "events": []string{"card.created"},
	})
	if code != http.StatusForbidden || field(t, body, "code") != "demo_disabled" {
		t.Errorf("подписка в демо: %d %s, ожидался отказ demo_disabled", code, body)
	}
}
