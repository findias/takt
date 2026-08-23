// Package config собирает настройки приложения из переменных окружения.
// Один образ на все профили развёртывания: разница между запуском на одной
// машине и в Kubernetes — только в значениях этих переменных.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	// BaseURL — адрес, по которому приложение открывают в браузере.
	// Из него клиент строит адрес WebSocket-соединения, поэтому ошибка
	// здесь даёт не понятную ошибку, а вечный спиннер.
	BaseURL     string
	DatabaseURL string
	ListenAddr  string
	// WebDir — каталог со собранным фронтендом. Пустое значение отключает
	// раздачу статики (удобно при разработке, когда работает Vite).
	WebDir string
	// Storage — место хранения вложений. Пока поддержан только file://,
	// но интерфейс заложен так, чтобы позже подставить s3://.
	Storage string

	// Signup — кому позволено заводить организации самостоятельно.
	//
	// До этой настройки регистрация была открыта всегда и выключить её
	// было нечем. На своём стенде это удобство; на чужом, выставленном
	// в корпоративную сеть, это значит, что любой сотрудник заводит себе
	// организацию мимо каталога, мимо провайдера входа и мимо владельца
	// — и обнаруживается это по счёту организаций, а не по отказу.
	Signup SignupMode

	// Вход через корпоративный провайдер. Настройки берутся из окружения,
	// а не из базы, и это решение стоит объяснить.
	//
	// Хранить настройки провайдера по организациям было бы правильно для
	// облачной установки с сотней арендаторов. Но там же пришлось бы
	// хранить секрет клиента, то есть шифровать его ключом, которого
	// у нас нет, — и появился бы ключ, его ротация, хранилище ключей
	// и вопрос «а кто расшифрует, если сервер потеряли». Корпоративный
	// вход ставят в закрытом контуре, где организация одна, а секрет
	// уже лежит в секретах кластера. Отсюда: одна установка — один
	// провайдер, и никаких секретов в базе.
	OIDC OIDCConfig
}

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// OrgSlug — организация, в которую попадает пришедший впервые.
	// Уже состоящий в какой-либо организации никуда не добавляется:
	// вход не должен молча менять чью-то принадлежность.
	OrgSlug string
	// Label — надпись на кнопке. У заказчика провайдер называется
	// не «OIDC», а «Корпоративный аккаунт» или именем своей системы,
	// и человек ищет глазами знакомое слово.
	Label string
}

func (c OIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != ""
}

// SignupMode — три ответа на вопрос «кто заводит организации».
type SignupMode string

const (
	// SignupFirst — пока в установке нет ни одной организации, её заводит
	// тот, кто пришёл первым; дальше только по приглашению.
	//
	// Это умолчание, и оно же ответ на вопрос «кто заводит первого
	// владельца»: он заводит себя сам, ровно один раз, до того как
	// установка кому-то показана. Отдельная команда установщика или
	// разовая ссылка решали бы тот же вопрос дороже — и обе требуют
	// доступа к серверу в тот момент, когда ставящий обычно уже ушёл.
	SignupFirst SignupMode = "first"
	// SignupOpen — заводить организации может кто угодно. Так работает
	// наш стенд и так работала бы облачная установка с сотней арендаторов.
	SignupOpen SignupMode = "open"
	// SignupClosed — не может никто, включая первого. Для установки,
	// где организация заводится заранее — миграцией данных, каталогом
	// или руками, — а вход идёт только через провайдера.
	SignupClosed SignupMode = "closed"
)

func Load() (Config, error) {
	c := Config{
		BaseURL:     env("BASE_URL", "http://localhost:8080"),
		DatabaseURL: env("DATABASE_URL", ""),
		ListenAddr:  env("LISTEN_ADDR", ":8080"),
		WebDir:      env("WEB_DIR", "./web/dist"),
		Storage:     env("STORAGE", "file://./data/attachments"),
		Signup:      SignupMode(env("SIGNUP", string(SignupFirst))),
		OIDC: OIDCConfig{
			Issuer:       strings.TrimRight(env("OIDC_ISSUER", ""), "/"),
			ClientID:     env("OIDC_CLIENT_ID", ""),
			ClientSecret: env("OIDC_CLIENT_SECRET", ""),
			OrgSlug:      env("OIDC_ORG", ""),
			Label:        env("OIDC_LABEL", "Корпоративный аккаунт"),
		},
	}

	if c.DatabaseURL == "" {
		return c, fmt.Errorf("не задан DATABASE_URL")
	}
	switch c.Signup {
	case SignupFirst, SignupOpen, SignupClosed:
	default:
		// Опечатка в SIGNUP не должна означать «как получится»: значение
		// решает, кто заводит организации, и молчаливое умолчание здесь
		// обнаружилось бы по счёту организаций.
		return c, fmt.Errorf(
			"SIGNUP=%q: бывает first (первый пришедший, дальше по приглашению), "+
				"open (кто угодно) или closed (никто)", c.Signup)
	}

	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return c, fmt.Errorf("BASE_URL = %q: ожидался абсолютный адрес вида https://board.example.ru", c.BaseURL)
	}

	if c.OIDC.Enabled() {
		u, err := url.Parse(c.OIDC.Issuer)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return c, fmt.Errorf("OIDC_ISSUER = %q: ожидался абсолютный адрес", c.OIDC.Issuer)
		}
		// Только https, кроме локального стенда. Провайдер по http означает,
		// что коды и токены ходят открытым текстом, и весь корпоративный
		// вход становится украшением.
		if u.Scheme != "https" && !isLoopback(u.Hostname()) {
			return c, fmt.Errorf("OIDC_ISSUER = %q: провайдер должен быть по https", c.OIDC.Issuer)
		}
		if c.OIDC.ClientSecret == "" {
			return c, fmt.Errorf("не задан OIDC_CLIENT_SECRET")
		}
		if c.OIDC.OrgSlug == "" {
			return c, fmt.Errorf("не задан OIDC_ORG: некуда зачислять пришедшего впервые")
		}
	}

	return c, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// RedirectURL — обратный адрес, который провайдер обязан знать заранее.
// Собирается из BASE_URL, чтобы его нельзя было настроить отдельно
// и разойтись с ним: несовпадение здесь даёт отказ в момент входа,
// а не при запуске.
func (c Config) RedirectURL() string { return c.BaseURL + "/api/auth/oidc/callback" }

// SecureCookies включает флаг Secure у cookie, когда приложение открывается
// по https. За обратным прокси схему знает только BASE_URL.
func (c Config) SecureCookies() bool {
	return strings.HasPrefix(c.BaseURL, "https://")
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
