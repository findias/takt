package board

import (
	"errors"
	"testing"
)

// Причину блокировки правят, не снимая блокировку (замечено владельцем
// 25.09.2026): прежде опечатку или устаревшую причину можно было
// исправить только снятием и новой блокировкой — и время в блоке
// разрывалось надвое.
func TestBlockReasonIsEditedInPlace(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Ждёт доступ", f.columnA)
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": id, "reason": "ждём досуп"})
	before := f.card(id).Blocked

	res := f.mustApply("SET_BLOCK_REASON", map[string]any{"cardId": id, "reason": "  ждём доступ к хранилищу  "})
	if len(res.Patch.Cards) == 0 || res.Patch.Cards[0].Blocked == nil ||
		res.Patch.Cards[0].Blocked.Reason != "ждём доступ к хранилищу" {
		t.Fatalf("причина в патче: %+v", res.Patch.Cards)
	}
	after := f.card(id).Blocked
	if after.ID != before.ID || !after.BlockedAt.Equal(before.BlockedAt) {
		t.Errorf("правка причины открыла новую блокировку: было %+v, стало %+v", before, after)
	}

	var conflict *ConflictError
	if _, err := f.apply("SET_BLOCK_REASON", map[string]any{"cardId": id, "reason": "   "}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("пустая причина: ожидался отказ, получено %v", err)
	}
	f.mustApply("UNBLOCK_CARD", map[string]any{"cardId": id})
	if _, err := f.apply("SET_BLOCK_REASON", map[string]any{"cardId": id, "reason": "что-то"}); !errors.As(err, &conflict) {
		t.Errorf("незаблокированная: ожидался конфликт, получено %v", err)
	}
}
