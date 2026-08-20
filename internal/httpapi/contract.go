package httpapi

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/auth"
)

// Контракт для тех, кто подключается снаружи.
//
// Внутри приложения контракта нет и быть не должно: браузерный клиент
// едет в одном образе с сервером и меняется вместе с ним. А интеграция
// живёт отдельно, обновляется когда захочет её владелец и имеет право
// на обещания: адрес с версией, предсказуемые коды ошибок, известные
// пределы частоты и безопасный повтор.

// Version — версия внешнего контракта. Меняется только когда ломается
// совместимость; добавление полей ломающим изменением не считается,
// и клиент обязан переживать незнакомые поля.
const Version = "v1"

// versioned пропускает /api/v1/… внутрь как /api/…
//
// Внутренние пути остаются без версии намеренно: свой же клиент не должен
// делать вид, что он посторонний. Версия — обещание чужим.
func versioned(next http.Handler) http.Handler {
	prefix := "/api/" + Version + "/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, prefix) {
			r = r.Clone(r.Context())
			r.URL.Path = "/api/" + strings.TrimPrefix(r.URL.Path, prefix)
			w.Header().Set("API-Version", Version)
		}
		next.ServeHTTP(w, r)
	})
}

// --- ограничение частоты ---

// Предел на ключ: интеграция, упершаяся в него, должна замедлиться,
// а не получить отказ навсегда. Считается ведром с постоянным доливом:
// короткие всплески проходят, ровный перебор — нет.
const (
	rateBurst  = 120 // столько запросов подряд можно
	ratePerSec = 2.0 // и столько доливается каждую секунду
	rateForget = 30 * time.Minute

	// Каталогу ведро своё и просторнее. Синхронизация по своей природе
	// идёт пачкой: провайдер приходит раз в сутки и приносит всех
	// разом, по человеку на запрос. Общий предел растянул бы заведение
	// сотни сотрудников на минуту с отказами посередине — а предел
	// заведён не против этого, а против интеграции, которая долбится
	// в цикле.
	scimBurst  = 600
	scimPerSec = 20.0
)

type bucket struct {
	tokens float64
	seen   time.Time
}

// limiter живёт в памяти процесса — как и весь остальной сервер: у нас
// один процесс на установку, и отдельное хранилище ради счётчика было бы
// лишней движущейся частью.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

func newLimiter() *limiter { return &limiter{buckets: map[string]*bucket{}} }

// allow отвечает, пропускать ли запрос, сколько запросов осталось
// и через сколько секунд запас снова будет полон.
//
// «Полон», а не «можно будет послать следующий»: ведро доливается
// непрерывно, окна у него нет, и честно назвать можно только момент,
// когда запас вернётся к обещанному пределу. На вопрос «когда мне
// повторить» отвечает Retry-After — он приходит вместе с отказом,
// и там этот вопрос и задают.
func (l *limiter) allow(key string, burst float64, perSec float64) (bool, int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.refill(key, burst, perSec)
	if b.tokens < 1 {
		return false, 0, resetIn(burst, b.tokens, perSec)
	}
	// Отданный запрос вычитается сразу: заголовки описывают состояние
	// после этого запроса, а не до него.
	b.tokens--
	return true, int(b.tokens), resetIn(burst, b.tokens, perSec)
}

// refill доливает ведро до текущего момента и возвращает его.
// Вызывается под замком.
func (l *limiter) refill(key string, burst, perSec float64) *bucket {
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: burst, seen: now}
		l.buckets[key] = b
	}
	b.tokens = min(burst, b.tokens+now.Sub(b.seen).Seconds()*perSec)
	b.seen = now

	// Заброшенные вёдра выкидываем здесь же: отдельный сборщик ради
	// десятка ключей на организацию — лишняя движущаяся часть.
	for k, old := range l.buckets {
		if now.Sub(old.seen) > rateForget {
			delete(l.buckets, k)
		}
	}
	return b
}

// left отвечает, осталась ли хоть одна попытка, и через сколько секунд
// появится следующая. Ничего не тратит: у входа тратит не сам запрос,
// а его неудача.
func (l *limiter) left(key string, burst, perSec float64) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, burst, perSec)
	if b.tokens >= 1 {
		return true, 0
	}
	return false, int(math.Ceil((1 - b.tokens) / perSec))
}

// spend тратит одну попытку.
func (l *limiter) spend(key string, burst, perSec float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, burst, perSec)
	b.tokens = max(0, b.tokens-1)
}

// forget снимает счёт: вспомнивший пароль — не подбиральщик.
func (l *limiter) forget(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// resetIn — через сколько секунд запас снова будет полон.
func resetIn(burst, tokens, perSec float64) int {
	return int(math.Ceil((burst - tokens) / perSec))
}

// limited ограничивает частоту запросов сервисных клиентов. Человек
// за браузером под это ограничение не попадает: он и не может настучать
// быстрее, чем нажимает. Перебор пароля выглядит именно так — редко
// и вежливо, — и считается отдельно, см. attempts.go.
func (s *Server) limited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		// Каталог считается отдельно и по своим числам: ведро у него
		// своё, и общий предел не должен обрывать синхронизацию на
		// середине списка сотрудников.
		burst, perSec, key := float64(rateBurst), float64(ratePerSec), token
		if strings.HasPrefix(r.URL.Path, "/scim/v2/") {
			burst, perSec, key = scimBurst, scimPerSec, "scim:"+token
		}

		allowed, left, reset := s.limiter.allow(key, burst, perSec)
		w.Header().Set("RateLimit-Limit", strconv.Itoa(int(burst)))
		w.Header().Set("RateLimit-Remaining", strconv.Itoa(left))
		// Сколько ждать до полного запаса. Отвечает на «как мне себя
		// вести», тогда как Retry-After ниже — на «когда повторить».
		w.Header().Set("RateLimit-Reset", strconv.Itoa(reset))
		if !allowed {
			// Секунда — время, за которое доливается больше одного
			// запроса. Просить подождать дольше незачем.
			w.Header().Set("Retry-After", "1")
			writeCoded(w, http.StatusTooManyRequests, "too_many_requests",
				"слишком часто, попробуйте через секунду")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- безопасный повтор ---

// withIdempotency отдаёт запомненный ответ, если тот же ключ повтора уже
// приходил, и запоминает ответ, если не приходил.
//
// Только для сервисных клиентов: у человека за браузером повтор — это
// нажатие кнопки, и там своя логика. Интеграция же обязана уметь
// повторять, не спрашивая разрешения: ответ теряется в сети регулярно,
// и «попробую ещё раз» не должно означать «заведу вторую доску».
func (s *Server) withIdempotency(
	w http.ResponseWriter, r *http.Request, p auth.Principal,
	next func(http.ResponseWriter, *http.Request, auth.Principal),
) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || r.Method == http.MethodGet || r.Method == http.MethodHead {
		next(w, r, p)
		return
	}

	// Тело читается здесь и возвращается обработчику: сверять повтор
	// по методу и пути мало — один и тот же адрес принимает разные
	// запросы, и «заведи „Найм“» с «заведи „Продажи“» под одним ключом
	// нельзя считать одним и тем же.
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(body))

	stored, err := s.replay(r, p, key)
	if err != nil {
		s.fail(w, "ключ повтора", err)
		return
	}
	if stored != nil {
		// Ключ, предъявленный к другому вызову, — ошибка клиента,
		// а не просьба вернуть прошлое: молча отдать чужой ответ значило
		// бы соврать.
		if stored.method != r.Method || stored.path != r.URL.Path {
			writeCoded(w, http.StatusConflict, "idempotency_key_reused",
				"этот ключ повтора уже использован для другого запроса")
			return
		}
		// Пустой отпечаток — запись, заведённая до того, как отпечатки
		// появились. Она доживает свои сутки без сверки: отказывать
		// на ровном месте хуже, чем один раз не проверить.
		if stored.fingerprint != "" && stored.fingerprint != fingerprint {
			writeCoded(w, http.StatusConflict, "idempotency_key_reused",
				"этот ключ повтора уже использован с другим телом запроса")
			return
		}
		w.Header().Set("Idempotent-Replay", "true")
		w.Header().Set("content-type", "application/json; charset=utf-8")
		w.WriteHeader(stored.status)
		_, _ = w.Write(stored.body)
		return
	}

	recorder := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
	next(recorder, r, p)

	// Запоминается только удавшееся: отказ повторять не только можно,
	// но и нужно — он мог быть вызван временной причиной.
	if recorder.status < 200 || recorder.status >= 300 {
		return
	}
	if err := s.remember(r, p, key, fingerprint, recorder.status, recorder.body); err != nil {
		s.log.Error("ключ повтора не сохранён", "ключ", key, "err", err)
	}
}

// readBody забирает тело целиком и возвращает его на место: дальше
// по цепочке его будет читать обработчик, и прочитанное однажды тело
// достанется ему пустым.
//
// Предел тот же, что у разбора тела, и по той же причине: без него
// чужой запрос решает, сколько нам занять памяти.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return nil, true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "тело запроса слишком велико или оборвалось")
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, true
}

type storedResponse struct {
	method string
	path   string
	// fingerprint — sha256 от тела первого запроса. Пусто у записей,
	// заведённых до появления отпечатков.
	fingerprint string
	status      int
	body        []byte
}

func (s *Server) replay(r *http.Request, p auth.Principal, key string) (*storedResponse, error) {
	var found *storedResponse
	err := s.db.InTenant(r.Context(), p.OrgID, p.ID, func(tx pgx.Tx) error {
		var out storedResponse
		var body []byte
		var fingerprint *string
		err := tx.QueryRow(r.Context(), `
			select method, path, fingerprint, status, body
			  from api_idempotency where key = $1`, key).
			Scan(&out.method, &out.path, &fingerprint, &out.status, &body)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if fingerprint != nil {
			out.fingerprint = *fingerprint
		}
		out.body = body
		found = &out
		return nil
	})
	return found, err
}

func (s *Server) remember(
	r *http.Request, p auth.Principal, key, fingerprint string, status int, body []byte,
) error {
	return s.db.InTenant(r.Context(), p.OrgID, p.ID, func(tx pgx.Tx) error {
		// Гонка двух одинаковых запросов разрешается в пользу первого:
		// второй просто не запомнится, а его собственный ответ уже ушёл.
		_, err := tx.Exec(r.Context(), `
			insert into api_idempotency (org_id, key, method, path, fingerprint, status, body)
			values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (org_id, key) do nothing`,
			p.OrgID, key, r.Method, r.URL.Path, fingerprint, status, string(body))
		return err
	})
}

// recordingWriter запоминает ответ, продолжая отдавать его наружу:
// повтор обязан вернуть ровно то же, что вернул первый вызов.
type recordingWriter struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (w *recordingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return w.ResponseWriter.Write(b)
}

// Flush пропускает сброс буфера дальше — по той же причине, что и у
// обёртки для логов: запись ответа не должна мешать потоковым ответам.
func (w *recordingWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
