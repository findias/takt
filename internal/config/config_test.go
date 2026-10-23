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

// Метка среды — короткое слово в шапке; длинная или многострочная
// сломала бы заголовок вкладки, и отказ лучше при старте, чем на экране.
func TestStandLabelIsShortAndSingleLine(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/takt")
	t.Setenv("STAND_LABEL", "  ИФТ ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.StandLabel != "ИФТ" {
		t.Errorf("метка %q, ожидалась обрезанная «ИФТ»", c.StandLabel)
	}
	t.Setenv("STAND_LABEL", "Предпромышленный стенд банка")
	if _, err := Load(); err == nil {
		t.Error("метка длиннее предела принята молча")
	}
	t.Setenv("STAND_LABEL", "DEV\nIFT")
	if _, err := Load(); err == nil {
		t.Error("метка с переводом строки принята молча")
	}
	t.Setenv("STAND_LABEL", "")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.StandLabel != "" {
		t.Errorf("метка появилась сама: %q", c.StandLabel)
	}
}
