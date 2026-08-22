package team

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// Администратор поддерева не выводит свою область из-под надзора.
//
// Политика спрашивает одно: остался ли узел внутри моей области. Для
// узла, который сам и есть корень области, ответ «да» при любом
// родителе — собственный идентификатор из пути никуда не девается.
// Поэтому до 22 августа 2026 администратор «Разработки» мог перенести
// её в корень дерева: наблюдатель, которому владелец выдал надзор
// за «Компанией», переставал видеть и «Разработку», и всё под ней,
// а администратором тот оставался. Подотчётный в одно действие
// переставал быть подотчётным.
//
// Проверка смотрит не на отказ, а на то, ради чего он нужен: что видит
// наблюдатель. Отказ можно вернуть и сломав что-нибудь другое; надзор
// либо цел, либо нет.
func TestSubtreeAdminCannotMoveItsAreaOutFromUnderOversight(t *testing.T) {
	f := newFixture(t)
	company := f.create("Компания", nil)
	dev := f.create("Разработка", &company.ID)
	platform := f.create("Платформа", &dev.ID)
	sales := f.create("Продажи", &company.ID)

	head := f.user("member")
	if _, err := f.svc.GrantAdmin(f.ctx, f.orgID, f.owner, head, dev.ID); err != nil {
		t.Fatalf("назначение администратора: %v", err)
	}
	watcher := f.user("member")
	if _, err := f.svc.Grant(f.ctx, f.orgID, f.owner, watcher, &company.ID); err != nil {
		t.Fatalf("владелец не выдал наблюдение: %v", err)
	}

	seen := f.observed(watcher)
	if !seen[dev.ID] || !seen[platform.ID] {
		t.Fatalf("наблюдатель не видит поддерево с самого начала: %v", seen)
	}

	// Наружу — нельзя ни в корень, ни к соседу. Отказ обязан объяснять:
	// человек здесь администратор, и «недостаточно прав» отправило бы его
	// просить о том, что рядом он делает сам.
	var tree *TreeError
	if err := f.svc.Move(f.ctx, f.orgID, head, dev.ID, nil); !asTree(err, &tree) {
		t.Errorf("свою область увели в корень: %v", err)
	} else if tree.Reason == "" {
		t.Error("отказ на перенос в корень пуст")
	}
	if err := f.svc.Move(f.ctx, f.orgID, head, dev.ID, &sales.ID); !asTree(err, &tree) {
		t.Errorf("свою область увели под соседа: %v", err)
	}

	// Внутри своей области — сколько угодно: запрещён не перенос,
	// а перемена того, кому область подотчётна.
	core, err := f.svc.Create(f.ctx, f.orgID, head, "Ядро", &dev.ID)
	if err != nil {
		t.Fatalf("администратор не завёл отдел в своей области: %v", err)
	}
	if err := f.svc.Move(f.ctx, f.orgID, head, platform.ID, &core.ID); err != nil {
		t.Errorf("администратор не переставил узел внутри своей области: %v", err)
	}
	// И переименовать свой корень — родитель у него снаружи по определению,
	// и правило про родителя не должно этого задевать.
	if err := f.svc.Rename(f.ctx, f.orgID, head, dev.ID, "Разработка и платформа"); err != nil {
		t.Errorf("администратор не переименовал свой корень: %v", err)
	}

	if seen := f.observed(watcher); !seen[dev.ID] || !seen[platform.ID] {
		t.Errorf("после перестановок надзор потерян: %v", seen)
	}

	// Владельцу можно: он и есть тот, кто решает, кому что подотчётно.
	// Надзор при этом теряется — и это его решение, а не чужое.
	if err := f.svc.Move(f.ctx, f.orgID, f.owner, dev.ID, nil); err != nil {
		t.Fatalf("владелец не смог вывести подразделение в корень: %v", err)
	}
	if seen := f.observed(watcher); seen[dev.ID] {
		t.Error("после переноса владельцем надзор за поддеревом остался")
	}
}

func asTree(err error, out **TreeError) bool {
	t, ok := err.(*TreeError)
	if ok {
		*out = t
	}
	return ok
}

// observed — то, что видит наблюдатель: спрашивается у той же функции,
// по которой политики решают видимость. Спрашивать список подразделений
// бесполезно — дерево видно всем в организации; наблюдение решает,
// чьи доски и карточки открыты.
func (f *fixture) observed(userID string) map[string]bool {
	f.t.Helper()
	out := map[string]bool{}
	err := f.db.InTenant(f.ctx, f.orgID, userID, func(tx pgx.Tx) error {
		var ids []string
		if err := tx.QueryRow(f.ctx,
			`select coalesce(array_agg(x::text), '{}') from unnest(app_observed_teams()) x`).
			Scan(&ids); err != nil {
			return err
		}
		for _, id := range ids {
			out[id] = true
		}
		return nil
	})
	if err != nil {
		f.t.Fatalf("чтение наблюдаемого: %v", err)
	}
	return out
}
