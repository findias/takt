// upgrade-smoke — дымовая проверка работающего сервера для прогона
// обновления (scripts/upgrade-check.sh, PROMPT-TESTING.md, уровень 1).
//
// Ходит только по тому, что умел прошлый выпуск: вход, «кто я», список
// досок, снимок доски, одна операция записи, состав организации. Этим
// же набором проверяется и старый бинарник на новой схеме, и новый
// на той же базе, и старый после отката — три раза одно и то же,
// чтобы различие было видно как различие, а не как другой сценарий.
//
// Отдельная программа, а не шаг в shell: разбор JSON и cookie в bash
// держится на jq и curl, которых в закрытом контуре может не быть,
// а Go у того, кто собирает проект, есть всегда.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"time"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:8096", "адрес сервера")
	email := flag.String("email", "anna@example.test", "почта владельца демо")
	password := flag.String("password", "parol12345", "пароль")
	label := flag.String("label", "", "как назвать созданную карточку")
	flag.Parse()

	if err := run(*url, *email, *password, *label); err != nil {
		fmt.Fprintln(os.Stderr, "дымовая проверка:", err)
		os.Exit(1)
	}
}

type client struct {
	base string
	http *http.Client
}

func run(base, email, password, label string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	c := client{base: base, http: &http.Client{Jar: jar, Timeout: 20 * time.Second}}

	if err := c.post("/api/auth/login", map[string]string{"email": email, "password": password}, nil); err != nil {
		return fmt.Errorf("вход: %w", err)
	}
	var me struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := c.get("/api/me", &me); err != nil {
		return fmt.Errorf("кто я: %w", err)
	}
	if me.Role != "owner" {
		return fmt.Errorf("после входа роль %q, ждали владельца", me.Role)
	}

	boards, err := c.boards()
	if err != nil {
		return fmt.Errorf("список досок: %w", err)
	}
	if len(boards) == 0 {
		return fmt.Errorf("список досок пуст")
	}

	var snap struct {
		Columns []struct {
			ID string `json:"id"`
		} `json:"columns"`
		Cards []any `json:"cards"`
	}
	if err := c.get("/api/boards/"+boards[0].ID, &snap); err != nil {
		return fmt.Errorf("снимок доски «%s»: %w", boards[0].Name, err)
	}
	if len(snap.Columns) == 0 {
		return fmt.Errorf("у доски «%s» нет колонок", boards[0].Name)
	}

	// Запись — обязательно: чтение работает и на сломанной схеме,
	// пока не тронешь колонку, которой больше нет или которая стала
	// обязательной без умолчания.
	title := "Проверка обновления"
	if label != "" {
		title += " · " + label
	}
	payload, _ := json.Marshal(map[string]any{
		"columnId": snap.Columns[0].ID, "title": title, "place": "end"})
	op := map[string]any{
		"operationId": uuid(),
		"type":        "CREATE_CARD",
		"payload":     json.RawMessage(payload),
	}
	if err := c.post("/api/boards/"+boards[0].ID+"/operations", op, nil); err != nil {
		return fmt.Errorf("создание карточки: %w", err)
	}

	var team struct {
		Members []any `json:"members"`
	}
	if err := c.get("/api/team", &team); err != nil {
		return fmt.Errorf("состав организации: %w", err)
	}
	if len(team.Members) < 2 {
		return fmt.Errorf("в организации %d человек, ждали демо-состав", len(team.Members))
	}

	fmt.Printf("ok: досок %d, на «%s» карточек %d, людей %d\n",
		len(boards), boards[0].Name, len(snap.Cards), len(team.Members))
	return nil
}

type board struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// boards терпит обе формы ответа — объект со списком и голый список:
// форма могла меняться между выпусками, а проверка обязана ходить
// и к старому серверу.
func (c client) boards() ([]board, error) {
	var raw json.RawMessage
	if err := c.get("/api/boards", &raw); err != nil {
		return nil, err
	}
	var wrapped struct {
		Boards []board `json:"boards"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Boards != nil {
		return wrapped.Boards, nil
	}
	var list []board
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("неожиданная форма ответа: %s", trim(raw))
	}
	return list, nil
}

func (c client) get(path string, out any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func (c client) post(path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.base+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func decode(resp *http.Response, out any) error {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, trim(buf.Bytes()))
	}
	if out == nil || buf.Len() == 0 {
		return nil
	}
	if err := json.Unmarshal(buf.Bytes(), out); err != nil {
		return fmt.Errorf("разбор ответа: %w: %s", err, trim(buf.Bytes()))
	}
	return nil
}

// uuid — случайный идентификатор операции: прошлый выпуск принимает
// только UUID, и строка с префиксом отвечала 500 — первое, что поймал
// этот прогон, и поймал в самой проверке.
func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func trim(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}
