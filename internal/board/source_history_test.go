package board

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// Перенесённое до 26.09.2026 могло сохранить системные сообщения YouGile
// как «.»: так их отдал первый настоящий перенос, и карточка показала
// «До переноса» столбиком точек. Записи в базе остаются, но на карточку
// выходят только те, в которых есть буква или цифра.
func TestSourceHistoryWithoutWordsStaysOffTheCard(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Перенесённая", f.columnA)
	err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `
			update cards set external_source = 'yougile', external_id = 'DEV-8', source_history_at = now()
			 where id = $1`, card); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `
			insert into card_source_history (org_id, board_id, card_id, at, author, text)
			select $1, $2, $3, now() - (n || ' minutes')::interval, '', t
			  from unnest(array['.', ' · ', 'Срок изменён на 1 октября']) with ordinality as x(t, n)`,
			f.orgID, f.boardID, card)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	detail, err := f.svc.Card(f.ctx, f.orgID, f.actorID, f.boardID, card)
	if err != nil {
		t.Fatal(err)
	}
	h := detail.SourceHistory
	if h == nil || len(h.Entries) != 1 || h.Entries[0].Text != "Срок изменён на 1 октября" {
		t.Fatalf("на карточке история: %+v", h)
	}
}
