//go:build load

package httpapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strconv"
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

// Расчётный состав под смешанной нагрузкой (PROMPT-TESTING.md, уровень 7).
//
// Остальные проверки этого файла и доски меряют по одному свойству:
// толпу на одной доске, соседство с тяжёлой, сотню открытых. Здесь —
// обещанный продуктом масштаб целиком: 130 человек в трёх организациях,
// у каждой по две доски на 150 карточек, и каждый в своём потоке читает
// доску, заводит карточку, заглядывает в «Задачи» и пробует чужую доску.
//
// Обещания, которые проверяются вместе, потому что ломаются вместе:
// ни одного ответа 5xx; людям не отвечают 429 (предел частоты — против
// сбежавшего ключа, а не против работы); чужая доска — всегда «не
// найдено», а в списке досок — только свои; медиана операции не дольше
// 50 мс и снимка — 100 мс, 95-й перцентиль — вчетверо от них. Ориентир —
// замер 21.09.2026 (операция 15 мс, снимок 13 мс); порог с запасом,
// чтобы краснела поломка, а не погода.
//
// Темп — LOAD_RATE операций в секунду на всю установку (по умолчанию 30,
// как в плане: 130 человек, у каждого действие раз в четыре секунды).
// Без темпа, «каждый жмёт без пауз», 129 потоков дали 26.09.2026 около
// 265 запросов в секунду при медиане 420 мс — это предел машины, а не
// поведение команды; его стоит знать, но не краснеть на нём.
// Длительность — LOAD_DURATION (по умолчанию минута; к выпуску — 10m).
func TestMixedLoadOfThePlannedCrowdStaysFastAndIsolated(t *testing.T) {
	duration := time.Minute
	if v := os.Getenv("LOAD_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("LOAD_DURATION: %v", err)
		}
		duration = d
	}
	rate := 30.0
	if v := os.Getenv("LOAD_RATE"); v != "" {
		r, err := strconv.ParseFloat(v, 64)
		if err != nil || r <= 0 {
			t.Fatalf("LOAD_RATE: %q", v)
		}
		rate = r
	}
	a := newAPI(t)

	type org struct {
		people []*session
		boards []string
		column map[string]string
	}
	const orgs, members, boardsPerOrg, cards = 3, 42, 2, 150
	all := make([]org, orgs)
	for i := range all {
		owner := a.registerOrg(fmt.Sprintf("Нагрузка %d", i+1))
		o := org{people: []*session{owner}, column: map[string]string{}}
		for b := 0; b < boardsPerOrg; b++ {
			id := owner.board(fmt.Sprintf("Доска %d-%d", i+1, b+1))
			o.boards = append(o.boards, id)
			var snap struct {
				Columns []struct{ ID string } `json:"columns"`
			}
			if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+id, nil, http.StatusOK), &snap); err != nil {
				t.Fatal(err)
			}
			o.column[id] = snap.Columns[0].ID
			for c := 0; c < cards; c++ {
				owner.op(id, uuid.NewString(), "CREATE_CARD",
					map[string]any{"columnId": o.column[id], "title": fmt.Sprintf("Карточка %d", c)})
			}
		}
		for m := 0; m < members; m++ {
			o.people = append(o.people, owner.join("member"))
		}
		all[i] = o
	}

	var (
		mu                    sync.Mutex
		opTimes, snapTimes    []time.Duration
		serverErrors, limited int
		leaks                 []string
	)
	note := func(list *[]time.Duration, d time.Duration) {
		mu.Lock()
		*list = append(*list, d)
		mu.Unlock()
	}
	check := func(code int, raw []byte, what string) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case code >= 500:
			serverErrors++
			if serverErrors <= 3 {
				t.Errorf("%s: %d %s", what, code, raw)
			}
		case code == http.StatusTooManyRequests:
			limited++
		}
	}

	// У каждого своё действие раз в period, начало разнесено: все
	// разом в первую секунду — это не команда, а залп.
	period := time.Duration(float64(orgs*(members+1)) / rate * float64(time.Second))
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	for i, o := range all {
		stranger := all[(i+1)%orgs].boards[0]
		own := map[string]bool{}
		for _, b := range o.boards {
			own[b] = true
		}
		for p, person := range o.people {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Duration(rand.Int64N(int64(period))))
				for n := 0; time.Now().Before(deadline); n++ {
					next := time.Now().Add(period)
					board := o.boards[(p+n)%len(o.boards)]

					start := time.Now()
					code, raw := person.do("GET", "/api/boards/"+board, nil)
					note(&snapTimes, time.Since(start))
					check(code, raw, "снимок")

					start = time.Now()
					code, raw = person.do("POST", "/api/boards/"+board+"/operations", map[string]any{
						"operationId": uuid.NewString(), "type": "CREATE_CARD",
						"payload": map[string]any{"columnId": o.column[board], "title": "Под нагрузкой"},
					})
					note(&opTimes, time.Since(start))
					check(code, raw, "операция")

					if n%5 == 0 {
						code, raw = person.do("GET", "/api/tasks", nil)
						check(code, raw, "задачи")
					}
					if n%10 == 0 {
						if code, _ := person.do("GET", "/api/boards/"+stranger, nil); code != http.StatusNotFound {
							mu.Lock()
							leaks = append(leaks, fmt.Sprintf("чужая доска ответила %d", code))
							mu.Unlock()
						}
						code, raw = person.do("GET", "/api/boards", nil)
						check(code, raw, "список досок")
						var list struct {
							Boards []struct{ ID string } `json:"boards"`
						}
						_ = json.Unmarshal(raw, &list)
						for _, b := range list.Boards {
							if !own[b.ID] {
								mu.Lock()
								leaks = append(leaks, "в списке досок чужая "+b.ID)
								mu.Unlock()
							}
						}
					}
					time.Sleep(time.Until(next))
				}
			}()
		}
	}
	wg.Wait()

	pct := func(list []time.Duration, q float64) time.Duration {
		if len(list) == 0 {
			return 0
		}
		sorted := slices.Clone(list)
		slices.Sort(sorted)
		return sorted[int(float64(len(sorted)-1)*q)]
	}
	people := orgs * (members + 1)
	t.Logf("%d человек, темп %.0f/с, %v: операций %d (медиана %v, 95%% %v), снимков %d (медиана %v, 95%% %v), 5xx %d, 429 %d",
		people, rate, duration, len(opTimes), pct(opTimes, 0.5), pct(opTimes, 0.95),
		len(snapTimes), pct(snapTimes, 0.5), pct(snapTimes, 0.95), serverErrors, limited)

	if len(leaks) > 0 {
		t.Errorf("под нагрузкой видно чужое (%d раз), первое: %s", len(leaks), leaks[0])
	}
	if serverErrors > 0 {
		t.Errorf("ответов 5xx: %d", serverErrors)
	}
	if limited > 0 {
		t.Errorf("людям отвечали 429: %d раз — предел частоты мешает работе", limited)
	}
	for _, c := range []struct {
		what         string
		list         []time.Duration
		median, tail time.Duration
	}{
		{"операция", opTimes, 50 * time.Millisecond, 200 * time.Millisecond},
		{"снимок", snapTimes, 100 * time.Millisecond, 400 * time.Millisecond},
	} {
		if m := pct(c.list, 0.5); m > c.median {
			t.Errorf("%s: медиана %v, порог %v", c.what, m, c.median)
		}
		if p := pct(c.list, 0.95); p > c.tail {
			t.Errorf("%s: 95-й перцентиль %v, порог %v", c.what, p, c.tail)
		}
	}
}
