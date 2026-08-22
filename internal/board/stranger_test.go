package board

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Человека из чужой организации нельзя вписать ни в доску, ни в карточку.
//
// `users` под RLS не попадает намеренно, и границу здесь держит код:
// три места принимают идентификатор человека снаружи — состав закрытой
// доски, исполнитель карточки и упоминание в обсуждении. Проверка
// на всех трёх сразу, потому что забывается такое поодиночке: у соседей,
// в структуре организации, ровно так и вышло — из трёх мест проверяло
// одно.
func TestStrangerFromAnotherOrgIsRefused(t *testing.T) {
	f := newFixture(t)
	_, stranger := newTenant(t, f.svc.db)
	card := f.createCard("Карточка", f.columnA)

	if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, stranger); !errors.Is(err, ErrNotOrgMember) {
		t.Errorf("посторонний в составе доски: %v, ожидалось %v", err, ErrNotOrgMember)
	}
	if _, err := f.apply("ASSIGN_CARD", map[string]any{"cardId": card, "userId": stranger}); err == nil {
		t.Error("посторонний назначен исполнителем")
	}

	// Упоминание постороннего не отказывает, а отбрасывается: обсуждение
	// пишется ради текста, и ронять целый комментарий из-за одного
	// негодного упоминания значило бы терять написанное.
	c, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card,
		"привет", nil, []string{stranger})
	if err != nil {
		t.Fatalf("комментарий с упоминанием постороннего: %v", err)
	}
	if len(c.Mentions) != 0 {
		t.Errorf("упомянут посторонний: %v", c.Mentions)
	}

	// И ни одной строки на постороннего ни в одной из трёх таблиц.
	f.inTenant(func(tx pgx.Tx) error {
		for _, table := range []string{"board_members", "card_assignees", "card_comment_mentions"} {
			var n int
			if err := tx.QueryRow(f.ctx,
				`select count(*) from `+table+` where user_id = $1`, stranger).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("в %s %d строк на постороннего", table, n)
			}
		}
		return nil
	})
}
