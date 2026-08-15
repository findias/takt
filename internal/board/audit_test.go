package board

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Журнал административных действий. Проверяется он тем же способом, что
// и всё остальное в этой схеме: не «код вызывает запись», а «база записала
// сама» — в том числе когда пишут в обход приложения.

type auditRow struct {
	Action  string
	Subject string
	Actor   *string
	Payload string
}

// audit читает записи журнала об одном предмете, от свежих к старым.
func (f *fixture) audit(subject, subjectID string) []auditRow {
	f.t.Helper()
	var out []auditRow
	f.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `
			select action, subject, actor_id::text, payload::text
			  from audit_events
			 where subject = $1 and subject_id = $2
			 order by id desc`, subject, subjectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRow
			if err := rows.Scan(&r.Action, &r.Subject, &r.Actor, &r.Payload); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out
}

func TestAdministrativeActionsAreRecordedWithTheirAuthor(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)

	created := f.audit("teams", dev)
	if len(created) != 1 || created[0].Action != "insert" {
		t.Fatalf("создание команды дало записи %+v, ожидалась одна о вставке", created)
	}
	if created[0].Actor == nil || *created[0].Actor != f.actorID {
		t.Errorf("создание команды записано без автора или с чужим: %v", created[0].Actor)
	}

	// Перенос подразделения — то самое действие, о котором спрашивают
	// постфактум: журнал обязан помнить и прежнего родителя, и нового.
	other := f.team("Продажи", nil)
	if err := f.move(dev, &other); err != nil {
		t.Fatal(err)
	}
	moved := f.audit("teams", dev)
	if len(moved) != 2 || moved[0].Action != "update" {
		t.Fatalf("перенос команды дал записи %+v, ожидались вставка и изменение", moved)
	}
	if !strings.Contains(moved[0].Payload, company) || !strings.Contains(moved[0].Payload, other) {
		t.Error("в записи о переносе нет прежнего или нового родителя")
	}
}

func TestObservationGrantIsRecorded(t *testing.T) {
	f := newFixture(t)
	watcher := addMember(t, f.svc.db, f.orgID, "member")
	team := f.team("Найм", nil)
	f.observes(watcher, &team)

	rows := f.audit("observers", watcher)
	if len(rows) != 1 || rows[0].Action != "insert" {
		t.Fatalf("выдача наблюдения дала записи %+v, ожидалась одна", rows)
	}
	if rows[0].Actor == nil || *rows[0].Actor != f.actorID {
		t.Error("выдача наблюдения записана без автора")
	}
	if !strings.Contains(rows[0].Payload, team) {
		t.Error("в записи не видно, за каким подразделением выдано наблюдение")
	}
}

// Журнал, из которого можно вычеркнуть строку, не журнал. Политик update
// и delete на нём нет вовсе, а значит они запрещены по умолчанию.
func TestAuditIsAppendOnly(t *testing.T) {
	f := newFixture(t)
	f.team("Компания", nil)

	err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(f.ctx, `update audit_events set action = 'insert'`)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			t.Errorf("изменено записей журнала: %d", tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("попытка изменения: %v", err)
	}

	err = f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(f.ctx, `delete from audit_events`)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			t.Errorf("удалено записей журнала: %d", tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("попытка удаления: %v", err)
	}

	if len(f.audit("teams", f.boardID)) != 0 {
		t.Error("тест смотрит не туда")
	}
}

// Назваться чужим именем нельзя ни при каком значении подписи.
func TestAuditSignatureCannotBeForged(t *testing.T) {
	f := newFixture(t)
	other := addMember(t, f.svc.db, f.orgID, "member")

	err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into audit_events (org_id, actor_id, action, subject, subject_id)
			values ($1, $2, 'delete', 'teams', gen_random_uuid())`, f.orgID, other)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("запись от чужого имени прошла: %v", err)
	}
}

// Журнал читают владелец и наблюдатель всей организации. Рядовой участник
// не читает: по ленте видно, кто кого куда переводил.
func TestAuditIsReadableOnlyByOwnerAndOrgObserver(t *testing.T) {
	f := newFixture(t)
	f.team("Компания", nil)
	member := addMember(t, f.svc.db, f.orgID, "member")
	watcher := addMember(t, f.svc.db, f.orgID, "member")
	f.observes(watcher, nil)

	count := func(userID string) int {
		f.t.Helper()
		var n int
		err := f.svc.db.InTenant(f.ctx, f.orgID, userID, func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `select count(*) from audit_events`).Scan(&n)
		})
		if err != nil {
			t.Fatalf("чтение журнала: %v", err)
		}
		return n
	}

	if count(f.actorID) == 0 {
		t.Error("владелец не видит журнала")
	}
	if count(watcher) == 0 {
		t.Error("наблюдатель всей организации не видит журнала")
	}
	if got := count(member); got != 0 {
		t.Errorf("рядовой участник видит %d записей журнала", got)
	}
}

// Хеш токена приглашения — не «почти безопасное» значение: политика
// открывает строку приглашения именно по хешу, поэтому знание хеша
// равносильно знанию ссылки. В журнал он попадать не должен.
func TestInviteSecretNeverReachesTheAudit(t *testing.T) {
	f := newFixture(t)

	// Хеш уникален в таблице, поэтому он свой на каждый прогон: иначе
	// тест переживает только первый запуск.
	secret := "секретный-хеш-" + uuid.NewString()
	email := uuid.NewString() + "@example.test"

	var inviteID string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
			values ($1, $2, 'member', $3, $4, now() + interval '1 day')
			returning id`, f.orgID, email, secret, f.actorID).Scan(&inviteID)
	})

	rows := f.audit("invites", inviteID)
	if len(rows) != 1 {
		t.Fatalf("приглашение дало записи %+v, ожидалась одна", rows)
	}
	if strings.Contains(rows[0].Payload, secret) {
		t.Error("хеш токена приглашения попал в журнал")
	}
	if strings.Contains(rows[0].Payload, "token_hash") {
		t.Error("в журнале осталось само поле с секретом")
	}
	if !strings.Contains(rows[0].Payload, email) {
		t.Error("журнал не сохранил, кого приглашали")
	}
}
