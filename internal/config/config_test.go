package config

import "testing"

// Стенд и демо — две разные роли процесса, и совмещать их нельзя:
// демо видят клиенты, а стенд показывает недопринятое.
func TestStandAndDemoDoNotMix(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/takt")
	t.Setenv("SIGNUP", "closed")
	t.Setenv("DEMO", "on")
	t.Setenv("STAND", "staging")
	if _, err := Load(); err == nil {
		t.Error("DEMO=on и STAND=staging вместе приняты, а должны быть отвергнуты")
	}
}

func TestStandIsOffUnlessAskedFor(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/takt")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stand {
		t.Error("стенд включился сам, без STAND=staging")
	}
	t.Setenv("STAND", "stagin")
	if _, err := Load(); err == nil {
		t.Error("опечатка в STAND принята молча")
	}
}
