package board

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Снимок несёт последнее изменение карточки за неделю — когда и кто
// (ROADMAP 34.14): по нему клиент подсвечивает чужие правки.
func TestSnapshotCarriesRecentChanges(t *testing.T) {
	f := newFixture(t)
	fresh := f.createCard("Свежая", f.columnA)
	old := f.createCard("Давняя", f.columnA)
	f.inTenant(func(tx pgx.Tx) error {
		// У журнала нет права на изменение — он только дописывается,
		// поэтому прошлое сочиняется удалением и новой строкой.
		if _, err := tx.Exec(f.ctx, `delete from card_events where card_id = $1`, old); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `
			insert into card_events (org_id, board_id, card_id, actor_id, type, at)
			values ($1, $2, $3, $4, 'created', now() - interval '10 days')`,
			f.orgID, f.boardID, old, f.actorID)
		return err
	})

	got := f.snapshot().RecentChanges
	c, ok := got[fresh]
	if !ok {
		t.Fatalf("свежей карточки нет в недавних изменениях: %v", got)
	}
	if c.ActorID == nil || *c.ActorID != f.actorID {
		t.Errorf("автор изменения: %v, ждали %s", c.ActorID, f.actorID)
	}
	if time.Since(c.At) > time.Minute {
		t.Errorf("момент изменения %v — не сейчас", c.At)
	}
	if _, ok := got[old]; ok {
		t.Error("изменение десятидневной давности попало в недавние")
	}
}
