// Package oidc — вход через корпоративный провайдер.
//
// Реализован код-поток (authorization code) с PKCE и без разбора id_token.
// Последнее — сознательное решение, и его стоит объяснить, потому что
// выглядит оно как упущение.
//
// Проверка подписи id_token нужна там, где токен приходит клиенту через
// браузер: по дороге его мог подменить кто угодно. У кода-потока токен
// приходит нам напрямую с конечной точки провайдера по TLS, в ответ
// на наш же запрос, — спецификация прямо разрешает не проверять подпись
// в этом случае (OIDC Core, 3.1.3.7). Взамен мы берём данные о человеке
// с userinfo тем же самым доступом. Написанная руками проверка JWT —
// разбор base64, выбор ключа из JWKS, сверка alg — это ровно тот код,
// в котором ошибаются, и ошибка в нём стоит всей аутентификации.
// Не иметь его безопаснее, чем иметь свой.
//
// PKCE включён, хотя для конфиденциального клиента он и не обязателен:
// он закрывает перехват кода на редиректе, стоит двадцати строк
// и не требует ничего от провайдера, который его не поддерживает.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Provider struct {
	cfg  Config
	http *http.Client

	// Описание провайдера меняется редко, но запрашивать его на каждый
	// вход значит поставить свой вход в зависимость от чужой доступности
	// дважды за попытку. Раз в час достаточно, чтобы заметить переезд
	// конечных точек, и достаточно редко, чтобы не мешать.
	mu      sync.Mutex
	meta    *metadata
	fetched time.Time
	metaTTL time.Duration
	nowFunc func() time.Time
}

type metadata struct {
	Issuer      string `json:"issuer"`
	AuthURL     string `json:"authorization_endpoint"`
	TokenURL    string `json:"token_endpoint"`
	UserInfoURL string `json:"userinfo_endpoint"`
}

func New(cfg Config) *Provider {
	p := &Provider{
		cfg: cfg,
		// Таймаут обязателен: провайдер, отвечающий вечно, иначе держал бы
		// наши соединения до исчерпания, и «сломался вход» превратилось бы
		// в «сломалось всё».
		http:    &http.Client{Timeout: 10 * time.Second},
		metaTTL: time.Hour,
		nowFunc: time.Now,
	}
	return p
}

// Claims — то немногое, что нам нужно знать о человеке.
type Claims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	// Неподтверждённая почта опасна: провайдер, позволяющий вписать чужой
	// адрес, позволил бы через нас войти в чужую учётную запись, если
	// связывать по почте. Поэтому связываем только подтверждённую.
	EmailVerified bool `json:"email_verified"`
}

func (p *Provider) Issuer() string { return p.cfg.Issuer }

// AuthURL строит адрес, на который отправляется браузер.
func (p *Provider) AuthURL(ctx context.Context, state, verifier string) (string, error) {
	meta, err := p.metadata(ctx)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.cfg.ClientID},
		"redirect_uri":          {p.cfg.RedirectURL},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(meta.AuthURL, "?") {
		sep = "&"
	}
	return meta.AuthURL + sep + q.Encode(), nil
}

// Exchange меняет код на доступ и сразу спрашивает, кто пришёл.
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (Claims, error) {
	meta, err := p.metadata(ctx)
	if err != nil {
		return Claims{}, err
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"code_verifier": {verifier},
		"client_id":     {p.cfg.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", meta.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Claims{}, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	// Секрет уходит заголовком, а не в теле: так его не видно в журналах
	// прокси, которые пишут тело чаще, чем принято думать.
	req.SetBasicAuth(url.QueryEscape(p.cfg.ClientID), url.QueryEscape(p.cfg.ClientSecret))

	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := p.call(req, &token); err != nil {
		return Claims{}, fmt.Errorf("обмен кода: %w", err)
	}
	if token.AccessToken == "" {
		return Claims{}, fmt.Errorf("обмен кода: провайдер не выдал доступ")
	}

	info, err := http.NewRequestWithContext(ctx, "GET", meta.UserInfoURL, nil)
	if err != nil {
		return Claims{}, err
	}
	info.Header.Set("authorization", "Bearer "+token.AccessToken)

	var claims Claims
	if err := p.call(info, &claims); err != nil {
		return Claims{}, fmt.Errorf("сведения о человеке: %w", err)
	}
	if claims.Subject == "" {
		return Claims{}, fmt.Errorf("сведения о человеке: провайдер не назвал sub")
	}
	return claims, nil
}

func (p *Provider) call(req *http.Request, out any) error {
	req.Header.Set("accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Ответ читается с ограничением: провайдер, отдающий бесконечный поток,
	// не должен уносить память сервера.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		// Тело чужой ошибки в лог кладём урезанным: в нём бывает и токен.
		return fmt.Errorf("ответ %d: %.200s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func (p *Provider) metadata(ctx context.Context) (*metadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.meta != nil && p.nowFunc().Sub(p.fetched) < p.metaTTL {
		return p.meta, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET",
		strings.TrimRight(p.cfg.Issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	meta := new(metadata)
	if err := p.call(req, meta); err != nil {
		return nil, fmt.Errorf("описание провайдера: %w", err)
	}
	// Издатель в описании обязан совпадать с тем, к кому мы обратились:
	// иначе описание, подсунутое посредником, увело бы вход на чужие
	// конечные точки. Это единственная проверка, которая тут возможна,
	// и стоит она одного сравнения.
	if strings.TrimRight(meta.Issuer, "/") != strings.TrimRight(p.cfg.Issuer, "/") {
		return nil, fmt.Errorf("описание провайдера: издатель %q не совпадает с настроенным %q",
			meta.Issuer, p.cfg.Issuer)
	}
	if meta.AuthURL == "" || meta.TokenURL == "" || meta.UserInfoURL == "" {
		return nil, fmt.Errorf("описание провайдера: нет обязательных конечных точек")
	}

	p.meta = meta
	p.fetched = p.nowFunc()
	return meta, nil
}

// Random — случайная строка для state и для проверочного кода PKCE.
func Random() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
