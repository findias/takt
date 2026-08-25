package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/store/testdb"
)

// Проверка установки ценна ровно тем, что краснеет на сломанной
// установке. Зелёная всегда — это не проверка, а надпись.

func итог(t *testing.T, итоги []Итог, что string) Итог {
	t.Helper()
	for _, и := range итоги {
		if и.Что == что {
			return и
		}
	}
	t.Fatalf("проверки %q в осмотре нет", что)
	return Итог{}
}

func TestHealthyInstallLooksHealthy(t *testing.T) {
	db := testdb.Shared(t)
	cfg := config.Config{BaseURL: "https://takt.example.test"}

	итоги := Осмотр(context.Background(), cfg, db)
	if !Ладно(итоги) {
		for _, и := range итоги {
			if !и.Ладно {
				t.Errorf("на рабочей установке краснеет %q: %s", и.Что, и.Ответ)
			}
		}
	}
	// Схема — та, что вшита в этот бинарник: проверка сверяет имена,
	// а не считает строки.
	if ответ := итог(t, итоги, "схема базы").Ответ; !strings.Contains(ответ, "применены все") {
		t.Errorf("схема: %q", ответ)
	}
}

func TestBrokenAddressIsCalledOut(t *testing.T) {
	db := testdb.Shared(t)

	// Адрес, который не разбирается: из него собираются и ссылки
	// в приглашениях, и адрес потока изменений.
	итоги := Осмотр(context.Background(), config.Config{BaseURL: "board.example.test"}, db)
	адрес := итог(t, итоги, "адрес (BASE_URL)")
	if адрес.Ладно {
		t.Error("адрес без схемы принят за рабочий")
	}
	if адрес.Совет == "" {
		t.Error("отказ не говорит, что делать")
	}
}

func TestInsecureAddressIsAllowedButNamed(t *testing.T) {
	db := testdb.Shared(t)

	// http — не поломка: так ставят стенд. Но сказать об этом надо,
	// потому что cookie в этом случае не защищённая.
	итоги := Осмотр(context.Background(), config.Config{BaseURL: "http://localhost:8080"}, db)
	адрес := итог(t, итоги, "адрес (BASE_URL)")
	if !адрес.Ладно {
		t.Error("стендовый адрес объявлен поломкой")
	}
	if !strings.Contains(адрес.Ответ, "Secure") {
		t.Errorf("про защищённость cookie не сказано: %q", адрес.Ответ)
	}
}

func TestUnreachableProviderIsCalledOut(t *testing.T) {
	db := testdb.Shared(t)

	// Провайдер настроен, но отвечает отказом: кнопка на экране входа
	// будет, а за ней — ничего, и выяснит это первый, кто ею
	// воспользуется.
	отказ := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer отказ.Close()

	cfg := config.Config{BaseURL: "https://takt.example.test"}
	cfg.OIDC.Issuer = отказ.URL
	cfg.OIDC.ClientID = "board"

	провайдер := итог(t, Осмотр(context.Background(), cfg, db), "корпоративный вход")
	if провайдер.Ладно {
		t.Error("недостижимый провайдер объявлен рабочим")
	}
	if !strings.Contains(провайдер.Ответ, "404") {
		t.Errorf("ответ не называет, чем именно ответил провайдер: %q", провайдер.Ответ)
	}
}

func TestMissingProviderIsNotAFailure(t *testing.T) {
	db := testdb.Shared(t)

	// Ненастроенный провайдер — обычный случай, а не поломка:
	// вход по паролю работает и без него.
	провайдер := итог(t,
		Осмотр(context.Background(), config.Config{BaseURL: "https://b.test"}, db),
		"корпоративный вход")
	if !провайдер.Ладно {
		t.Errorf("вход по паролю объявлен поломкой: %s", провайдер.Ответ)
	}
}
