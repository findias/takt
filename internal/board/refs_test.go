package board

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Ссылки на заявки внешних систем: много на карточку, у каждой вид,
// дубль и неизвестный вид отвергаются с объяснением.

func TestRefsAreAddedListedAndRemoved(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Выгрузка обращений", f.columnA)

	for _, r := range []struct{ kind, ref string }{
		{RefZNO, "ЗНО-10492"},
		{RefZNO, " ЗНО-10517 "},
		{RefZNI, "https://sm.example.test/changes/771"},
		{RefProblem, "PRB-12"},
		{RefRDS, "RDS-3"},
	} {
		f.mustApply("ADD_CARD_REF", map[string]any{"cardId": card, "kind": r.kind, "ref": r.ref})
	}

	refs := f.snapshot().CardRefs[card]
	if len(refs) != 5 {
		t.Fatalf("ссылок %d, ожидалось 5: %+v", len(refs), refs)
	}
	// Порядок — порядок появления, пробелы по краям срезаны.
	if refs[1].Kind != RefZNO || refs[1].Ref != "ЗНО-10517" {
		t.Errorf("вторая ссылка: %+v", refs[1])
	}

	f.mustApply("REMOVE_CARD_REF", map[string]any{"cardId": card, "refId": refs[0].ID})
	// Повтор — не ошибка: ссылки уже нет, и этого и хотели.
	f.mustApply("REMOVE_CARD_REF", map[string]any{"cardId": card, "refId": refs[0].ID})
	if got := len(f.snapshot().CardRefs[card]); got != 4 {
		t.Errorf("после снятия ссылок %d, ожидалось 4", got)
	}

	// И добавление, и снятие видны в истории — с тем, что именно было.
	var added, removed int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select count(*) filter (where type = 'ref_added'),
			       count(*) filter (where type = 'ref_removed' and payload->>'ref' = 'ЗНО-10492')
			  from card_events where card_id = $1`, card).Scan(&added, &removed)
	})
	if added != 5 || removed != 1 {
		t.Errorf("событий: добавлено %d, снято %d; ожидалось 5 и 1", added, removed)
	}
}

func TestRefRejectsDuplicateAndUnknownKind(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	f.mustApply("ADD_CARD_REF", map[string]any{"cardId": card, "kind": RefZNO, "ref": "ЗНО-1"})

	var conflict *ConflictError
	if _, err := f.apply("ADD_CARD_REF", map[string]any{
		"cardId": card, "kind": RefZNO, "ref": "ЗНО-1"}); !errors.As(err, &conflict) {
		t.Errorf("дубль: ожидался конфликт, получено %v", err)
	}
	// Тот же номер другим видом — другая ссылка: ЗНО и ЗНИ нумеруются
	// каждая своим счётчиком.
	f.mustApply("ADD_CARD_REF", map[string]any{"cardId": card, "kind": RefZNI, "ref": "ЗНО-1"})

	for name, payload := range map[string]map[string]any{
		"неизвестный вид": {"cardId": card, "kind": "incident", "ref": "INC-1"},
		"пустая ссылка":   {"cardId": card, "kind": RefZNO, "ref": "   "},
	} {
		if _, err := f.apply("ADD_CARD_REF", payload); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: ожидался отказ как неверный запрос, получено %v", name, err)
		}
	}
}

// Операция приходит от доски; ссылку чужой доске через неё не повесить,
// даже в своей организации.
func TestRefOnlyOnCardOfThisBoard(t *testing.T) {
	f := newFixture(t)
	other, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Соседи", "")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	res := f.applyTo(other.ID, "CREATE_CARD", map[string]any{
		"columnId": snap.Columns[0].ID, "title": "Чужая"})
	foreign := res.Patch.Cards[0].ID

	var conflict *ConflictError
	if _, err := f.apply("ADD_CARD_REF", map[string]any{
		"cardId": foreign, "kind": RefZNO, "ref": "ЗНО-1"}); !errors.As(err, &conflict) {
		t.Errorf("ссылка на карточку чужой доски: ожидался конфликт, получено %v", err)
	}
}
