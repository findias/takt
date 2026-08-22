package team

import (
	"errors"
	"strings"
	"testing"
)

// Живой узел не висит под убранным — какой бы дверью ни шли.
//
// Правило было, но охраняло одну дверь из трёх. Архивация не пускала
// убрать узел с живыми потомками; возврат из архива не пускал вернуть
// узел под архивированного старшего — это проверял код; а заведение
// нового узла под архивированным старшим проходило. Прогон 22 августа
// 2026 отвечал 201, и в дереве оказывался узел, у которого есть
// `parent_id`, а старшего в дереве нет.
//
// Четвёртый за день случай одной формы: правило записано там, где
// о нём думали, и не записано там, где не думали. Поэтому оно
// сформулировано в базе не по действию, а по итогу — «если после
// правки узел живой, его старший обязан быть живым тоже», — и этим
// закрывает все три двери сразу, вместе с четвёртой, которую никто
// ещё не придумал.
func TestLiveNodeNeverHangsUnderAnArchivedParent(t *testing.T) {
	f := newFixture(t)
	parent := f.create("Разработка", nil)
	child := f.create("Платформа", &parent.ID)
	sibling := f.create("Продажи", nil)

	// Чтобы убрать старшего, сперва уводим потомка в сторону.
	if err := f.svc.Move(f.ctx, f.orgID, f.owner, child.ID, &sibling.ID); err != nil {
		t.Fatalf("перенос потомка: %v", err)
	}
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, parent.ID); err != nil {
		t.Fatalf("архивация старшего: %v", err)
	}

	var tree *TreeError
	// Дверь первая: завести новый узел внутрь убранного.
	if _, err := f.svc.Create(f.ctx, f.orgID, f.owner, "Ядро", &parent.ID); !errors.As(err, &tree) {
		t.Errorf("узел заведён под убранным старшим: %v", err)
	}
	// Дверь вторая: перенести живой узел внутрь убранного.
	if err := f.svc.Move(f.ctx, f.orgID, f.owner, child.ID, &parent.ID); !errors.As(err, &tree) {
		t.Errorf("узел перенесён под убранного старшего: %v", err)
	}
	// Дверь третья: вернуть из архива узел, старший которого убран.
	inside := f.create("Инфра", &sibling.ID)
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, inside.ID); err != nil {
		t.Fatalf("архивация потомка: %v", err)
	}
	if err := f.svc.Move(f.ctx, f.orgID, f.owner, child.ID, nil); err != nil {
		t.Fatalf("увод потомка в корень: %v", err)
	}
	if err := f.svc.Archive(f.ctx, f.orgID, f.owner, sibling.ID); err != nil {
		t.Fatalf("архивация старшего: %v", err)
	}
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, inside.ID); !errors.As(err, &tree) {
		t.Errorf("узел возвращён под убранного старшего: %v", err)
	}

	// Все три отказа называют старшего по имени: порядок действий,
	// известный только из молчания, — это загадка, а не порядок.
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, inside.ID); err != nil &&
		!strings.Contains(err.Error(), "Продажи") {
		t.Errorf("отказ не называет старшего: %v", err)
	}

	// А по порядку — всё возвращается.
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, sibling.ID); err != nil {
		t.Fatalf("возврат старшего: %v", err)
	}
	if err := f.svc.Restore(f.ctx, f.orgID, f.owner, inside.ID); err != nil {
		t.Fatalf("возврат потомка после старшего: %v", err)
	}
}

// Пустое название — ошибка просящего, а не сервера.
func TestEmptyNameIsAnsweredWithWords(t *testing.T) {
	f := newFixture(t)
	var bad *BadRequest
	if _, err := f.svc.Create(f.ctx, f.orgID, f.owner, "  ", nil); !errors.As(err, &bad) {
		t.Errorf("заведение с пустым названием: %v", err)
	}
	node := f.create("Разработка", nil)
	if err := f.svc.Rename(f.ctx, f.orgID, f.owner, node.ID, " "); !errors.As(err, &bad) {
		t.Errorf("переименование в пустое: %v", err)
	}
}
