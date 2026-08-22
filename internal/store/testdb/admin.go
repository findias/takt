package testdb

import (
	"net/url"
	"os"
	"sync"
	"testing"
)

// saidAdmin — то же правило, что и у said: объяснение печатается один раз.
var saidAdmin sync.Once

// AdminURL возвращает адрес базы для того, кто умеет заводить базы.
//
// Нужен ровно одной проверке — той, что применяет цепочку миграций
// с нуля. Роль приложения завести базу не может намеренно (`nocreatedb`
// в `deploy/postgres-init`), и это правильно: с правами, которых
// приложению не нужно, оно однажды ими и воспользуется. Поэтому базу
// под проверку заводит другая роль, а миграции в ней идут уже под
// ролью приложения — иначе проверялась бы не та цепочка, что применяется
// на самом деле.
func AdminURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("TEST_ADMIN_DATABASE_URL"); u != "" {
		return u
	}
	if testing.Short() {
		t.Skip("короткий прогон: проверки против базы пропущены намеренно")
	}
	message := "не задан TEST_ADMIN_DATABASE_URL"
	saidAdmin.Do(func() {
		message = "не задан TEST_ADMIN_DATABASE_URL, а без него цепочку миграций\n" +
			"негде применить с нуля: роль приложения заводить базы не умеет.\n" +
			"Прогнать всё: make check (или make test-integration) — они задают его сами.\n" +
			"Сознательно обойтись без базы: go test -short ./..."
	})
	t.Fatal(message)
	return ""
}

// WithDatabase подставляет другое имя базы в тот же адрес: та же роль,
// тот же сервер, другая база.
func WithDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// UserOf достаёт имя роли из адреса: под ней и должна оказаться заведённая
// база, иначе миграции в ней не применятся.
func UserOf(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	return u.User.Username(), nil
}
