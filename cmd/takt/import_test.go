package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/importer/pack"
	"github.com/findias/takt/internal/store/testdb"
)

// `takt import` — пакет больше 50 МБ переносит администратор на сервере
// (docs/import-package.md). Проверяется обещание команды: по умолчанию
// предпросмотр и ничего не записано; с --apply перенесено; наблюдатель
// не переносит; человек в двух организациях обязан назвать одну.
func TestImportCommandPreviewsThenApplies(t *testing.T) {
	if testing.Short() {
		t.Skip("нужна база")
	}
	db := testdb.Shared(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	email := "cli-" + suffix + "@example.test"

	var orgID, userID string
	if err := db.Pool.QueryRow(ctx, `insert into orgs (name, slug) values ($1, $2) returning id`,
		"Командная строка "+suffix, "cli-"+suffix).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, orgID) })
	if err := db.Pool.QueryRow(ctx, `insert into users (email, name, password_hash) values ($1, 'Админ', 'x') returning id`,
		email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `delete from users where id = $1`, userID) })
	if _, err := db.Pool.Exec(ctx, `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`, orgID, userID); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := pack.Write(&buf, pack.Manifest{CreatedBy: "проверка", Source: pack.Source{System: "trello"}}, []pack.Board{{
		Title:   "Из Trello",
		Columns: []pack.Column{{ExternalID: "l-1", Title: "To Do"}},
		Cards: []pack.Card{
			{ExternalID: "c-1", Title: "Первая", Column: "l-1"},
			{ExternalID: "c-2", Title: "Часть первой", Column: "l-1", Parent: ptr("c-1")},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "trello.takt")
	if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runImport(ctx, db, []string{file, "--as", strings.ToUpper(email)}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Переедут: карточек 2 из 2, подзадач 1") ||
		!strings.Contains(out.String(), "Это предпросмотр") {
		t.Fatalf("предпросмотр:\n%s", out.String())
	}
	// Считать — от имени владельца: без контекста организации политики
	// ответят «0» при любом числе досок, и проверка молчала бы всегда.
	count := func() int {
		var n int
		if err := db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select count(*) from boards`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 0 {
		t.Fatal("предпросмотр завёл доску")
	}

	out.Reset()
	if err := runImport(ctx, db, []string{file, "--as", email, "--apply"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Перенесено: карточек 2") || count() != 1 {
		t.Fatalf("перенос:\n%s", out.String())
	}

	// Наблюдатель не переносит.
	if _, err := db.Pool.Exec(ctx, `update memberships set role = 'viewer' where user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if err := runImport(ctx, db, []string{file, "--as", email}, &out); err == nil || !strings.Contains(err.Error(), "наблюдатель") {
		t.Fatalf("наблюдатель: %v", err)
	}
	if err := runImport(ctx, db, []string{"--as", email}, &out); err == nil || !strings.Contains(err.Error(), "не назван файл") {
		t.Fatalf("без файла: %v", err)
	}
}

func ptr(s string) *string { return &s }
