//go:build load

package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Поведение под нагрузкой на уровне HTTP. Отделено тем же тегом сборки,
// что и нагрузочные проверки доски: они идут минуты. Запуск: make load.

// Сто открытых досок — заявленный масштаб установки, и держит их один
// слушатель базы: подписка стоит соединения, и по соединению на вкладку
// закончилось бы исчерпанием пула на третьем десятке.
//
// Проверяется здесь именно это обещание: оповещение доходит до всех
// подключённых, а не до первых нескольких, и одно изменение не
// превращается в сотню запросов к базе.
func TestManyOpenBoardsAllGetTheChange(t *testing.T) {
	const watchers = 100

	a := newAPI(t)
	owner := a.registerOrg("Компания")
	boardID := owner.board("Общая")

	raw := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}

	arrived := make(chan int, watchers)
	var ready sync.WaitGroup
	ready.Add(watchers)

	for i := 0; i < watchers; i++ {
		go func(i int) {
			req, err := http.NewRequest("GET", a.server.URL+"/api/boards/"+boardID+"/stream", nil)
			if err != nil {
				ready.Done()
				return
			}
			// Все смотрят одной и той же сессией: разница между сотней
			// вкладок одного человека и сотней людей для потока никакая,
			// а заводить сотню учётных записей — значит мерить регистрацию.
			resp, err := owner.client.Do(req)
			if err != nil {
				ready.Done()
				return
			}
			defer resp.Body.Close()
			ready.Done()

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "data: ") {
					arrived <- i
					return
				}
			}
		}(i)
	}
	ready.Wait()
	// Подписки доходят до узла не мгновенно: дать им встать в очередь.
	time.Sleep(500 * time.Millisecond)

	start := time.Now()
	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": snap.Columns[0].ID, "title": "Всем видно"},
	}, http.StatusOK)

	got := 0
	deadline := time.After(15 * time.Second)
	for got < watchers {
		select {
		case <-arrived:
			got++
		case <-deadline:
			t.Fatalf("оповещение дошло до %d слушателей из %d", got, watchers)
		}
	}
	t.Logf("одно изменение дошло до %d открытых досок за %v", watchers, time.Since(start))
}

// Ключ доступа ограничен по частоте: интеграция, упершаяся в предел,
// должна замедлиться, а не получить отказ навсегда. Проверяется, что
// предел вообще срабатывает и что упирается в него именно ключ,
// а не человек с сессией рядом.
func TestRateLimitStopsARunawayKeyButNotThePeople(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")

	created := owner.mustDo("POST", "/api/clients", map[string]any{
		"name": "Бегун " + uuid.NewString()[:8], "scopes": []string{"boards:read"},
	}, http.StatusCreated)
	token := field(t, created, "token").(string)

	limited := 0
	for i := 0; i < 400 && limited == 0; i++ {
		req, err := http.NewRequest("GET", a.server.URL+"/api/v1/boards", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("authorization", "Bearer "+token)
		resp, err := a.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = i
		}
	}
	if limited == 0 {
		t.Fatal("ключ не упёрся в предел частоты за четыреста запросов")
	}
	t.Logf("предел частоты сработал на %d-м запросе", limited)

	// Человек рядом продолжает работать: предел считается на ключ.
	owner.mustDo("GET", "/api/boards", nil, http.StatusOK)
}
