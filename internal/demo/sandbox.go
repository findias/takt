package demo

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
)

// SweepSandboxes убирает песочницы, у которых вышел срок, и отвечает,
// сколько убрано.
//
// Одним удалением строки организации: всё остальное уходит каскадом,
// включая людей, заведённых вместе с песочницей (sandbox_org_id).
// Настоящую организацию удалить отсюда нельзя — у неё срока нет,
// и условие на него стоит в самом запросе, а не в вызывающем коде.
//
// Каскада мало: в песочнице появляются люди, которых наполнение
// не заводило, — служебная личность ключа интеграции и тот, кто
// принял приглашение. Их уносит второе правило: человек, у которого
// после удаления песочницы не осталось ни одной организации, уходит
// тоже. Состоящего ещё где-то оно не трогает.
func SweepSandboxes(ctx context.Context, db *store.Store) (int, error) {
	return removeSandboxes(ctx, db, `sandbox_expires_at <= now()`)
}

// RemoveSandbox убирает одну песочницу тем же порядком, что и уборка:
// недозаведённую при неудаче и заведённую проверкой.
func RemoveSandbox(ctx context.Context, db *store.Store, orgID string) error {
	_, err := removeSandboxes(ctx, db, `id = $1`, orgID)
	return err
}

// removeSandboxes — общая часть: какие песочницы, решает условие,
// а «только песочницы» стоит здесь и обойдено быть не может.
func removeSandboxes(ctx context.Context, db *store.Store, which string, args ...any) (int, error) {
	var removed int
	// #sql-склейка: условие — одна из двух констант выше, ввода в нём нет.
	where := `sandbox_expires_at is not null and ` + which
	err := db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
		var members []string
		if err := tx.QueryRow(ctx, `
			select coalesce(array_agg(distinct m.user_id), '{}')
			  from memberships m join orgs o on o.id = m.org_id
			 where o.`+where, args...).Scan(&members); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `delete from orgs where `+where, args...)
		if err != nil {
			return err
		}
		removed = int(tag.RowsAffected())
		_, err = tx.Exec(ctx, `
			delete from users u
			 where u.id = any($1)
			   and not exists (select 1 from memberships m where m.user_id = u.id)`, members)
		return err
	})
	return removed, err
}

// LiveSandboxes — сколько песочниц сейчас живо. По нему держится
// общий предел: бесплатная база маленькая, и сотня заходов подряд
// не должна её заполнить.
func LiveSandboxes(ctx context.Context, db *store.Store) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `
		select count(*) from orgs
		 where sandbox_expires_at is not null and sandbox_expires_at > now()`).Scan(&n)
	return n, err
}
