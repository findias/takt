package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/demo"
)

// Отказы, которые клиент различает по коду, вызываются и сверяются
// по коду и статусу (PROMPT-TESTING.md, уровень 3). Каждый из них до
// 26.09.2026 не вызывал ни один тест: код мог не возникать вовсе или
// возникать с другим статусом — и экран, ветвящийся по коду, ломался бы
// молча. Сторож — contract_codes_tested_test.go.

// refused вызывает запрос и сверяет статус и код отказа.
func refused(t *testing.T, s *session, method, path string, body any, status int, code string) {
	t.Helper()
	got, raw := s.do(method, path, body)
	if got != status {
		t.Errorf("%s %s: статус %d, ждали %d; тело: %s", method, path, got, status, raw)
		return
	}
	if c, _ := field(t, raw, "code").(string); c != code {
		t.Errorf("%s %s: код %q, ждали %q; тело: %s", method, path, c, code, raw)
	}
	if msg, _ := field(t, raw, "error").(string); msg == "" {
		t.Errorf("%s %s: отказ без текста: %s", method, path, raw)
	}
}

func TestRefusalsCarryTheirContractCodes(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Коды отказов")
	member := owner.join("member")

	t.Run("люди и приглашения", func(t *testing.T) {
		refused(t, owner, "POST", "/api/invites",
			map[string]any{"email": owner.email, "role": "member"}, http.StatusConflict, "already_member")
		refused(t, member, "POST", "/api/invites",
			map[string]any{"email": "kto-to@example.test", "role": "member"}, http.StatusForbidden, "invite_not_yours")

		gone := owner.team("Убранное", nil)
		owner.mustDo("DELETE", "/api/teams/"+gone, nil, http.StatusNoContent)
		refused(t, owner, "POST", "/api/invites",
			map[string]any{"email": "v-arhiv@example.test", "role": "member", "teamId": gone},
			http.StatusConflict, "team_gone")

		live := owner.team("Живое", nil)
		refused(t, member, "POST", "/api/team-admins",
			map[string]any{"userId": member.userID, "teamId": live}, http.StatusForbidden, "appoint_not_yours")
		refused(t, member, "DELETE", "/api/members/"+owner.userID+"/identity", nil,
			http.StatusForbidden, "erase_not_yours")
		// Общий код по статусу: отказ без своего кода различается хотя бы
		// так, и клиент на это опирается.
		refused(t, owner, "DELETE", "/api/members/"+owner.userID+"/identity", nil,
			http.StatusConflict, "conflict")
		refused(t, owner, "POST", "/api/teams", map[string]any{"name": ""},
			http.StatusBadRequest, "bad_request")
	})

	t.Run("доски и карточки", func(t *testing.T) {
		boardID := owner.board("Коды доски")
		card := owner.cardOn(boardID, "Не трогать")
		refused(t, member, "POST", "/api/boards/"+boardID+"/operations", map[string]any{
			"operationId": uuid.NewString(), "type": "DELETE_CARD",
			"payload": map[string]any{"cardId": card},
		}, http.StatusForbidden, "purge_not_yours")

		owner.mustDo("DELETE", "/api/boards/"+boardID, nil, http.StatusNoContent)
		refused(t, owner, "GET", "/api/boards/"+boardID, nil, http.StatusNotFound, "board_archived")
	})

	t.Run("пароль и срезы", func(t *testing.T) {
		refused(t, owner, "PUT", "/api/me/password",
			map[string]any{"current": "parol12345", "next": "parol12345"}, http.StatusBadRequest, "password_same")

		slice := map[string]any{"name": "Квартал", "query": "from=2026-07-01&to=2026-09-30"}
		owner.mustDo("POST", "/api/reports/slices", slice, http.StatusCreated)
		refused(t, owner, "POST", "/api/reports/slices", slice, http.StatusConflict, "report_slice_taken")
	})

	t.Run("перенос из таблицы", func(t *testing.T) {
		// Пакет переноса вместо таблицы — «не таблица», а не внутренняя ошибка.
		refused(t, owner, "POST", "/api/import/table",
			map[string]any{"file": []byte("PK\x03\x04не книга"), "newBoardName": "Z", "apply": true},
			http.StatusBadRequest, "import_unreadable")
		// Без колонки заголовка предпросмотр подсказывает, а применение
		// отказывает кодом.
		refused(t, owner, "POST", "/api/import/table",
			map[string]any{"file": []byte("Foo,Bar\n1,2\n"), "newBoardName": "Z", "apply": true},
			http.StatusBadRequest, "import_mapping")
	})
}

func TestYougileRefusalsCarryTheirCodes(t *testing.T) {
	fake := newFakeYougile(t)
	a := newAPIWith(t, func(c *config.Config) { c.YougileURL = fake.URL })
	owner := a.registerOrg("YouGile отказывает")
	fake.owner = owner.email
	body := map[string]any{"key": ygKey, "board": ygBoard, "newBoardName": "Склад"}

	refused(t, owner, "POST", "/api/import/yougile",
		map[string]any{"key": ygKey, "board": "b-нет-такой", "newBoardName": "Склад"},
		http.StatusNotFound, "yougile_not_found")

	fake.mu.Lock()
	fake.status = http.StatusTooManyRequests
	fake.mu.Unlock()
	refused(t, owner, "POST", "/api/import/yougile", body, http.StatusServiceUnavailable, "yougile_busy")
}

// Демо: предел песочниц и частота заведения. Песочниц 300 и 10 подряд —
// заводить их по-настоящему минуты, поэтому предел частоты выбирается
// заранее через тот же счётчик, что у сервера, а предел числа — порогом,
// поставленным в нынешнее число песочниц. Порядок важен: полная база
// отказывает раньше, чем считается частота.
func TestDemoRefusalsCarryTheirCodes(t *testing.T) {
	a := newAPIWith(t, func(c *config.Config) { c.Demo = true; c.Signup = config.SignupClosed })
	visitor := a.session()

	for {
		if ok, _ := a.impl.limiter.left(sandboxBucket, sandboxBurst, sandboxPerSec); !ok {
			break
		}
		a.impl.limiter.spend(sandboxBucket, sandboxBurst, sandboxPerSec)
	}
	refused(t, visitor, "POST", "/api/demo/sandbox", nil, http.StatusTooManyRequests, "demo_busy")

	live, err := demo.LiveSandboxes(context.Background(), a.impl.db)
	if err != nil {
		t.Fatal(err)
	}
	restore := sandboxLimit
	sandboxLimit = live
	t.Cleanup(func() { sandboxLimit = restore })
	refused(t, visitor, "POST", "/api/demo/sandbox", nil, http.StatusServiceUnavailable, "demo_full")
}

// Отказ приходит на языке человека (PROMPT-TESTING.md, уровень 3).
//
// TestEveryMessageHasEnglish проверяет, что у каждого отказа есть
// перевод в каталоге; что сервер его отдаёт, не проверял никто. Здесь
// человек выбирает английский, и те же отказы обязаны прийти без единой
// русской буквы — и с тем же кодом, потому что клиент различает по коду.
func TestRefusalsSpeakTheLanguageOfThePerson(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Language of refusals")
	member := owner.join("member")
	for _, s := range []*session{owner, member} {
		s.mustDo("PUT", "/api/me/lang", map[string]any{"lang": "en"}, http.StatusNoContent)
	}
	cyrillic := regexp.MustCompile(`[А-Яа-яЁё]`)
	english := func(s *session, method, path string, body any, status int, code string) {
		t.Helper()
		got, raw := s.do(method, path, body)
		if got != status {
			t.Errorf("%s %s: статус %d, ждали %d; тело: %s", method, path, got, status, raw)
			return
		}
		if c, _ := field(t, raw, "code").(string); c != code {
			t.Errorf("%s %s: код %q, ждали %q", method, path, c, code)
		}
		msg, _ := field(t, raw, "error").(string)
		if msg == "" || cyrillic.MatchString(msg) {
			t.Errorf("%s %s: отказ не по-английски: %q", method, path, msg)
		}
	}

	english(owner, "POST", "/api/invites",
		map[string]any{"email": owner.email, "role": "member"}, http.StatusConflict, "already_member")
	english(member, "POST", "/api/invites",
		map[string]any{"email": "someone@example.test", "role": "member"}, http.StatusForbidden, "invite_not_yours")
	gone := owner.team("Archived", nil)
	owner.mustDo("DELETE", "/api/teams/"+gone, nil, http.StatusNoContent)
	english(owner, "POST", "/api/invites",
		map[string]any{"email": "late@example.test", "role": "member", "teamId": gone}, http.StatusConflict, "team_gone")
	live := owner.team("Live", nil)
	english(member, "POST", "/api/team-admins",
		map[string]any{"userId": member.userID, "teamId": live}, http.StatusForbidden, "appoint_not_yours")
	english(member, "DELETE", "/api/members/"+owner.userID+"/identity", nil, http.StatusForbidden, "erase_not_yours")
	english(owner, "PUT", "/api/me/password",
		map[string]any{"current": "parol12345", "next": "parol12345"}, http.StatusBadRequest, "password_same")

	boardID := owner.board("Refusals board")
	card := owner.cardOn(boardID, "Keep")
	english(member, "POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(), "type": "DELETE_CARD",
		"payload": map[string]any{"cardId": card},
	}, http.StatusForbidden, "purge_not_yours")
	owner.mustDo("DELETE", "/api/boards/"+boardID, nil, http.StatusNoContent)
	english(owner, "GET", "/api/boards/"+boardID, nil, http.StatusNotFound, "board_archived")

	slice := map[string]any{"name": "Quarter", "query": "from=2026-07-01&to=2026-09-30"}
	owner.mustDo("POST", "/api/reports/slices", slice, http.StatusCreated)
	english(owner, "POST", "/api/reports/slices", slice, http.StatusConflict, "report_slice_taken")
	english(owner, "POST", "/api/import/table",
		map[string]any{"file": []byte("Foo,Bar\n1,2\n"), "newBoardName": "Z", "apply": true},
		http.StatusBadRequest, "import_mapping")
}
