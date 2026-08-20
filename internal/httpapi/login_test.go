package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Перебор пароля. Проверяется не «есть ограничитель», а то, ради чего
// он заведён: подбор выдыхается, а человек, путающий пароли, работать
// продолжает.

// login шлёт попытку входа и возвращает код, тело и заголовок Retry-After.
func (a *api) login(email, password string) (int, []byte, string) {
	a.t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		a.t.Fatal(err)
	}
	resp, err := a.client.Post(a.server.URL+"/api/auth/login",
		"application/json", bytes.NewReader(body))
	if err != nil {
		a.t.Fatalf("вход: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, resp.Header.Get("Retry-After")
}

func TestPasswordGuessingRunsOutOfAttempts(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания с паролем")

	for i := 1; i <= 10; i++ {
		code, raw, _ := a.login(owner.email, "не тот пароль")
		if code != http.StatusUnauthorized {
			t.Fatalf("попытка %d: код %d, ожидался 401; тело: %s", i, code, raw)
		}
	}

	code, raw, retry := a.login(owner.email, "не тот пароль")
	if code != http.StatusTooManyRequests {
		t.Fatalf("одиннадцатая попытка: код %d, ожидался 429; тело: %s", code, raw)
	}
	// Клиент различает случаи по коду, а не по тексту.
	if got, _ := field(t, raw, "code").(string); got != "too_many_attempts" {
		t.Errorf("код отказа %q, ожидался too_many_attempts; тело: %s", got, raw)
	}
	// Отказ обязан сказать, когда повторять: без этого человеку остаётся
	// только жать ещё, а именно это и надо прекратить.
	if retry == "" {
		t.Error("отказ не назвал, через сколько повторять")
	}

	// Главное: верный пароль тоже не пускают. Иначе перебор, нашедший
	// пароль на исходе ведра, входит как ни в чём не бывало.
	if code, raw, _ := a.login(owner.email, "parol12345"); code != http.StatusTooManyRequests {
		t.Errorf("верный пароль после исчерпания попыток: код %d, ожидался 429; тело: %s", code, raw)
	}
}

// Счёт ведётся по адресу, а не по всему входу: подбор к одному адресу
// не должен запирать вход остальным.
func TestGuessingOneAccountDoesNotLockOthers(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	second := a.registerOrg("Вторая")

	for i := 0; i < 12; i++ {
		a.login(first.email, "не тот пароль")
	}
	if code, raw, _ := a.login(second.email, "parol12345"); code != http.StatusOK {
		t.Errorf("сосед по системе не вошёл: код %d, тело: %s", code, raw)
	}
}

// Успешный вход обнуляет счёт: иначе забывчивый копил бы неудачи неделями
// и однажды не вошёл бы с первого раза.
func TestSuccessfulLoginForgetsFailures(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Забывчивые")

	for i := 0; i < 8; i++ {
		a.login(owner.email, "не тот пароль")
	}
	if code, _, _ := a.login(owner.email, "parol12345"); code != http.StatusOK {
		t.Fatalf("верный пароль на девятой попытке: код %d", code)
	}
	for i := 1; i <= 10; i++ {
		if code, raw, _ := a.login(owner.email, "не тот пароль"); code != http.StatusUnauthorized {
			t.Fatalf("после успешного входа попытка %d: код %d, ожидался 401; тело: %s",
				i, code, raw)
		}
	}
}

// Отказ по исчерпанию не должен выдавать, заведён ли адрес: иначе
// старание не выдавать это временем ответа теряет смысл.
func TestAttemptLimitTellsNothingAboutTheAccount(t *testing.T) {
	a := newAPI(t)
	unknown := "нет-такого-" + uuid.NewString()[:8] + "@example.test"

	for i := 1; i <= 10; i++ {
		if code, raw, _ := a.login(unknown, "любой"); code != http.StatusUnauthorized {
			t.Fatalf("попытка %d к незаведённому адресу: код %d; тело: %s", i, code, raw)
		}
	}
	if code, raw, _ := a.login(unknown, "любой"); code != http.StatusTooManyRequests {
		t.Errorf("незаведённый адрес не упёрся в предел: код %d; тело: %s", code, raw)
	}
}
