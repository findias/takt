package team

import (
	"errors"
	"testing"
)

// Владелец подразделения назначает и снимает владельцев строго ниже
// своего узла (этап 31, 0073). Проверяется сервис без всякой проверки
// роли над ним: всё отказанное здесь отказано политикой базы.
func TestSubdivisionOwnerAppointsOnlyBelowItself(t *testing.T) {
	f := newFixture(t)
	company := f.create("Компания", nil)
	dev := f.create("Разработка", &company.ID)
	platform := f.create("Платформа", &dev.ID)
	core := f.create("Ядро", &platform.ID)
	sales := f.create("Продажи", &company.ID)

	head := f.user("member") // владелец «Разработки»
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, head, dev.ID); err != nil {
		t.Fatal(err)
	}
	lead := f.user("member")
	other := f.user("member")

	// Ниже себя — на любую глубину.
	platformLead, err := f.svc.GrantAdmin(f.ctx, f.orgID, head, lead, platform.ID)
	if err != nil {
		t.Fatalf("владелец «Разработки» не назначил владельца «Платформы»: %v", err)
	}
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, head, other, core.ID); err != nil {
		t.Errorf("не назначил на два уровня ниже: %v", err)
	}

	// Свой узел, выше и сбоку — нет.
	for name, id := range map[string]string{
		"свой узел": dev.ID, "выше": company.ID, "сосед": sales.ID,
	} {
		if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, head, other, id); !errors.Is(err, ErrAppointNotYours) {
			t.Errorf("%s: ждали ErrAppointNotYours, получили %v", name, err)
		}
	}

	// Власть сверху не урезается: младший не снимает старшего.
	admins, err := f.svc.Admins(f.ctx, f.orgID, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	var headID string
	for _, a := range admins {
		if a.UserID == head && a.TeamID == dev.ID {
			headID = a.ID
		}
	}
	if err := f.svc.RevokeAdmin(f.ctx, f.orgID, lead, headID); !errors.Is(err, ErrAppointNotYours) {
		t.Errorf("владелец «Платформы» снимает владельца «Разработки»: %v", err)
	}
	// Себя со своего узла тоже не снимает: узел не ниже его.
	if err := f.svc.RevokeAdmin(f.ctx, f.orgID, lead, platformLead.ID); !errors.Is(err, ErrAppointNotYours) {
		t.Errorf("владелец снял себя сам: %v", err)
	}

	// Старший снимает назначенного им.
	if err := f.svc.RevokeAdmin(f.ctx, f.orgID, head, platformLead.ID); err != nil {
		t.Errorf("старший не снял владельца ниже: %v", err)
	}

	// Рядовой участник не назначает никого.
	plain := f.user("member")
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, plain, lead, sales.ID); !errors.Is(err, ErrAppointNotYours) {
		t.Errorf("рядовой назначил владельца: %v", err)
	}

	// Снятый теряет и это право.
	if err := f.svc.RevokeAdmin(f.ctx, f.orgID, f.owner, headID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, head, lead, platform.ID); !errors.Is(err, ErrAppointNotYours) {
		t.Errorf("снятый владелец всё ещё назначает: %v", err)
	}
}
