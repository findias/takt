package board

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Тесты семантики колонок и отметок потока. Как и остальные тесты операций,
// работают против настоящей базы: вся логика здесь живёт в SQL.

func (f *fixture) columns() []Column {
	f.t.Helper()
	return f.snapshot().Columns
}

func (f *fixture) card(id string) Card {
	f.t.Helper()
	for _, c := range f.snapshot().Cards {
		if c.ID == id {
			return c
		}
	}
	f.t.Fatalf("карточка %s не найдена на доске", id)
	return Card{}
}

func TestDefaultColumnsCarryFlowSemantics(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	if len(cols) != 3 {
		t.Fatalf("ожидались три колонки по умолчанию, получено %d", len(cols))
	}

	want := []struct {
		kind     string
		started  bool
		finished bool
	}{
		{KindQueue, false, false},
		{KindInProgress, true, false},
		{KindDone, false, true},
	}
	for i, w := range want {
		got := cols[i]
		if got.Kind != w.kind || got.IsStartedPoint != w.started || got.IsFinishedPoint != w.finished {
			t.Errorf("колонка %q: вид %q, старт %v, финиш %v; ожидалось %q/%v/%v",
				got.Name, got.Kind, got.IsStartedPoint, got.IsFinishedPoint,
				w.kind, w.started, w.finished)
		}
	}
}

func TestFlowStampsFollowStartAndFinishPoints(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	queue, work, done := cols[0].ID, cols[1].ID, cols[2].ID

	id := f.createCard("Задача", queue)
	if c := f.card(id); c.StartedAt != nil || c.FinishedAt != nil {
		t.Fatalf("карточка в очереди уже помечена начатой или завершённой: %+v", c)
	}

	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": work, "place": "end"})
	started := f.card(id).StartedAt
	if started == nil {
		t.Fatal("пересечение точки старта не проставило начало работы")
	}
	if f.card(id).FinishedAt != nil {
		t.Error("работа помечена завершённой раньше времени")
	}

	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": done, "place": "end"})
	if f.card(id).FinishedAt == nil {
		t.Fatal("пересечение точки финиша не проставило завершение")
	}

	// Возврат в работу снимает завершение, но не трогает начало: иначе
	// время цикла карточки, которую доделывали, окажется меньше реального.
	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": work, "place": "end"})
	back := f.card(id)
	if back.FinishedAt != nil {
		t.Error("карточка вернулась в работу, но осталась завершённой")
	}
	if back.StartedAt == nil || !back.StartedAt.Equal(*started) {
		t.Errorf("начало работы переписано: было %v, стало %v", started, back.StartedAt)
	}
}

func TestColumnEntryResetsOnlyWhenColumnChanges(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	queue, work := cols[0].ID, cols[1].ID

	id := f.createCard("Первая", queue)
	f.createCard("Вторая", queue)
	entered := f.card(id).ColumnEnteredAt

	// Перестановка внутри колонки — не вход в неё: возраст карточки
	// не должен обнуляться от того, что её подняли выше в очереди.
	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": queue, "place": "end"})
	if got := f.card(id).ColumnEnteredAt; !got.Equal(entered) {
		t.Errorf("перестановка внутри колонки обнулила возраст: было %v, стало %v", entered, got)
	}

	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": work, "place": "end"})
	if got := f.card(id).ColumnEnteredAt; !got.After(entered) {
		t.Errorf("смена колонки не обновила момент входа: было %v, стало %v", entered, got)
	}
}

func TestSoftLimitAllowsAndHardLimitBlocks(t *testing.T) {
	f := newFixture(t)
	work := f.columns()[1].ID

	// Мягкий лимит — только подсветка на клиенте, операции он не трогает.
	f.mustApply("UPDATE_COLUMN", map[string]any{"columnId": work, "wipLimit": 1})
	f.createCard("Первая", work)
	f.createCard("Вторая", work)
	if got := len(f.titles(work)); got != 2 {
		t.Fatalf("мягкий лимит помешал работе: в колонке %d карточек", got)
	}

	f.mustApply("UPDATE_COLUMN", map[string]any{"columnId": work, "wipLimitHard": true})
	_, err := f.apply("CREATE_CARD", map[string]any{"columnId": work, "title": "Третья"})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("жёсткий лимит не остановил создание карточки: %v", err)
	}
	if conflict.ColumnID != work {
		t.Errorf("конфликт не указал переполненную колонку: %+v", conflict)
	}

	// Перестановка внутри переполненной колонки должна оставаться возможной:
	// она не добавляет работы, а колонка могла набрать карточек раньше,
	// пока лимит был мягким.
	var cardID string
	for _, c := range f.snapshot().Cards {
		if c.ColumnID == work {
			cardID = c.ID
			break
		}
	}
	if _, err := f.apply("MOVE_CARD", map[string]any{
		"cardId": cardID, "toColumnId": work, "place": "end"}); err != nil {
		t.Errorf("перестановка внутри колонки с жёстким лимитом отклонена: %v", err)
	}

	// Снять лимит можно только явным null — отсутствие поля ничего не меняет.
	f.mustApply("UPDATE_COLUMN", map[string]any{"columnId": work, "wipLimit": nil})
	if _, err := f.apply("CREATE_CARD", map[string]any{"columnId": work, "title": "Четвёртая"}); err != nil {
		t.Errorf("лимит не снялся: %v", err)
	}
}

func TestMoveEventKeepsColumnSnapshot(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	queue, work := cols[0].ID, cols[1].ID

	id := f.createCard("Задача", queue)
	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": work, "place": "end"})

	// Переименование колонки не должно переписывать историю задним числом.
	f.mustApply("RENAME_COLUMN", map[string]any{"columnId": work, "name": "Разработка"})

	var raw []byte
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select payload from card_events
			 where card_id = $1 and type = 'moved'
			 order by id desc limit 1`, id).Scan(&raw)
	})

	var payload struct {
		From struct{ Name, Kind string } `json:"from"`
		To   struct{ Name, Kind string } `json:"to"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.To.Name != "В работе" {
		t.Errorf("в журнале сохранилось имя колонки %q, ожидалось «В работе»", payload.To.Name)
	}
	if payload.To.Kind != KindInProgress || payload.From.Kind != KindQueue {
		t.Errorf("виды колонок в журнале: из %q в %q", payload.From.Kind, payload.To.Kind)
	}
}

func TestColumnUpdateIsValidated(t *testing.T) {
	f := newFixture(t)
	work := f.columns()[1].ID

	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"неизвестный вид", map[string]any{"columnId": work, "kind": "ожидание"}},
		{"нулевой лимит", map[string]any{"columnId": work, "wipLimit": 0}},
		{"пустое имя", map[string]any{"columnId": work, "name": "  "}},
	}
	for _, c := range cases {
		if _, err := f.apply("UPDATE_COLUMN", c.payload); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: ожидалась ErrBadRequest, получено %v", c.name, err)
		}
	}

	if _, err := f.apply("UPDATE_COLUMN", map[string]any{
		"columnId": uuid.NewString(), "name": "Нет такой"}); err == nil {
		t.Error("обновление несуществующей колонки прошло без ошибки")
	}

	// Нечитаемый идентификатор — ошибка клиента, а не сбой сервера:
	// иначе опечатка в запросе выглядит как падение.
	if _, err := f.apply("UPDATE_COLUMN", map[string]any{
		"columnId": "не-uuid", "name": "Х"}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("нечитаемый идентификатор: ожидалась ErrBadRequest, получено %v", err)
	}

	res := f.mustApply("UPDATE_COLUMN", map[string]any{
		"columnId":       work,
		"kind":           KindDone,
		"isStartedPoint": false,
		"policy":         "Готово — значит развёрнуто на стенде",
	})
	got := res.Patch.Columns[0]
	if got.Kind != KindDone || got.IsStartedPoint || got.Policy == "" {
		t.Errorf("колонка обновилась не полностью: %+v", got)
	}
}
