// Package yougile — перенос из YouGile по его API v2 (ROADMAP 23.3).
//
// Здесь только поставщик промежуточной модели: сходить в YouGile,
// собрать доску, колонки, задачи, людей и стикеры и сложить их
// в importer.Plan. Заводит карточки то же, что и перенос из таблицы, —
// с тем же предпросмотром, пропуском повторов и отчётом.
//
// Устройство API сверено 22.09.2026 по двум независимым клиентам
// (github.com/Hazardooo/yougilego и github.com/nebelov/yougile-mcp):
// официальная страница документации в тот день не открывалась.
// Всё, в чём они расходились, здесь не используется.
//
// Ни пароль, ни ключ не хранятся: пароль нужен один раз, чтобы получить
// ключ, а ключ живёт в браузере, пока открыт экран переноса, и едет
// с каждым запросом, как и файл таблицы.
package yougile

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

// Отказы, которые человеку объясняют, что делать. Сеть здесь чужая,
// и самое частое — её нет вовсе: в закрытом контуре YouGile не виден.
var (
	ErrUnreachable = errors.New("YouGile не отвечает — если сервер в закрытом контуре, выгрузите таблицу в YouGile («Отчёты → Таблицы») и перенесите её файлом")
	ErrDenied      = errors.New("YouGile не принял вход или ключ — проверьте почту, пароль и компанию")
	ErrBusy        = errors.New("YouGile просит подождать: слишком много запросов — повторите через минуту")
	// ErrOverloaded — YouGile ответил 502/503/504 и не перестал за время
	// терпения: он занят, а не сломан, и повторить стоит позже.
	ErrOverloaded = errors.New("YouGile сейчас не справляется — повторите через несколько минут")
	ErrNotFound   = errors.New("в YouGile нет такой доски — выберите доску из списка")
)

// Предел на одну страницу у YouGile — тысяча; ответ одной страницы
// больше нескольких мегабайт не бывает.
const (
	pageSize    = 1000
	maxResponse = 16 << 20
	// Больше задач, чем переносим за раз, с запасом на людей и стикеры.
	maxItems = 50000
)

// Client ходит в YouGile. Base — адрес установки: облачная по умолчанию,
// у коробочной свой (настройка YOUGILE_URL).
type Client struct {
	Base string
	Key  string
	HTTP *http.Client
	// Patient — на «слишком много запросов» ждать и повторять, а не
	// отказывать. Выгрузчику нужно: он обходит чаты сотен задач и
	// упирается в предел YouGile. Серверу — нет: человек на экране
	// должен услышать «подождите», а не ждать молча минуту.
	Patient bool
	// wait — как ждать; подменяется в проверках, чтобы не спать.
	wait func(time.Duration)
}

// New — клиент с разумным сроком на запрос: чужой медленный сервер
// не должен держать перенос вечно.
func New(base, key string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.try(ctx, method, path, query, body, out, 0)
}

// patience — сколько раз повторять терпеливому клиенту: паузы растут
// вдвое, 1+2+…+64 — около двух минут на один запрос.
const patience = 7

func (c *Client) try(ctx context.Context, method, path string, query url.Values, body, out any, attempt int) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	target := c.Base + "/api-v2/" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ErrUnreachable
	}
	defer resp.Body.Close()
	// Переждать стоит не только «слишком много запросов»: YouGile
	// отвечает и 502/503/504, когда ему тяжело, — на доске в восемьсот
	// задач это случается посреди работы, и считать такой ответ
	// поломкой значит бросить полчаса выгрузки (найдено владельцем
	// 22.09.2026 на ARCHTEAM: «YouGile ответил 503 на chats/…»).
	if переждать(resp.StatusCode) && c.Patient && attempt < patience {
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
	case переждать(resp.StatusCode) && resp.StatusCode != http.StatusTooManyRequests:
		// Терпение кончилось (или клиент нетерпелив): это не поломка
		// формата, а занятость — и отказ говорит именно это.
		return ErrOverloaded
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrDenied
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrBusy
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 300:
		return fmt.Errorf("YouGile ответил %d на %s", resp.StatusCode, path)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(out); err != nil {
		return fmt.Errorf("ответ YouGile на %s не разобран: %w", path, err)
	}
	return nil
}

type page[T any] struct {
	Paging struct {
		Next bool `json:"next"`
	} `json:"paging"`
	Content []T `json:"content"`
}

// all собирает все страницы списка.
func all[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	var out []T
	if query == nil {
		query = url.Values{}
	}
	// Сдвиг — на столько, сколько пришло, а не сколько просили: сервер
	// вправе отдать страницу меньше запрошенной, и шаг в тысячу молча
	// перепрыгнул бы через остаток.
	for offset := 0; ; {
		query.Set("limit", fmt.Sprint(pageSize))
		query.Set("offset", fmt.Sprint(offset))
		var p page[T]
		if err := c.do(ctx, http.MethodGet, path, query, nil, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Content...)
		if !p.Paging.Next || len(p.Content) == 0 {
			return out, nil
		}
		// Сервер, который вечно отвечает «есть ещё», не должен
		// заставить нас копить записи без конца.
		if len(out) > maxItems {
			return nil, ErrTooBig
		}
		offset += len(p.Content)
	}
}

// Company — компания, в которой состоит человек.
type Company struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Companies — компании по логину и паролю. Пароль уходит в YouGile
// и больше никуда.
func Companies(ctx context.Context, base, login, password string) ([]Company, error) {
	var p page[Company]
	err := New(base, "").do(ctx, http.MethodPost, "auth/companies", nil,
		map[string]string{"login": login, "password": password}, &p)
	return p.Content, err
}

// Key — ключ API компании: существующий, если он есть, иначе новый.
// Новый ключ — это запись в чужой системе, и экран переноса говорит
// об этом прямо: удалить его можно в YouGile.
func Key(ctx context.Context, base, login, password, companyID string) (key string, created bool, err error) {
	c := New(base, "")
	auth := map[string]string{"login": login, "password": password, "companyId": companyID}
	var keys []struct {
		Key     string `json:"key"`
		Deleted bool   `json:"deleted"`
	}
	if err := c.do(ctx, http.MethodPost, "auth/keys/get", nil, auth, &keys); err != nil {
		return "", false, err
	}
	for _, k := range keys {
		if !k.Deleted && k.Key != "" {
			return k.Key, false, nil
		}
	}
	var made struct {
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodPost, "auth/keys", nil, auth, &made); err != nil {
		return "", false, err
	}
	return made.Key, true, nil
}

// Board — доска YouGile вместе с проектом: у разных проектов бывают
// одноимённые доски.
type Board struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project"`
}

// Boards — все живые доски компании, по проектам.
func (c *Client) Boards(ctx context.Context) ([]Board, error) {
	type item struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		ProjectID string `json:"projectId"`
		Deleted   bool   `json:"deleted"`
	}
	projects, err := all[item](ctx, c, "projects", nil)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, p := range projects {
		if !p.Deleted {
			names[p.ID] = p.Title
		}
	}
	boards, err := all[item](ctx, c, "boards", nil)
	if err != nil {
		return nil, err
	}
	var out []Board
	for _, b := range boards {
		project, ok := names[b.ProjectID]
		if b.Deleted || !ok {
			continue
		}
		out = append(out, Board{ID: b.ID, Title: b.Title, Project: project})
	}
	return out, nil
}

// переждать — ответ, после которого имеет смысл повторить: YouGile
// просит подождать или ему временно нехорошо.
func переждать(код int) bool {
	switch код {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}
