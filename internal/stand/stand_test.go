package stand

import "testing"

// Разбор проверяется на сообщении того вида, что требует CONTRIBUTING,
// и на двух отступлениях от него: без перевода и без «как проверить».
// Отступления не роняют заметку — их показывают с пометкой, поэтому
// разбор обязан отличать «не написано» от «пусто по ошибке разбора».
func TestParseSplitsTheTwoHalvesAndTheCheck(t *testing.T) {
	log := "aaa1\x1f2026-09-22T10:00:00+03:00\x1f" +
		"Personal settings behind the name\n\n" +
		"The name opens a dialog.\n\n" +
		"How to check:\n1. Click your name.\n2. Pick English.\n\n" +
		"--- ru ---\n" +
		"Личные настройки за именем\n\n" +
		"Имя открывает диалог.\n\n" +
		"Как проверить:\n1. Нажмите на имя.\n2. Выберите English.\n" +
		"\x1e\n" +
		"bbb2\x1f2026-09-21T10:00:00+03:00\x1f" +
		"Only English here\n\nNo translation, no check.\n" +
		"\x1e\n" +
		"ccc3\x1f2026-09-20T10:00:00+03:00\x1f" +
		"Build only\n\nHow to check: nothing to see — the build only\n" +
		"\x1e\n"

	got := Parse(log)
	if len(got) != 3 {
		t.Fatalf("коммитов %d, ожидалось 3: %+v", len(got), got)
	}

	full := got[0]
	if full.Hash != "aaa1" || full.Date != "2026-09-22T10:00:00+03:00" {
		t.Errorf("хеш и дата: %q %q", full.Hash, full.Date)
	}
	if full.En.Title != "Personal settings behind the name" ||
		full.En.Body != "The name opens a dialog." ||
		full.En.Check != "1. Click your name.\n2. Pick English." {
		t.Errorf("английская половина: %+v", full.En)
	}
	if full.Ru == nil {
		t.Fatal("русская половина потерялась")
	}
	if full.Ru.Title != "Личные настройки за именем" ||
		full.Ru.Body != "Имя открывает диалог." ||
		full.Ru.Check != "1. Нажмите на имя.\n2. Выберите English." {
		t.Errorf("русская половина: %+v", *full.Ru)
	}

	bare := got[1]
	if bare.Ru != nil {
		t.Errorf("перевода нет, а разбор его нашёл: %+v", *bare.Ru)
	}
	if bare.En.Check != "" || bare.En.Body != "No translation, no check." {
		t.Errorf("без «как проверить»: %+v", bare.En)
	}

	// Метка и текст на одной строке — тоже блок проверки.
	if got[2].En.Check != "nothing to see — the build only" || got[2].En.Body != "" {
		t.Errorf("проверка в одну строку: %+v", got[2].En)
	}
}

func TestEmptyLogIsAnEmptyList(t *testing.T) {
	// Пустой список, а не nil: клиент читает `commits.length`, и `null`
	// в ответе уронил бы экран стенда без коммитов поверх master.
	if got := Parse(""); got == nil || len(got) != 0 {
		t.Errorf("пустой журнал: %#v", got)
	}
}

// Коммит до правила «по-английски с переводом» — только русский:
// он русская половина, а не английская, иначе русскому экрану
// пришлось бы писать над русским текстом «показан английский».
func TestRussianOnlyCommitIsNotTakenForEnglish(t *testing.T) {
	log := "abc\x1f2026-09-20T10:00:00+03:00\x1fМетку заводят прямо с карточки\n\nТело.\n\x1e" +
		"def\x1f2026-09-22T10:00:00+03:00\x1fAn English title\n\nBody.\n\x1e"
	c := Parse(log)
	if len(c) != 2 {
		t.Fatalf("коммитов %d", len(c))
	}
	if !c[0].OnlyRu || c[0].Ru == nil || c[0].Ru.Title != "Метку заводят прямо с карточки" {
		t.Fatalf("русский коммит: %+v", c[0])
	}
	if c[1].OnlyRu || c[1].Ru != nil {
		t.Fatalf("английский без перевода остаётся английским: %+v", c[1])
	}
}
