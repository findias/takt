package board

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Исполнитель карточки.
//
// Проверяется не «поле сохраняется», а границы: назначить можно только
// участника организации, снять — всегда, а уход человека из организации
// не должен стирать подпись под работой, которую он делал.

func TestCardGetsAndLosesAssignee(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Кому-то делать", f.columnA)

	// По умолчанию никого: работа сначала появляется, потом обретает
	// исполнителя.
	if got := f.card(cardID).AssigneeID; got != nil {
		t.Fatalf("у новой карточки уже есть исполнитель: %v", *got)
	}

	res := f.mustApply("ASSIGN_CARD", map[string]any{
		"cardId": cardID, "assigneeId": f.actorID,
	})
	if len(res.Patch.Cards) != 1 || res.Patch.Cards[0].AssigneeID == nil {
		t.Fatalf("патч без исполнителя: %+v", res.Patch)
	}
	if got := f.card(cardID).AssigneeID; got == nil || *got != f.actorID {
		t.Errorf("исполнитель не сохранился: %v", got)
	}

	// Снятие — то же назначение, только никому.
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "assigneeId": nil})
	if got := f.card(cardID).AssigneeID; got != nil {
		t.Errorf("исполнитель не снялся: %v", *got)
	}
}

// Назначить постороннего нельзя. Проверка живёт в операции, а не во
// внешнем ключе: ключу пришлось бы каскадно снимать исполнителя при
// исключении человека, то есть переписывать историю.
func TestAssigneeMustBeInTheOrganisation(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	cardID := f.createCard("Своя работа", f.columnA)

	_, err := f.apply("ASSIGN_CARD", map[string]any{
		"cardId": cardID, "assigneeId": other.actorID,
	})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("назначен посторонний, ошибка: %v", err)
	}
	if got := f.card(cardID).AssigneeID; got != nil {
		t.Errorf("исполнителем стал посторонний: %v", *got)
	}

	// И несуществующего человека — тоже нельзя.
	_, err = f.apply("ASSIGN_CARD", map[string]any{
		"cardId": cardID, "assigneeId": uuid.NewString(),
	})
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("назначен несуществующий человек, ошибка: %v", err)
	}
}

// Человек ушёл из организации — карточки остаются подписанными им.
// Иначе история работы теряет ответ на вопрос «кто это делал».
func TestLeavingTheOrganisationKeepsTheSignature(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Сделал и ушёл", f.columnA)
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "assigneeId": f.actorID})

	// Участие снимается напрямую: так делает исключение из организации.
	// Возвращаем его тут же — иначе читать доску станет некому: без
	// участия политики не покажут ничего, и это правильно.
	if _, err := f.svc.db.Pool.Exec(f.ctx,
		`delete from memberships where org_id = $1 and user_id = $2`,
		f.orgID, f.actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		f.orgID, f.actorID); err != nil {
		t.Fatal(err)
	}

	// Главное: удаление участия не потянуло за собой исполнителя.
	// Внешний ключ на memberships сделал бы именно это — и история
	// работы лишилась бы ответа на вопрос «кто её делал».
	if got := f.card(cardID).AssigneeID; got == nil || *got != f.actorID {
		t.Errorf("подпись под работой исчезла вместе с участием: %v", got)
	}
}

// Список тех, кого можно назначить, приезжает со снимком: иначе
// исполнитель на карточке остался бы идентификатором.
func TestSnapshotCarriesPeople(t *testing.T) {
	f := newFixture(t)
	snap := f.snapshot()

	if len(snap.People) == 0 {
		t.Fatal("снимок без людей организации")
	}
	found := false
	for _, p := range snap.People {
		if p.UserID == f.actorID {
			found = true
			if p.Name == "" {
				t.Error("человек без имени: показывать нечего")
			}
		}
	}
	if !found {
		t.Error("владельца доски нет среди тех, кого можно назначить")
	}
}
