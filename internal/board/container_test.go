package board

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// Дерево и поток не мешают друг другу (этап 32.6): эпик — карточка
// с внуками — не занимает места в лимите колонки.

func (f *fixture) hardLimit(columnID string, limit int) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `update board_columns set wip_limit = $2, wip_limit_hard = true where id = $1`,
			columnID, limit)
		return err
	})
}

func TestContainerTakesNoPlaceInWIP(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	f.sub(feature, "Задача")
	other := f.createCard("Работа", f.columnA)

	// В колонке B лимит 1; одна работа уже там.
	f.hardLimit(f.columnB, 1)
	f.mustApply("MOVE_CARD", map[string]any{"cardId": other, "toColumnId": f.columnB, "place": "end"})

	// Эпик проходит в полную колонку: его работа посчитана частями.
	if _, err := f.apply("MOVE_CARD", map[string]any{"cardId": epic, "toColumnId": f.columnB, "place": "end"}); err != nil {
		t.Fatalf("эпик не прошёл в полную колонку: %v", err)
	}
	// А обычная работа — по-прежнему нет: эпик места не занял, но и
	// лимит не отменил.
	if _, err := f.apply("MOVE_CARD", map[string]any{"cardId": feature, "toColumnId": f.columnB, "place": "end"}); err == nil {
		t.Error("фича прошла в колонку с исчерпанным жёстким лимитом")
	}
}

// Контейнер определяется деревом в момент счёта: потерял внуков —
// снова работа и снова занимает место.
func TestContainerIsDecidedByTreeNow(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	task := f.sub(feature, "Задача")
	busy := f.createCard("Занятое место", f.columnA)
	f.hardLimit(f.columnB, 1)
	f.mustApply("MOVE_CARD", map[string]any{"cardId": busy, "toColumnId": f.columnB, "place": "end"})
	if _, err := f.apply("MOVE_CARD", map[string]any{"cardId": epic, "toColumnId": f.columnB, "place": "end"}); err != nil {
		t.Fatalf("эпик с внуком не прошёл: %v", err)
	}
	f.mustApply("MOVE_CARD", map[string]any{"cardId": epic, "toColumnId": f.columnA, "place": "end"})
	f.mustApply("UNLINK_CARDS", map[string]any{"fromCard": feature, "toCard": task, "kind": "subtask"})
	if _, err := f.apply("MOVE_CARD", map[string]any{"cardId": epic, "toColumnId": f.columnB, "place": "end"}); err == nil {
		t.Error("карточка без внуков прошла в полную колонку как контейнер")
	}
}
