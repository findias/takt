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
}

func Load() (Config, error) {
	c := Config{
		BaseURL:     env("BASE_URL", "http://localhost:8080"),
		DatabaseURL: env("DATABASE_URL", ""),
		ListenAddr:  env("LISTEN_ADDR", ":8080"),
		WebDir:      env("WEB_DIR", "./web/dist"),
		Storage:     env("STORAGE", "file://./data/attachments"),
	}

	if c.DatabaseURL == "" {
		return c, fmt.Errorf("не задан DATABASE_URL")
	}

	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return c, fmt.Errorf("BASE_URL = %q: ожидался абсолютный адрес вида https://board.example.ru", c.BaseURL)
	}

	return c, nil
}

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
