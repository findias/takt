package board

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

// Тесты работают против настоящей базы: почти вся логика операций живёт
// в SQL и транзакциях, и подменять их заглушками — значит проверять
// заглушки. Запуск:
//
//	make test-integration
//
// Без TEST_DATABASE_URL тесты пропускаются, чтобы `go test ./...` оставался
// зелёным на машине без docker.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("не задан TEST_DATABASE_URL, интеграционные тесты пропущены")
	}
	db, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("подключение к тестовой базе: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

type fixture struct {
	svc      *Service
	orgID    string
	actorID  string
	boardID  string
	columnA  string
	columnB  string
	ctx      context.Context
	t        *testing.T
	sequence int
}

// newTenant заводит изолированную организацию с владельцем.
func newTenant(t *testing.T, db *store.Store) (orgID, userID string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()

	err := db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Тест "+suffix[:8], "test-"+suffix[:8]).Scan(&orgID)
	if err != nil {
		t.Fatalf("создание организации: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, orgID)
	})

	err = db.Pool.QueryRow(ctx, `
		insert into users (email, name, password_hash)
		values ($1, 'Тестовый', 'x') returning id`,
		suffix+"@example.test").Scan(&userID)
	if err != nil {
		t.Fatalf("создание пользователя: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from users where id = $1`, userID)
	})

	_, err = db.Pool.Exec(ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		orgID, userID)
	if err != nil {
		t.Fatalf("создание членства: %v", err)
	}
	return orgID, userID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testStore(t)
	ctx := context.Background()
	svc := New(db)
	orgID, actorID := newTenant(t, db)

	b, err := svc.Create(ctx, orgID, actorID, "Доска")
	if err != nil {
		t.Fatalf("создание доски: %v", err)
	}
	snap, err := svc.Snapshot(ctx, orgID, actorID, b.ID)
	if err != nil {
		t.Fatalf("снимок доски: %v", err)
	}
	if len(snap.Columns) < 2 {
		t.Fatalf("ожидались колонки по умолчанию, получено %d", len(snap.Columns))
	}

	return &fixture{
		svc: svc, orgID: orgID, actorID: actorID, boardID: b.ID,
		columnA: snap.Columns[0].ID, columnB: snap.Columns[1].ID,
		ctx: ctx, t: t,
	}
}

// snapshot читает доску фикстуры от имени её владельца. Видимость доски
// считается по человеку, поэтому безличного снимка у теста быть не может.
func (f *fixture) snapshot() Snapshot {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	return snap
}

// inTenant выполняет запрос к базе от имени владельца фикстуры — для
// проверок того, чего не видно в снимке доски: журнала, интервалов,
// исхода архивной карточки.
func (f *fixture) inTenant(fn func(pgx.Tx) error) {
	f.t.Helper()
	if err := f.svc.db.InTenant(f.ctx, f.orgID, f.actorID, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) apply(kind string, payload any) (Result, error) {
	f.t.Helper()
	return f.applyWithID(uuid.NewString(), kind, payload)
}

func (f *fixture) applyWithID(opID, kind string, payload any) (Result, error) {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.svc.Apply(f.ctx, f.orgID, f.actorID, f.boardID,
		Request{OperationID: opID, Type: kind, Payload: raw})
}

func (f *fixture) mustApply(kind string, payload any) Result {
	f.t.Helper()
	res, err := f.apply(kind, payload)
	if err != nil {
		f.t.Fatalf("операция %s не выполнилась: %v", kind, err)
	}
	return res
}

// titles возвращает названия карточек колонки в порядке позиций.
func (f *fixture) titles(columnID string) []string {
	f.t.Helper()
	out := []string{}
	for _, c := range f.snapshot().Cards {
		if c.ColumnID == columnID {
			out = append(out, c.Title)
		}
	}
	return out
}

func (f *fixture) createCard(title, columnID string) string {
	f.t.Helper()
	res := f.mustApply("CREATE_CARD", map[string]any{"columnId": columnID, "title": title})
	return res.Patch.Cards[0].ID
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCreateAppendsToEndByDefault(t *testing.T) {
	f := newFixture(t)
	want := []string{"Первая", "Вторая", "Третья"}
	for _, title := range want {
		f.createCard(title, f.columnA)
	}
	if got := f.titles(f.columnA); !equal(got, want) {
		t.Errorf("порядок карточек %v, ожидался %v", got, want)
	}
}

func TestPlacement(t *testing.T) {
	f := newFixture(t)
	first := f.createCard("Первая", f.columnA)
	f.createCard("Вторая", f.columnA)
	third := f.createCard("Третья", f.columnA)

	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": third, "toColumnId": f.columnA, "place": "start"})
	if got := f.titles(f.columnA); !equal(got, []string{"Третья", "Первая", "Вторая"}) {
		t.Fatalf("после place=start порядок %v", got)
	}

	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": third, "toColumnId": f.columnA, "place": "after", "afterCardId": first})
	if got := f.titles(f.columnA); !equal(got, []string{"Первая", "Третья", "Вторая"}) {
		t.Fatalf("после place=after порядок %v", got)
	}

	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": third, "toColumnId": f.columnB, "place": "end"})
	if got := f.titles(f.columnA); !equal(got, []string{"Первая", "Вторая"}) {
		t.Errorf("карточка не покинула исходную колонку: %v", got)
	}
	if got := f.titles(f.columnB); !equal(got, []string{"Третья"}) {
		t.Errorf("карточка не появилась в целевой колонке: %v", got)
	}
}

// Повтор операции — обычное дело: клиент не дождался ответа и отправил
// её заново. Второй раз она выполняться не должна.
func TestOperationIsIdempotent(t *testing.T) {
	f := newFixture(t)
	opID := uuid.NewString()
	payload := map[string]any{"columnId": f.columnA, "title": "Единственная"}

	first, err := f.applyWithID(opID, "CREATE_CARD", payload)
	if err != nil {
		t.Fatalf("первая попытка: %v", err)
	}
	second, err := f.applyWithID(opID, "CREATE_CARD", payload)
	if err != nil {
		t.Fatalf("повтор: %v", err)
	}

	if first.Version != second.Version {
		t.Errorf("версия доски выросла на повторе: %d → %d", first.Version, second.Version)
	}
	if first.Patch.Cards[0].ID != second.Patch.Cards[0].ID {
		t.Error("повтор вернул другую карточку")
	}
	if got := f.titles(f.columnA); len(got) != 1 {
		t.Errorf("создано %d карточек вместо одной: %v", len(got), got)
	}
}

// Опорная карточка исчезла между тем, как клиент отрисовал доску, и тем,
// как операция дошла до сервера. Это конфликт, а не ошибка сервера:
// клиент должен получить текущий порядок и пересобраться.
func TestStaleAnchorReturnsConflictWithCurrentOrder(t *testing.T) {
	f := newFixture(t)
	anchor := f.createCard("Опора", f.columnA)
	moving := f.createCard("Подвижная", f.columnA)
	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": anchor})

	_, err := f.apply("MOVE_CARD", map[string]any{
		"cardId": moving, "toColumnId": f.columnA, "place": "after", "afterCardId": anchor})

	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ожидался конфликт, получено: %v", err)
	}
	if conflict.Version == 0 {
		t.Error("в конфликте не проставлена версия доски")
	}
	if len(conflict.Order) != 1 || conflict.Order[0].ID != moving {
		t.Errorf("в конфликте неверный текущий порядок: %+v", conflict.Order)
	}
}

func TestArchivedCardDisappearsButEventsRemain(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Временная", f.columnA)
	res := f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": id})

	if len(res.Patch.RemovedCardIDs) != 1 || res.Patch.RemovedCardIDs[0] != id {
		t.Errorf("патч не сообщил об удалении: %+v", res.Patch)
	}
	if got := f.titles(f.columnA); len(got) != 0 {
		t.Errorf("карточка осталась на доске: %v", got)
	}

	var events int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select count(*) from card_events where card_id = $1`, id).Scan(&events)
	})
	// Мягкое удаление: журнал должен пережить карточку, иначе аналитика
	// потока потеряет её жизненный цикл.
	if events < 2 {
		t.Errorf("в журнале %d событий, ожидались создание и архивация", events)
	}
}

func TestBadRequestsAreRejected(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name    string
		kind    string
		payload map[string]any
	}{
		{"пустое название", "CREATE_CARD", map[string]any{"columnId": f.columnA, "title": "   "}},
		{"неизвестный тип", "МОЖЕТ_БЫТЬ", map[string]any{}},
		{"after без якоря", "CREATE_CARD", map[string]any{"columnId": f.columnA, "title": "x", "place": "after"}},
		{"недопустимый place", "CREATE_CARD", map[string]any{"columnId": f.columnA, "title": "x", "place": "нигде"}},
		{"колонка не указана", "CREATE_CARD", map[string]any{"title": "x"}},
	}
	for _, c := range cases {
		if _, err := f.apply(c.kind, c.payload); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: ожидалась ErrBadRequest, получено %v", c.name, err)
		}
	}

	if _, err := f.apply("MOVE_CARD", map[string]any{
		"cardId": uuid.NewString(), "toColumnId": f.columnA}); err == nil {
		t.Error("перемещение несуществующей карточки прошло без ошибки")
	}
}

// Идентификатор операции придумывает клиент, а таких клиентов много.
// Пока ключ был глобальным, повтор чужого идентификатора из другой
// организации ломал операцию: вставка натыкалась на невидимую строку,
// код читал это как «уже выполнено» и не находил сохранённого результата.
func TestSameOperationIdInTwoOrgsDoesNotCollide(t *testing.T) {
	first := newFixture(t)
	second := newFixture(t)
	shared := uuid.NewString()

	one, err := first.applyWithID(shared, "CREATE_CARD",
		map[string]any{"columnId": first.columnA, "title": "Своя"})
	if err != nil {
		t.Fatalf("первая организация: %v", err)
	}
	two, err := second.applyWithID(shared, "CREATE_CARD",
		map[string]any{"columnId": second.columnA, "title": "Тоже своя"})
	if err != nil {
		t.Fatalf("вторая организация с тем же идентификатором: %v", err)
	}

	if one.Patch.Cards[0].ID == two.Patch.Cards[0].ID {
		t.Error("организации получили одну и ту же карточку")
	}
	if got := first.titles(first.columnA); len(got) != 1 || got[0] != "Своя" {
		t.Errorf("в первой организации %v", got)
	}
	if got := second.titles(second.columnA); len(got) != 1 || got[0] != "Тоже своя" {
		t.Errorf("во второй организации %v", got)
	}
}

// Отмена: карточку, убранную с доски, можно вернуть.
//
// Диалог «вы уверены?» перед архивацией был бы вопросом, ответ на который
// известен заранее; вместо него — обратимость. Проверяется, что возврат
// именно возвращает: карточка снова на доске, и исход с неё снят —
// вернувшаяся работа не является ни сделанной, ни выброшенной.
func TestArchivedCardComesBack(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Передумали", f.columnA)

	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": cardID})
	if got := len(f.titles(f.columnA)); got != 0 {
		t.Fatalf("после архивации на доске %d карточек", got)
	}

	res := f.mustApply("RESTORE_CARD", map[string]any{"cardId": cardID})
	if len(res.Patch.Cards) != 1 || res.Patch.Cards[0].ID != cardID {
		t.Fatalf("возврат не прислал карточку: %+v", res.Patch)
	}
	if got := f.titles(f.columnA); len(got) != 1 || got[0] != "Передумали" {
		t.Errorf("на доске %v", got)
	}

	// Исход снят: иначе вернувшаяся карточка так и осталась бы
	// «выброшенной» и испортила бы пропускную способность молча.
	for _, c := range f.snapshot().Cards {
		if c.ID == cardID && c.Outcome != nil {
			t.Errorf("исход остался: %q", *c.Outcome)
		}
	}
}

// Повтор отмены безобиден: сообщение с кнопкой живёт десять секунд,
// и нажать дважды — обычное дело.
func TestRestoringTwiceIsHarmless(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Дважды", f.columnA)
	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": cardID})
	f.mustApply("RESTORE_CARD", map[string]any{"cardId": cardID})

	_, err := f.apply("RESTORE_CARD", map[string]any{"cardId": cardID})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("повторный возврат ответил %v, ожидался конфликт с объяснением", err)
	}
	if got := len(f.titles(f.columnA)); got != 1 {
		t.Errorf("после повтора на доске %d карточек", got)
	}
}
