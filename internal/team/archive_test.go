package team

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Убранное подразделение возвращается, а пока оно в архиве — не обещает
// того, чего не даёт.
//
// До 22 августа 2026 «Убрать подразделение» уносило узел из дерева без
// вопроса и без дороги назад: возврата не было ни в API, ни на экране.
// Это нарушало собственное правило проекта сразу с двух сторон —
// «необратимое спрашивает, обратимое — нет»: действие не спрашивало,
// как обратимое, и не возвращалось, как необратимое.
//
// Вторая половина проверки — про то, что архив делает с надзором.
// Записи наблюдателя и администратора переживают архивацию (иначе
// возврат вернул бы пустой узел), но не действуют: и то и другое
// считается по живым узлам. Значит, показывать их нельзя — это слово,
// за которым ничего нет, и хуже обычного: наблюдение обещает надзор,
// а надзора нет, и не знают об этом обе стороны сразу.
func TestArchivedTeamComesBackAndPromisesNothingMeanwhile(t *testing.T) {
	f := newFixture(t)
	company := f.create("Компания", nil)
	dev := f.create("Разработка", &company.ID)

	head := f.user("member")
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, head, dev.ID); err != nil {
		t.Fatalf("назначение администратора: %v", err)
	}
	watcher := f.user("member")
	if _, err := f.svc.Grant(f.ctx, f.orgID, f.owner, watcher, &dev.ID); err != nil {
		t.Fatalf("выдача наблюдения: %v", err)
	}

	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, dev.ID); err != nil {
		t.Fatalf("архивация: %v", err)
	}

	// Списки молчат о том, чего больше нет.
	if obs, _ := f.svc.Observers(f.ctx, f.orgID, f.owner); len(obs) != 0 {
		t.Errorf("наблюдение за убранным узлом показано: %+v", obs)
	}
	if adm, _ := f.svc.Admins(f.ctx, f.orgID, f.owner); len(adm) != 0 {
		t.Errorf("администратор убранного узла показан: %+v", adm)
	}
	// А записи целы: иначе возврат вернул бы пустой узел.
	if n := f.count(`select count(*) from observers where team_id = $1`, dev.ID); n != 1 {
		t.Errorf("запись наблюдения потеряна при архивации: %d", n)
	}
	if n := f.count(`select count(*) from team_admins where team_id = $1`, dev.ID); n != 1 {
		t.Errorf("запись администратора потеряна при архивации: %d", n)
	}

	// Архив показывает, что и откуда убрано.
	arch, err := f.svc.Archived(f.ctx, f.orgID, f.owner)
	if err != nil || len(arch) != 1 {
		t.Fatalf("архив: %d записей, err=%v", len(arch), err)
	}
	if arch[0].Name != "Разработка" || arch[0].ParentName != "Компания" {
		t.Errorf("в архиве %q внутри %q", arch[0].Name, arch[0].ParentName)
	}
	if arch[0].ParentArchived {
		t.Error("живой старший назван архивированным")
	}

	// Возврат — и надзор возвращается вместе с узлом.
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, dev.ID); err != nil {
		t.Fatalf("возврат: %v", err)
	}
	if obs, _ := f.svc.Observers(f.ctx, f.orgID, f.owner); len(obs) != 1 {
		t.Errorf("после возврата наблюдение не вернулось: %+v", obs)
	}
	if adm, _ := f.svc.Admins(f.ctx, f.orgID, f.owner); len(adm) != 1 {
		t.Errorf("после возврата администратор не вернулся: %+v", adm)
	}
}

// Возврат под архивированного старшего — отказ, и отказ называет его
// по имени: порядок действий, известный только из молчания, — загадка,
// а не порядок.
func TestRestoreRefusesUnderArchivedParent(t *testing.T) {
	f := newFixture(t)
	dev := f.create("Разработка", nil)
	platform := f.create("Платформа", &dev.ID)

	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, platform.ID); err != nil {
		t.Fatalf("архивация потомка: %v", err)
	}
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, dev.ID); err != nil {
		t.Fatalf("архивация старшего: %v", err)
	}

	var tree *TreeError
	err := f.svc.Restore(f.ctx, f.orgID, f.owner, platform.ID)
	if !errors.As(err, &tree) {
		t.Fatalf("возврат под архивированного старшего: %v", err)
	}
	if !strings.Contains(tree.Reason, "Разработка") {
		t.Errorf("отказ не называет старшего: %q", tree.Reason)
	}

	// Архив об этом говорит заранее, а не после нажатия.
	arch, _ := f.svc.Archived(f.ctx, f.orgID, f.owner)
	for _, a := range arch {
		if a.Name == "Платформа" && !a.ParentArchived {
			t.Error("архив не сказал, что старший тоже убран")
		}
		if a.Name == "Разработка" && a.ParentArchived {
			t.Error("у корневого узла нашёлся архивированный старший")
		}
	}

	// Сперва старший, потом этот — и оба возвращаются.
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, dev.ID); err != nil {
		t.Fatalf("возврат старшего: %v", err)
	}
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, platform.ID); err != nil {
		t.Fatalf("возврат потомка после старшего: %v", err)
	}
	if list, _ := f.svc.List(f.ctx, f.orgID, f.owner); len(list) != 2 {
		t.Errorf("в дереве %d узлов, ожидалось 2", len(list))
	}
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	err := f.db.InTenant(f.ctx, f.orgID, f.owner, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, sql, args...).Scan(&n)
	})
	if err != nil {
		f.t.Fatalf("счёт: %v", err)
	}
	return n
}
