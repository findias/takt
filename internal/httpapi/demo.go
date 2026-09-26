package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/findias/takt/internal/demo"
)

// Публичное демо (ROADMAP 30.1): посетитель нажимает «Попробовать»
// и получает свою организацию с демонстрационными данными на сутки.
//
// Ручка существует только при DEMO=on: у установки заказчика её нет
// вовсе, а не «есть, но отказывает» — отказывающая ручка всё равно
// сообщает, что где-то бывает демо, и её пробуют.

// SandboxTTL — сколько живёт песочница. Сутки: достаточно, чтобы
// вернуться к ней после совещания, и недостаточно, чтобы бесплатная
// база превратилась в чьё-то рабочее хранилище.
const SandboxTTL = 24 * time.Hour

// Живых песочниц одновременно. Одна занимает около 190 КБ таблиц
// и индексов (замер на двадцати, 21.09.2026), бесплатная база —
// полгигабайта: триста песочниц — около 60 МБ, с большим запасом
// на то, что посетители в них наработают. Переменная, а не константа:
// проверка отказа demo_full ставит порог в нынешнее число песочниц —
// заводить их три сотни в тесте незачем.
var sandboxLimit = 300

const (
	// Новых — не больше тридцати в час на всех, с запасом в десять
	// подряд. Общий счёт, а не по адресу: адреса за прокси мы не знаем
	// (см. attempts.go), а по заголовку, который пишет кто угодно,
	// предел обходится сменой строки.
	sandboxBurst  = 10.0
	sandboxPerSec = 30.0 / 3600
	sandboxBucket = "демо:песочницы"
)

func (s *Server) registerDemoRoutes(mux *http.ServeMux) {
	if !s.cfg.Demo {
		return
	}
	mux.HandleFunc("POST /api/demo/sandbox", s.handleSandbox)
}

func (s *Server) handleSandbox(w http.ResponseWriter, r *http.Request) {
	live, err := demo.LiveSandboxes(r.Context(), s.db)
	if err != nil {
		s.fail(w, "счёт песочниц", err)
		return
	}
	if live >= sandboxLimit {
		writeCoded(w, http.StatusServiceUnavailable, "demo_full",
			"демо сейчас заполнено — попробуйте через час, освободится место")
		return
	}
	if ok, after := s.limiter.left(sandboxBucket, sandboxBurst, sandboxPerSec); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(after))
		writeCoded(w, http.StatusTooManyRequests, "demo_busy",
			"слишком много новых песочниц подряд — попробуйте через "+minutes(after))
		return
	}
	s.limiter.spend(sandboxBucket, sandboxBurst, sandboxPerSec)

	box, err := demo.FillSandbox(r.Context(), s.db, SandboxTTL)
	if err != nil {
		s.fail(w, "заведение песочницы", err)
		return
	}
	s.startSession(w, r, box.OwnerID)
}
