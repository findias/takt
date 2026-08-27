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

// База по сети без TLS обязана быть названа. Не поломкой — сеть бывает
// доверенной, — но названа: молчание здесь читается как «всё в порядке»,
// и именно так строка из руководства, написанная для локального сокета,
// доезжает до базы в соседней стойке.
func TestDatabaseOverTheNetworkWithoutTLSIsNamed(t *testing.T) {
	db := testdb.Shared(t)
	cfg := config.Config{
		BaseURL:     "https://takt.example.test",
		DatabaseURL: "postgres://takt:s3cretpass@db.example.test:5432/takt?sslmode=disable",
	}

	связь := итог(t, Осмотр(context.Background(), cfg, db), "связь с базой")
	if !связь.Ладно {
		t.Error("незащищённая связь объявлена поломкой: решает это тот, кто ставил")
	}
	if !strings.Contains(связь.Ответ, "открыто") || связь.Совет == "" {
		t.Errorf("про открытый трафик не сказано или не сказано, что делать: %+v", связь)
	}
	if strings.Contains(связь.Ответ, "s3cretpass") || strings.Contains(связь.Совет, "s3cretpass") {
		t.Errorf("пароль базы попал в вывод осмотра: %+v", связь)
	}
}

// verify-full — то, чего от установки в бою и ждут: ни жалобы, ни совета.
func TestVerifiedDatabaseConnectionIsQuiet(t *testing.T) {
	db := testdb.Shared(t)
	cfg := config.Config{
		BaseURL:     "https://takt.example.test",
		DatabaseURL: "postgres://takt@db.example.test:5432/takt?sslmode=verify-full",
	}

	связь := итог(t, Осмотр(context.Background(), cfg, db), "связь с базой")
	if !связь.Ладно || связь.Совет != "" {
		t.Errorf("проверенная связь вызвала замечание: %+v", связь)
	}
}

// База рядом — не повод для замечания: трафик не выходит за машину.
func TestLocalDatabaseNeedsNoTLSAdvice(t *testing.T) {
	db := testdb.Shared(t)
	cfg := config.Config{
		BaseURL:     "https://takt.example.test",
		DatabaseURL: "postgres://takt:takt@localhost:5432/takt?sslmode=disable",
	}

	связь := итог(t, Осмотр(context.Background(), cfg, db), "связь с базой")
	if связь.Совет != "" {
		t.Errorf("локальной базе выдан совет про TLS: %+v", связь)
	}
}

// Открытая регистрация — тоже не поломка, но её называют: на адресе,
// доступном всей компании, она означает чужие организации рядом.
func TestOpenSignupIsNamed(t *testing.T) {
	db := testdb.Shared(t)
	cfg := config.Config{BaseURL: "https://takt.example.test", Signup: config.SignupOpen}

	кто := итог(t, Осмотр(context.Background(), cfg, db), "кто заводит организации")
	if !кто.Ладно {
		t.Error("открытая регистрация объявлена поломкой")
	}
	if !strings.Contains(кто.Ответ, "всякий") || кто.Совет == "" {
		t.Errorf("про открытую регистрацию сказано невнятно: %+v", кто)
	}
}
