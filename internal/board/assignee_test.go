package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Исполнители карточки.
//
// Проверяется не «поле сохраняется», а границы: исполнителей может быть
// несколько и порядок назначения сохраняется, назначить можно только
// участника организации, снять — всегда, а уход человека из организации
// не должен стирать подпись под работой, которую он делал.

// inviteMember заводит второго участника той же организации: без него
// «несколько исполнителей» проверить не на ком.
func (f *fixture) inviteMember(name string) string {
	f.t.Helper()
	var userID string
	err := f.svc.db.Pool.QueryRow(f.ctx, `
		insert into users (email, name, password_hash)
		values ($1, $2, 'x') returning id`,
		uuid.NewString()+"@example.test", name).Scan(&userID)
	if err != nil {
		f.t.Fatalf("создание участника: %v", err)
	}
	f.t.Cleanup(func() {
		_, _ = f.svc.db.Pool.Exec(context.Background(), `delete from users where id = $1`, userID)
	})
	if _, err := f.svc.db.Pool.Exec(f.ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		f.orgID, userID); err != nil {
		f.t.Fatalf("создание членства: %v", err)
	}
	return userID
}

// assignees читает исполнителей карточки из снимка доски: снимок — то,
// что видит клиент, и проверять надо именно его.
func (f *fixture) assignees(cardID string) []string {
	f.t.Helper()
	return f.snapshot().CardAssignees[cardID]
}

func TestCardGetsAndLosesAssignee(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Кому-то делать", f.columnA)

	// По умолчанию никого: работа сначала появляется, потом обретает
	// исполнителя.
	if got := f.assignees(cardID); len(got) != 0 {
		t.Fatalf("у новой карточки уже есть исполнители: %v", got)
	}

	res := f.mustApply("ASSIGN_CARD", map[string]any{
		"cardId": cardID, "userId": f.actorID,
	})
	if got := res.Patch.CardAssignees[cardID]; len(got) != 1 || got[0] != f.actorID {
		t.Fatalf("патч без исполнителя: %+v", res.Patch)
	}
	if got := f.assignees(cardID); len(got) != 1 || got[0] != f.actorID {
		t.Errorf("исполнитель не сохранился: %v", got)
	}

	// Повтор безобиден: список тот же, а не два одинаковых человека.
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": f.actorID})
	if got := f.assignees(cardID); len(got) != 1 {
		t.Errorf("повторное назначение задвоило исполнителя: %v", got)
	}

	f.mustApply("UNASSIGN_CARD", map[string]any{"cardId": cardID, "userId": f.actorID})
	if got := f.assignees(cardID); len(got) != 0 {
		t.Errorf("исполнитель не снялся: %v", got)
	}

	// Снятие того, кого не назначали, — не ошибка: результат тот же,
	// которого просили.
	f.mustApply("UNASSIGN_CARD", map[string]any{"cardId": cardID, "userId": f.actorID})
}

// Несколько исполнителей — то, ради чего заведён список: пара за одной
// задачей, смежник на день, проверяющий. Порядок назначения сохраняется:
// первым в списке остаётся тот, кто взялся первым, и это единственное,
// чем список отвечает на вопрос «кто отвечает».
func TestCardKeepsSeveralAssigneesInOrder(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Делать вдвоём", f.columnA)
	second := f.inviteMember("Второй исполнитель")

	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": f.actorID})
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": second})

	got := f.assignees(cardID)
	if len(got) != 2 || got[0] != f.actorID || got[1] != second {
		t.Fatalf("исполнители %v, ожидались %s и %s именно в этом порядке",
			got, f.actorID, second)
	}

	// Снятие одного оставляет второго — и не меняет порядок остальных.
	f.mustApply("UNASSIGN_CARD", map[string]any{"cardId": cardID, "userId": f.actorID})
	if got := f.assignees(cardID); len(got) != 1 || got[0] != second {
		t.Errorf("после снятия первого осталось %v, ожидался %s", got, second)
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
		"cardId": cardID, "userId": other.actorID,
	})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("назначен посторонний, ошибка: %v", err)
	}
	if got := f.assignees(cardID); len(got) != 0 {
		t.Errorf("исполнителем стал посторонний: %v", got)
	}

	// И несуществующего человека — тоже нельзя.
	_, err = f.apply("ASSIGN_CARD", map[string]any{
		"cardId": cardID, "userId": uuid.NewString(),
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
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": f.actorID})

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
	if got := f.assignees(cardID); len(got) != 1 || got[0] != f.actorID {
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

// Ключ интеграции состоит в организации наравне с человеком — так
// устроены политики, — но работу ему не назначают: «назначить задачу
// ключу» это предложение, за которым ничего нет.
func TestServiceIdentityIsNotAssignable(t *testing.T) {
	f := newFixture(t)
	botID := f.inviteMember("Обмен со складом")
	if _, err := f.svc.db.Pool.Exec(f.ctx,
		`update users set kind = 'service' where id = $1`, botID); err != nil {
		t.Fatal(err)
	}

	for _, p := range f.snapshot().People {
		if p.UserID == botID {
			t.Error("ключ предлагается в исполнители")
		}
	}
}
