// Package jira — выгрузка досок Jira в пакет переноса для takt-fetch
// (ROADMAP 23.4, 23.8).
//
// Две Jira, и различает их вход, а не флаг:
//
//   - облако (Jira Cloud) — почта и токен API, REST v3; задачи ищутся
//     через search/jql постранично по nextPageToken — прежний /search
//     в облаке снят, и писать на него значило бы сломаться сразу;
//   - своя установка (Jira Data Center, Server) — личный токен (PAT),
//     REST v2, /search со startAt. Её чаще всего держат в том же
//     закрытом контуре, куда переезжают, и тогда выгрузчик запускают
//     рядом с ней: периметр не пересекается вовсе.
//
// Доска — доска Jira Software (agile): у неё есть колонки и фильтр,
// а у проекта ни того ни другого. Колонка доски — набор статусов;
// задача ложится в колонку своего статуса.
//
// Устройство API сверено с описанием Atlassian (REST v3, v2, Agile 1.0)
// 23.09.2026; на настоящей Jira не проверено — проверка идёт против
// поддельного сервера с теми же ответами.
package jira

import (
	"bytes"
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
	ErrUnreachable = errors.New("Jira не отвечает — проверьте подключение к интернету и адрес --url")
	ErrDenied      = errors.New("Jira не приняла вход — проверьте почту и токен API (для своей установки — личный токен)")
	ErrForbidden   = errors.New("Jira не пускает к этой доске — у учётной записи нет права её просматривать")
	ErrBusy        = errors.New("Jira просит подождать: слишком много запросов — повторите через несколько минут")
	ErrOverloaded  = errors.New("Jira сейчас не справляется — повторите через несколько минут")
	ErrNotFound    = errors.New("в Jira нет такой доски — выберите доску из списка")
	ErrNotBoard    = errors.New("адрес отвечает, но это не Jira — проверьте --url: нужен адрес вида https://компания.atlassian.net")
)

const (
	maxResponse = 32 << 20
	// patience — сколько раз повторять на «подождите»: паузы растут
	// вдвое, 1+2+…+64 — около двух минут на один запрос.
	patience = 7
)

// Client ходит в Jira.
type Client struct {
	Base string
	// Email и Token — облако (Basic); Token без Email — своя установка
	// (Bearer, личный токен).
	Email string
	Token string
	HTTP  *http.Client
	// wait — как ждать; подменяется в проверках, чтобы не спать.
	wait func(time.Duration)
}

// New — клиент с разумным сроком на запрос.
func New(base, email, token string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"), Email: email, Token: token,
		HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

// Cloud — облачная Jira: вход почтой и токеном.
func (c *Client) Cloud() bool { return c.Email != "" }

func (c *Client) api() string {
	if c.Cloud() {
		return "/rest/api/3/"
	}
	return "/rest/api/2/"
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.try(ctx, http.MethodGet, path, query, nil, out, 0)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.try(ctx, http.MethodPost, path, nil, body, out, 0)
}

func (c *Client) try(ctx context.Context, method, path string, query url.Values, body, out any, attempt int) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	target := c.Base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Cloud() {
		req.SetBasicAuth(c.Email, c.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnreachable
	}
	defer resp.Body.Close()
	// Облачная Jira считает запросы очками и отвечает 429 с Retry-After;
	// своя установка под нагрузкой — 503. И то и другое пережидается:
	// за ним минуты уже сделанной выгрузки.
	if busy(resp.StatusCode) && attempt < patience {
		pause := time.Duration(1<<attempt) * time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 120 {
			pause = time.Duration(s) * time.Second
		}
		wait := c.wait
		if wait == nil {
			wait = time.Sleep
		}
		wait(pause)
		return c.try(ctx, method, path, query, body, out, attempt+1)
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
		return fmt.Errorf("Jira ответила %d на %s", resp.StatusCode, path)
	}
	if out == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return ErrUnreachable
	}
	if len(raw) > maxResponse {
		return fmt.Errorf("Jira ответила на %s больше чем %d МБ — это не ответ Jira", path, maxResponse>>20)
	}
	// Страница входа или прокси вместо JSON — не та система по адресу.
	if err := json.Unmarshal(raw, out); err != nil {
		return ErrNotBoard
	}
	return nil
}

func busy(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// BoardInfo — доска в списке.
type BoardInfo struct {
	ID      string
	Name    string
	Type    string
	Project string
}

// Boards — доски, которые учётной записи видно.
func (c *Client) Boards(ctx context.Context) ([]BoardInfo, error) {
	var out []BoardInfo
	for start := 0; ; {
		var page struct {
			Values []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Type     string `json:"type"`
				Location struct {
					ProjectKey  string `json:"projectKey"`
					ProjectName string `json:"projectName"`
				} `json:"location"`
			} `json:"values"`
			IsLast bool `json:"isLast"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"50"}}
		if err := c.get(ctx, "/rest/agile/1.0/board", q, &page); err != nil {
			return nil, err
		}
		for _, v := range page.Values {
			out = append(out, BoardInfo{
				ID: strconv.FormatInt(v.ID, 10), Name: v.Name, Type: v.Type,
				Project: strings.TrimSpace(v.Location.ProjectKey + " " + v.Location.ProjectName),
			})
		}
		start += len(page.Values)
		if page.IsLast || len(page.Values) == 0 || len(out) > 10000 {
			return out, nil
		}
	}
}
