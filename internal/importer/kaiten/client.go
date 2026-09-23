// Package kaiten — выгрузка досок Kaiten в пакет переноса для takt-fetch
// (ROADMAP 23.4, 23.8).
//
// Kaiten — облачный (`компания.kaiten.ru`) и коробочный, на своём
// адресе. API у них один: REST `/api/latest/…` с токеном из профиля
// в заголовке Bearer. Коробку чаще всего держат в том же закрытом
// контуре, куда переезжают, и тогда выгрузчик запускают рядом с ней —
// периметр не пересекается вовсе (путь Г из 23.8).
//
// Доска Kaiten живёт в пространстве; у неё колонки, у колонок бывают
// подколонки, а поперёк идут дорожки. Карточка лежит в колонке
// (или подколонке) и на дорожке.
//
// Устройство API сверено с developers.kaiten.ru 23.09.2026; на настоящем
// Kaiten не проверено — нет учётной записи; проверка идёт против
// поддельного сервера с теми же ответами.
package kaiten

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Отказы, которые объясняют, что делать.
var (
	ErrUnreachable = errors.New("Kaiten не отвечает — проверьте подключение к интернету и адрес --url")
	ErrDenied      = errors.New("Kaiten не принял токен — проверьте KAITEN_TOKEN: его выдают в профиле Kaiten, раздел «API-ключ»")
	ErrForbidden   = errors.New("Kaiten не пускает к этой доске — у учётной записи нет доступа к её пространству")
	ErrBusy        = errors.New("Kaiten просит подождать: слишком много запросов — повторите через несколько минут")
	ErrOverloaded  = errors.New("Kaiten сейчас не справляется — повторите через несколько минут")
	ErrNotFound    = errors.New("в Kaiten нет такой доски — выберите доску из списка")
	ErrNotKaiten   = errors.New("адрес отвечает, но это не Kaiten — проверьте --url: нужен адрес вида https://компания.kaiten.ru")
)

const (
	maxResponse = 32 << 20
	// patience — сколько раз повторять на «подождите»: паузы растут
	// вдвое, 1+2+…+64 — около двух минут на один запрос.
	patience = 7
	// page — сколько Kaiten отдаёт за раз; больше он не даёт.
	page = 100
)

// Client ходит в Kaiten.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
	// wait — как ждать; подменяется в проверках, чтобы не спать.
	wait func(time.Duration)
}

// New — клиент с разумным сроком на запрос.
func New(base, token string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"), Token: token,
		HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.try(ctx, path, query, out, 0)
}

func (c *Client) try(ctx context.Context, path string, query url.Values, out any, attempt int) error {
	target := c.Base + "/api/latest" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnreachable
	}
	defer resp.Body.Close()
	// Kaiten считает запросы в секунду и отвечает 429, называя в
	// X-RateLimit-Reset, когда счёт обнулится. Пережидается: за ним
	// минуты уже сделанной выгрузки.
	if busy(resp.StatusCode) && attempt < patience {
		pause := time.Duration(1<<attempt) * time.Second
		if s, ok := retryAfter(resp.Header); ok {
			pause = s
		}
		wait := c.wait
		if wait == nil {
			wait = time.Sleep
		}
		wait(pause)
		return c.try(ctx, path, query, out, attempt+1)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrBusy
	case busy(resp.StatusCode):
		return ErrOverloaded
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrDenied
	case resp.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 300:
		return fmt.Errorf("Kaiten ответил %d на %s", resp.StatusCode, path)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return ErrUnreachable
	}
	if len(raw) > maxResponse {
		return fmt.Errorf("Kaiten ответил на %s больше чем %d МБ — это не ответ Kaiten", path, maxResponse>>20)
	}
	// Страница входа или прокси вместо JSON — не та система по адресу.
	if err := json.Unmarshal(raw, out); err != nil {
		return ErrNotKaiten
	}
	return nil
}

// retryAfter — сколько ждать по заголовкам ответа: Retry-After
// в секундах или X-RateLimit-Reset — момент обнуления счёта (секунды
// эпохи). Больше двух минут не ждём: такое число — не пауза, а ошибка.
func retryAfter(h http.Header) (time.Duration, bool) {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 && s <= 120 {
		return time.Duration(s) * time.Second, true
	}
	if at, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if d := time.Until(time.Unix(at, 0)); d > 0 && d <= 2*time.Minute {
			return d.Round(time.Second) + time.Second, true
		}
	}
	return 0, false
}

func busy(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// BoardInfo — доска в списке.
type BoardInfo struct {
	ID    string
	Title string
	Space string
}

// Boards — доски всех пространств, которые учётной записи видно.
func (c *Client) Boards(ctx context.Context) ([]BoardInfo, error) {
	type space struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	var spaces []space
	for offset := 0; ; offset += page {
		var got []space
		q := url.Values{"limit": {strconv.Itoa(page)}, "offset": {strconv.Itoa(offset)}}
		if err := c.get(ctx, "/spaces", q, &got); err != nil {
			return nil, err
		}
		spaces = append(spaces, got...)
		if len(got) < page || len(spaces) > 10000 {
			break
		}
	}
	var out []BoardInfo
	seen := map[int64]bool{}
	for _, s := range spaces {
		var boards []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		}
		if err := c.get(ctx, "/spaces/"+strconv.FormatInt(s.ID, 10)+"/boards", nil, &boards); err != nil {
			// Пространство без доступа не роняет список: его доски
			// просто не видны, как и в самом Kaiten.
			if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		for _, b := range boards {
			// Одна доска бывает в нескольких пространствах.
			if seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			out = append(out, BoardInfo{ID: strconv.FormatInt(b.ID, 10), Title: strings.TrimSpace(b.Title), Space: strings.TrimSpace(s.Title)})
		}
	}
	return out, nil
}
