// Package monday — выгрузка досок monday.com в пакет переноса для
// takt-fetch (ROADMAP 23.4, 23.8).
//
// Ради чего источник вообще нужен по API, а не файлом: выгрузка доски
// в xlsx у monday режется на десяти тысячах элементов и молчит об этом.
// API такого предела не имеет — элементы листаются курсором.
//
// API один — GraphQL на api.monday.com/v2, личный токен в заголовке
// Authorization без слова Bearer. Предел считается «сложностью»
// запроса в минуту; превышение приходит отказом с полем
// retry_in_seconds, и его пережидают.
//
// Устройство API сверено с developer.monday.com 23.09.2026; на настоящем
// monday не проверено — нет учётной записи; проверка идёт против
// поддельного сервера с теми же ответами.
package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Отказы, которые объясняют, что делать.
var (
	ErrUnreachable = errors.New("monday не отвечает — проверьте подключение к интернету")
	ErrDenied      = errors.New("monday не принял токен — проверьте MONDAY_TOKEN: его выдают в профиле, Developers → API token")
	ErrBusy        = errors.New("monday просит подождать: исчерпан предел запросов — повторите через несколько минут")
	ErrOverloaded  = errors.New("monday сейчас не справляется — повторите через несколько минут")
	ErrNotFound    = errors.New("в monday нет такой доски — выберите доску из списка")
	ErrNotMonday   = errors.New("адрес отвечает, но это не monday — проверьте --url: по умолчанию https://api.monday.com/v2")
)

const (
	// DefaultURL — адрес API. Флаг --url нужен проверкам и прокси,
	// своей установки у monday нет.
	DefaultURL  = "https://api.monday.com/v2"
	maxResponse = 64 << 20
	// patience — сколько раз пережидать отказ по пределу.
	patience = 7
)

// Client ходит в monday.
type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
	// wait — как ждать; подменяется в проверках, чтобы не спать.
	wait func(time.Duration)
}

// New — клиент с разумным сроком на запрос.
func New(url, token string) *Client {
	if url == "" {
		url = DefaultURL
	}
	return &Client{URL: url, Token: token, HTTP: &http.Client{Timeout: 120 * time.Second}}
}

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code           string  `json:"code"`
		RetryInSeconds float64 `json:"retry_in_seconds"`
	} `json:"extensions"`
}

// query — один запрос GraphQL. Ответ кладётся в out из поля data.
func (c *Client) query(ctx context.Context, q string, vars map[string]any, out any) error {
	return c.try(ctx, q, vars, out, 0)
}

func (c *Client) try(ctx context.Context, q string, vars map[string]any, out any, attempt int) error {
	body, err := json.Marshal(map[string]any{"query": q, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnreachable
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return ErrUnreachable
	}
	if len(raw) > maxResponse {
		return fmt.Errorf("monday ответил больше чем %d МБ — это не ответ monday", maxResponse>>20)
	}
	var envelope struct {
		Data           json.RawMessage `json:"data"`
		Errors         []gqlError      `json:"errors"`
		ErrorCode      string          `json:"error_code"`
		ErrorMessage   string          `json:"error_message"`
		RetryInSeconds float64         `json:"retry_in_seconds"`
	}
	parsed := json.Unmarshal(raw, &envelope) == nil

	// Предел: 429 или отказ в теле с кодом предела. Сколько ждать,
	// monday называет сам — retry_in_seconds или Retry-After.
	if pause, limited := limitOf(resp, envelope.Errors, envelope.ErrorCode, envelope.RetryInSeconds); limited || overloaded(resp.StatusCode) {
		if attempt < patience {
			if pause <= 0 || pause > 2*time.Minute {
				pause = time.Duration(1<<attempt) * time.Second
			}
			wait := c.wait
			if wait == nil {
				wait = time.Sleep
			}
			wait(pause)
			return c.try(ctx, q, vars, out, attempt+1)
		}
		if limited {
			return ErrBusy
		}
		return ErrOverloaded
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrDenied
	case !parsed:
		return ErrNotMonday
	case resp.StatusCode >= 300:
		return fmt.Errorf("monday ответил %d", resp.StatusCode)
	}
	for _, e := range envelope.Errors {
		code := strings.ToLower(e.Extensions.Code)
		if strings.Contains(code, "unauthorized") || strings.Contains(code, "user_unauthorized") {
			return ErrDenied
		}
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("monday отказал: %s", envelope.Errors[0].Message)
	}
	if envelope.ErrorMessage != "" {
		return fmt.Errorf("monday отказал: %s", envelope.ErrorMessage)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return ErrNotMonday
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return ErrNotMonday
	}
	return nil
}

// limitOf — отказ ли это по пределу и сколько ждать.
func limitOf(resp *http.Response, errs []gqlError, code string, retry float64) (time.Duration, bool) {
	limited := resp.StatusCode == http.StatusTooManyRequests || isLimit(code)
	for _, e := range errs {
		if isLimit(e.Extensions.Code) || isLimit(e.Message) {
			limited = true
			if e.Extensions.RetryInSeconds > 0 {
				retry = e.Extensions.RetryInSeconds
			}
		}
	}
	if !limited {
		return 0, false
	}
	if retry > 0 {
		return time.Duration(retry*float64(time.Second)) + time.Second, true
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		return time.Duration(s) * time.Second, true
	}
	return 0, true
}

func isLimit(s string) bool {
	s = strings.ToLower(s)
	for _, w := range []string{"complexityexception", "complexity budget", "rate_limit", "rate limit", "concurrency", "daily_limit"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func overloaded(code int) bool {
	return code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// BoardInfo — доска в списке.
type BoardInfo struct {
	ID        string
	Name      string
	Workspace string
}

// Boards — доски, которые видно токену. Документы и доски подэлементов
// monday тоже называет досками — они пропускаются.
func (c *Client) Boards(ctx context.Context) ([]BoardInfo, error) {
	const q = `query ($page: Int!) { boards(limit: 100, page: $page, state: active) { id name type workspace { name } } }`
	var out []BoardInfo
	for page := 1; ; page++ {
		var got struct {
			Boards []struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				Type      string `json:"type"`
				Workspace *struct {
					Name string `json:"name"`
				} `json:"workspace"`
			} `json:"boards"`
		}
		if err := c.query(ctx, q, map[string]any{"page": page}, &got); err != nil {
			return nil, err
		}
		for _, b := range got.Boards {
			if b.Type != "" && b.Type != "board" {
				continue
			}
			ws := "Main workspace"
			if b.Workspace != nil && strings.TrimSpace(b.Workspace.Name) != "" {
				ws = strings.TrimSpace(b.Workspace.Name)
			}
			out = append(out, BoardInfo{ID: b.ID, Name: strings.TrimSpace(b.Name), Workspace: ws})
		}
		if len(got.Boards) < 100 || page > 200 {
			return out, nil
		}
	}
}
