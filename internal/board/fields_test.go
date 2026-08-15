package board

import (
	"errors"
	"testing"
)

// Свои поля карточки. Проверяется главное: значение обязано
// соответствовать виду поля, и держит это база, а не договорённость.

func TestFieldsOfEveryKindAreStoredAndRead(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)

	kinds := []struct {
		name    string
		kind    string
		options []string
		value   any
		want    any
	}{
		{"Заказчик", FieldText, nil, "Отдел кадров", "Отдел кадров"},
		{"Стоимость", FieldNumber, nil, 12.5, 12.5},
		{"Срок", FieldDate, nil, "2026-09-01", "2026-09-01"},
		{"Срочно", FieldCheckbox, nil, true, true},
		{"Важность", FieldSelect, []string{"низкая", "высокая"}, "высокая", "высокая"},
	}

	byID := map[string]string{}
	for _, k := range kinds {
		field, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, k.name, k.kind, k.options)
		if err != nil {
			t.Fatalf("создание поля %q: %v", k.name, err)
		}
		byID[field.ID] = k.name
		f.mustApply("SET_CARD_FIELD", map[string]any{
			"cardId": card, "fieldId": field.ID, "value": k.value})
	}

	values := f.snapshot().FieldValues[card]
	if len(values) != len(kinds) {
		t.Fatalf("значений %d, ожидалось %d: %+v", len(values), len(kinds), values)
	}
	got := map[string]any{}
	for _, v := range values {
		got[byID[v.FieldID]] = v.Value
	}
	for _, k := range kinds {
		if got[k.name] != k.want {
			t.Errorf("поле %q: значение %#v, ожидалось %#v", k.name, got[k.name], k.want)
		}
	}
}

// Проверить соответствие ограничением таблицы нельзя — вид лежит в другой
// таблице. Значит, правило держит триггер, и обойти его через операцию
// не должно получаться.
func TestValueMustMatchFieldKind(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)

	number, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Стоимость", FieldNumber, nil)
	if err != nil {
		t.Fatal(err)
	}
	choice, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Важность", FieldSelect,
		[]string{"низкая", "высокая"})
	if err != nil {
		t.Fatal(err)
	}
	date, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Срок", FieldDate, nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		field string
		value any
	}{
		{"строка вместо числа", number.ID, "много"},
		{"вариант не из списка", choice.ID, "средняя"},
		{"дата не датой", date.ID, "первое сентября"},
		{"пустая строка", choice.ID, "   "},
	}
	for _, c := range cases {
		if _, err := f.apply("SET_CARD_FIELD", map[string]any{
			"cardId": card, "fieldId": c.field, "value": c.value}); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: принято, ошибка %v", c.name, err)
		}
	}

	if len(f.snapshot().FieldValues[card]) != 0 {
		t.Error("отвергнутые значения всё-таки сохранились")
	}
}

// Смена значения не должна оставлять хвост от прежнего: в таблице ровно
// одна заполненная колонка, и ограничение это стережёт.
func TestChangingValueLeavesNoTrace(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	field, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Заказчик", FieldText, nil)
	if err != nil {
		t.Fatal(err)
	}

	f.mustApply("SET_CARD_FIELD", map[string]any{
		"cardId": card, "fieldId": field.ID, "value": "Первый"})
	f.mustApply("SET_CARD_FIELD", map[string]any{
		"cardId": card, "fieldId": field.ID, "value": "Второй"})

	values := f.snapshot().FieldValues[card]
	if len(values) != 1 || values[0].Value != "Второй" {
		t.Fatalf("значения после смены: %+v", values)
	}

	// Пустое значение снимает поле: «поля нет» и «поле пустое» — одно
	// и то же, третьего состояния заводить незачем.
	f.mustApply("SET_CARD_FIELD", map[string]any{
		"cardId": card, "fieldId": field.ID, "value": nil})
	if got := f.snapshot().FieldValues[card]; len(got) != 0 {
		t.Errorf("поле не снялось: %+v", got)
	}
}

func TestFieldDefinitionsAreValidated(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "  ", FieldText, nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("поле без названия: %v", err)
	}
	if _, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Что-то", "цвет", nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("неизвестный вид: %v", err)
	}
	if _, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Выбор", FieldSelect, nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("выбор без вариантов: %v", err)
	}

	// Повторы и пустые варианты выбрасываются: два неразличимых варианта
	// превращают отчёт по ним в перечисление опечаток.
	field, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Важность", FieldSelect,
		[]string{"низкая", " низкая ", "", "высокая"})
	if err != nil {
		t.Fatal(err)
	}
	if len(field.Options) != 2 {
		t.Errorf("варианты после чистки: %v", field.Options)
	}

	// Два поля с одним названием — гарантированная путаница в отчётах.
	if _, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "ВАЖНОСТЬ", FieldText, nil); !errors.Is(err, ErrFieldExists) {
		t.Errorf("поле-двойник: %v", err)
	}
}

// Убранное поле не удаляет значения: поле заводили как раз затем, чтобы
// эти данные были, и стирать их вместе с определением нельзя.
func TestArchivedFieldKeepsItsValues(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	field, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Заказчик", FieldText, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.mustApply("SET_CARD_FIELD", map[string]any{
		"cardId": card, "fieldId": field.ID, "value": "Отдел кадров"})

	if err := f.svc.ArchiveField(f.ctx, f.orgID, f.actorID, field.ID); err != nil {
		t.Fatal(err)
	}

	snap := f.snapshot()
	if len(snap.Fields) != 0 {
		t.Errorf("убранное поле осталось в словаре: %+v", snap.Fields)
	}
	if len(snap.FieldValues[card]) != 1 {
		t.Errorf("значения исчезли вместе с определением: %+v", snap.FieldValues[card])
	}

	// В убранное поле больше не пишут.
	if _, err := f.apply("SET_CARD_FIELD", map[string]any{
		"cardId": card, "fieldId": field.ID, "value": "Другой"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("запись в убранное поле: %v", err)
	}
}

func TestFieldsOfAnotherOrgAreInvisible(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Заказчик", FieldText, nil); err != nil {
		t.Fatal(err)
	}
	other := newFixture(t)

	fields, err := f.svc.Fields(f.ctx, other.orgID, other.actorID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 0 {
		t.Errorf("из чужой организации видно %d полей", len(fields))
	}
}
