// Пакет demo наполняет пустую базу данными, на которые можно смотреть.
//
// Заведён ради работы над интерфейсом. Поднятый стенд пуст: чтобы увидеть
// хоть один экран в рабочем виде, приходилось руками заводить организацию,
// людей, доски, карточки со свойствами, итерации и архив — час работы
// перед тем, как посмотреть на пять минут. Смотреть на пустой интерфейс
// бесполезно: почти всякая ошибка вёрстки видна только на настоящей
// длине текста, настоящем числе меток и настоящем счётчике.
//
// Данные заводятся теми же сервисами, что и живые, а не вставками
// в таблицы: демо, обошедшее правила, показывало бы состояния, в которые
// приложение попасть не может.
//
// Единственное отступление — отметки времени. Карточка, законченная
// секунду назад, даёт время цикла в ноль дней, и все метрики выглядят
// сломанными. Поэтому отметки потока после наполнения сдвигаются в
// прошлое прямым запросом: это данные о прошлом, которого не было,
// и притворяться, что они получены иначе, незачем.
package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/apiclient"
	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/board"
	"github.com/konkov/agile/internal/org"
	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/team"
	"github.com/konkov/agile/internal/webhook"
)

// Password — пароль всех демонстрационных людей. Один на всех: это стенд,
// а не установка.
const Password = "parol12345"

// Person — кого заводим и с какой ролью.
type Person struct {
	Email string
	Name  string
	Role  string
}

// People — состав демонстрационной организации. Первый в списке —
// владелец, под ним и входят.
var People = []Person{
	{Email: "anna@example.test", Name: "Анна Королёва", Role: auth.RoleOwner},
	{Email: "boris@example.test", Name: "Борис Дятлов", Role: auth.RoleMember},
	{Email: "vera@example.test", Name: "Вера Соколова", Role: auth.RoleMember},
	{Email: "gleb@example.test", Name: "Глеб Тишин", Role: auth.RoleViewer},
}

// OrgName — название демонстрационной организации.
const OrgName = "Северный проект"

type filler struct {
	ctx     context.Context
	db      *store.Store
	orgs    *org.Service
	teams   *team.Service
	boards  *board.Service
	orgID   string
	people  map[string]string // почта → идентификатор
	teamIDs map[string]string // название подразделения → идентификатор
}

// ErrAlreadyFilled — демонстрационные данные уже заведены. Не ошибка
// сама по себе: повторный запуск не должен ни падать невнятно, ни
// заводить вторую такую же организацию. Обновляются они только
// с чистой базы — `make stand`.
var ErrAlreadyFilled = errors.New("демонстрационные данные уже заведены")

// Fill наполняет базу. Возвращает ошибку при первой же неудаче: половина
// демонстрационных данных хуже, чем их отсутствие, — по ней нельзя понять,
// что показывает интерфейс, а что не доехало.
func Fill(ctx context.Context, db *store.Store) error {
	f := &filler{
		ctx:    ctx,
		db:     db,
		orgs:   org.New(db),
		teams:  team.New(db),
		boards: board.New(db),
		people: map[string]string{},
	}

	var taken bool
	if err := db.Pool.QueryRow(ctx,
		`select exists (select 1 from users where lower(email) = $1)`,
		People[0].Email).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return ErrAlreadyFilled
	}

	if err := f.organization(); err != nil {
		return fmt.Errorf("организация: %w", err)
	}
	if err := f.structure(); err != nil {
		return fmt.Errorf("структура: %w", err)
	}
	if err := f.workspace(); err != nil {
		return fmt.Errorf("доски: %w", err)
	}
	if err := f.backdate(); err != nil {
		return fmt.Errorf("сдвиг отметок в прошлое: %w", err)
	}
	return nil
}

func (f *filler) owner() string { return f.people[People[0].Email] }

// --- организация и люди ---

func (f *filler) organization() error {
	for i, p := range People {
		hash, err := auth.HashPassword(Password)
		if err != nil {
			return err
		}
		var id string
		err = f.db.Pool.QueryRow(f.ctx, `
			insert into users (email, name, password_hash)
			values ($1, $2, $3) returning id`, p.Email, p.Name, hash).Scan(&id)
		if err != nil {
			return fmt.Errorf("личность %s: %w", p.Email, err)
		}
		f.people[p.Email] = id

		if i == 0 {
			m, err := f.orgs.Create(f.ctx, OrgName, id)
			if err != nil {
				return err
			}
			f.orgID = m.OrgID
			continue
		}
		// Остальные вписываются участием напрямую: приглашение по ссылке
		// требует второй стороны, а её на стенде нет.
		if _, err := f.db.Pool.Exec(f.ctx, `
			insert into memberships (org_id, user_id, role) values ($1, $2, $3)`,
			f.orgID, id, p.Role); err != nil {
			return err
		}
	}

	// Ключ интеграции. На стенде он нужен не ради самого ключа, а ради
	// того, что его служебная личность стоит в списке людей: пока такой
	// личности нет, экран «Команда» показывает только людей, и ошибку
	// «ключу предлагают роль и удаление данных» на нём не увидеть.
	if _, err := apiclient.New(f.db).Create(f.ctx, f.orgID, f.owner(), "Обмен со складом",
		[]string{apiclient.ScopeBoardsRead, apiclient.ScopeBoardsWrite}, nil); err != nil {
		return fmt.Errorf("ключ интеграции: %w", err)
	}

	// Подписка на события — здесь же и до досок, чтобы к моменту показа
	// у неё был журнал доставок. Адрес заведомо недостижим, и это
	// не небрежность: интереснее всего экран подписок выглядит именно
	// тогда, когда доставка не идёт, — иначе не увидеть ни ошибки,
	// ни отключения, ни кнопки «повторить». Работник сдаётся после
	// восьми попыток и подписку отключает, так что стенд не остаётся
	// с вечным стуком в пустоту.
	if _, err := webhook.New(f.db).Create(f.ctx, f.orgID, f.owner(), "Оповещение дежурного",
		"https://example.test/hooks/board",
		[]string{"card.created", "card.moved", "card.blocked"}); err != nil {
		return fmt.Errorf("подписка на события: %w", err)
	}
	return nil
}

// --- дерево подразделений ---

func (f *filler) structure() error {
	razrabotka, err := f.teams.Create(f.ctx, f.orgID, f.owner(), "Разработка", nil)
	if err != nil {
		return err
	}
	platforma, err := f.teams.Create(f.ctx, f.orgID, f.owner(), "Платформа", &razrabotka.ID)
	if err != nil {
		return err
	}
	yadro, err := f.teams.Create(f.ctx, f.orgID, f.owner(), "Ядро", &platforma.ID)
	if err != nil {
		return err
	}
	prodazhi, err := f.teams.Create(f.ctx, f.orgID, f.owner(), "Продажи", nil)
	if err != nil {
		return err
	}
	f.teamIDs = map[string]string{
		"Разработка": razrabotka.ID, "Платформа": platforma.ID,
		"Ядро": yadro.ID, "Продажи": prodazhi.ID,
	}

	if err := f.teams.AddMember(f.ctx, f.orgID, f.owner(), yadro.ID,
		f.people["boris@example.test"]); err != nil {
		return err
	}
	if err := f.teams.AddMember(f.ctx, f.orgID, f.owner(), prodazhi.ID,
		f.people["vera@example.test"]); err != nil {
		return err
	}
	// Борис отвечает за «Платформу» целиком, Глеб наблюдает за всей
	// организацией: на экране структуры видно оба вида полномочий.
	if _, err := f.teams.GrantAdmin(f.ctx, f.orgID, f.owner(),
		f.people["boris@example.test"], platforma.ID); err != nil {
		return err
	}
	_, err = f.teams.Grant(f.ctx, f.orgID, f.owner(), f.people["gleb@example.test"], nil)
	return err
}

// --- доски, карточки и всё, что на них висит ---

func (f *filler) workspace() error {
	postavki, err := f.boards.Create(f.ctx, f.orgID, f.owner(), "Поставки", "ПОСТ")
	if err != nil {
		return err
	}
	platforma, err := f.boards.Create(f.ctx, f.orgID, f.owner(), "Платформа", "ПЛАТ")
	if err != nil {
		return err
	}
	naym, err := f.boards.Create(f.ctx, f.orgID, f.owner(), "Найм", "НАЙМ")
	if err != nil {
		return err
	}

	// Три вида видимости разом: иначе на экране доступа нечего смотреть.
	if err := f.boards.SetAccess(f.ctx, f.orgID, f.owner(), platforma.ID,
		board.VisibilityTeam, ptr(f.teamIDs["Платформа"])); err != nil {
		return err
	}
	// Открытая доска со своим подразделением: «чья доска» и «кому видно»
	// разные вопросы, и в дереве структуры она видна по первому из них.
	// Без такой доски на экране структуры не увидеть, что доски узла
	// вообще бывают.
	if err := f.boards.SetAccess(f.ctx, f.orgID, f.owner(), postavki.ID,
		board.VisibilityOrg, ptr(f.teamIDs["Продажи"])); err != nil {
		return err
	}
	// Закрытие вписывает закрывающего само — отдельного «впиши себя»
	// больше не нужно.
	if err := f.boards.SetAccess(f.ctx, f.orgID, f.owner(), naym.ID,
		board.VisibilityPrivate, nil); err != nil {
		return err
	}

	// Метки и своё поле — на всю организацию.
	labels := map[string]string{}
	for name, tone := range map[string]string{"Срочно": "rose", "Смежники": "teal", "Риск": "amber"} {
		l, err := f.boards.CreateLabel(f.ctx, f.orgID, f.owner(), name, tone)
		if err != nil {
			return err
		}
		labels[name] = l.ID
	}
	customer, err := f.boards.CreateField(f.ctx, f.orgID, f.owner(), "Заказчик", "text", nil)
	if err != nil {
		return err
	}

	if err := f.fillPostavki(postavki, platforma, labels, customer.ID); err != nil {
		return err
	}
	return f.fillPlatforma(platforma)
}

func (f *filler) fillPostavki(b, neighbour board.Info, labels map[string]string, field string) error {
	snap, err := f.boards.Snapshot(f.ctx, f.orgID, f.owner(), b.ID)
	if err != nil {
		return err
	}
	queue, doing, done := snap.Columns[0], snap.Columns[1], snap.Columns[2]

	// Разметка колонки: политика входа словами и жёсткий лимит. Без них
	// шапка колонки на экране пуста, а она — половина её высоты.
	if _, err := f.apply(b.ID, "UPDATE_COLUMN", map[string]any{
		"columnId": doing.ID, "wipLimit": 4, "wipLimitHard": true,
		"policy": "Есть постановка, известен исполнитель и срок",
	}); err != nil {
		return err
	}
	// Обещание доски: с ним появляется метка «дольше обещанного».
	if err := f.boards.SetSLE(f.ctx, f.orgID, f.owner(), b.ID, ptr(8), 85); err != nil {
		return err
	}

	type card struct {
		title    string
		column   string
		estimate float64
		labels   []string
		who      []string
		note     string
	}
	plan := []card{
		{"Согласовать смету с подрядчиком", "Очередь", 5, []string{"Срочно"}, nil,
			"Смета на второй этап. Спорные позиции — леса и вывоз грунта."},
		{"Обновить регламент приёмки", "Очередь", 3, nil, []string{"boris@example.test"}, ""},
		{"Разобрать обращения за неделю", "В работе", 2, nil,
			[]string{"vera@example.test", "boris@example.test"},
			"Восемнадцать обращений, половина — про сроки поставки."},
		{"Выпустить релиз склада", "В работе", 8, []string{"Риск", "Смежники"},
			[]string{"anna@example.test"}, "Ждём подтверждения от смежников по интеграции."},
		{"Перевезти стенд в новый офис", "Готово", 3, nil, []string{"boris@example.test"}, ""},
		{"Закрыть акт за июль", "Готово", 2, nil, []string{"vera@example.test"}, ""},
		{"Проверить остатки на складе", "Готово", 1, nil, nil, ""},
	}
	columns := map[string]string{"Очередь": queue.ID, "В работе": doing.ID, "Готово": done.ID}
	// Путь до места: в «Готово» карточка приезжает через работу, а не
	// прыжком — иначе в журнале не окажется начала, и время цикла считать
	// будет не из чего.
	route := map[string][]string{
		"Очередь":  nil,
		"В работе": {"В работе"},
		"Готово":   {"В работе", "Готово"},
	}

	ids := map[string]string{}
	for _, c := range plan {
		// Все заводятся в очереди и переносятся дальше операциями, а не
		// создаются сразу в нужной колонке: так у них появляется история
		// переходов, без которой лента доски пуста, а полосы потока
		// показывают один день.
		res, err := f.apply(b.ID, "CREATE_CARD", map[string]any{
			"columnId": queue.ID, "title": c.title, "place": "end"})
		if err != nil {
			return fmt.Errorf("карточка %q: %w", c.title, err)
		}
		id := res.Patch.Cards[0].ID
		ids[c.title] = id

		for _, step := range route[c.column] {
			if _, err := f.apply(b.ID, "MOVE_CARD", map[string]any{
				"cardId": id, "toColumnId": columns[step], "place": "end"}); err != nil {
				return fmt.Errorf("перенос %q в %q: %w", c.title, step, err)
			}
		}

		if _, err := f.apply(b.ID, "UPDATE_CARD", map[string]any{
			"cardId": id, "estimate": c.estimate, "description": c.note}); err != nil {
			return err
		}
		for _, name := range c.labels {
			if _, err := f.apply(b.ID, "LABEL_CARD", map[string]any{
				"cardId": id, "labelId": labels[name]}); err != nil {
				return err
			}
		}
		for _, email := range c.who {
			if _, err := f.apply(b.ID, "ASSIGN_CARD", map[string]any{
				"cardId": id, "userId": f.people[email]}); err != nil {
				return err
			}
		}
	}

	// Своё поле, блокировка с причиной, разбиение на подзадачи — в том
	// числе на доску соседей, ради строки «Доска «Платформа»».
	if _, err := f.apply(b.ID, "SET_CARD_FIELD", map[string]any{
		"cardId": ids["Согласовать смету с подрядчиком"], "fieldId": field,
		"value": "Северстрой"}); err != nil {
		return err
	}
	// Уровни приоритета видно только на тех карточках, где он не
	// средний, — а на доске из одних средних не увидеть, чем верх
	// шкалы отличается от низа.
	for title, level := range map[string]string{
		"Согласовать смету с подрядчиком": "highest",
		"Разобрать обращения за неделю":   "high",
		"Обновить регламент приёмки":      "low",
	} {
		if _, err := f.apply(b.ID, "UPDATE_CARD", map[string]any{
			"cardId": ids[title], "priority": level}); err != nil {
			return err
		}
	}
	// Одно обязательство наружу: без него на доске не увидеть, чем срок
	// отличается от возраста, а «успеваем ли к четвергу» не спросить.
	if _, err := f.apply(b.ID, "UPDATE_CARD", map[string]any{
		"cardId": ids["Согласовать смету с подрядчиком"], "dueOn": date(2)}); err != nil {
		return err
	}
	if _, err := f.apply(b.ID, "BLOCK_CARD", map[string]any{
		"cardId": ids["Выпустить релиз склада"],
		"reason": "смежники не подтвердили формат выгрузки"}); err != nil {
		return err
	}
	// Части лежат на разных людях, и у одной идёт разговор: без этого
	// на доске не увидеть, ради чего в строке подзадачи стоят аватар
	// и счётчик реплик.
	parts := map[string]string{
		"Собрать сборку":       "boris@example.test",
		"Прогнать нагрузочные": "vera@example.test",
	}
	for _, title := range []string{"Собрать сборку", "Прогнать нагрузочные"} {
		res, err := f.apply(b.ID, "CREATE_SUBTASK", map[string]any{
			"parentCardId": ids["Выпустить релиз склада"], "title": title})
		if err != nil {
			return err
		}
		// В патче связывания первой идёт родительская карточка, второй —
		// сама часть: берём ту, что не родитель.
		part := ""
		for _, c := range res.Patch.Cards {
			if c.ID != ids["Выпустить релиз склада"] {
				part = c.ID
			}
		}
		if part == "" {
			return fmt.Errorf("подзадача %q не вернулась в патче", title)
		}
		if _, err := f.apply(b.ID, "ASSIGN_CARD", map[string]any{
			"cardId": part, "userId": f.people[parts[title]]}); err != nil {
			return err
		}
		if title != "Собрать сборку" {
			continue
		}
		for _, text := range []string{
			"Сборка встала на шаге с миграциями.",
			"Поправил, пересобираю.",
		} {
			if _, err := f.boards.AddComment(f.ctx, f.orgID,
				f.people[parts[title]], b.ID, part, text, nil, nil); err != nil {
				return err
			}
		}
	}
	if _, err := f.apply(b.ID, "CREATE_SUBTASK", map[string]any{
		"parentCardId": ids["Выпустить релиз склада"],
		"title":        "Поднять квоту на хранилище", "boardId": neighbour.ID}); err != nil {
		return err
	}

	// Обсуждение с веткой: одна реплика в панели не показывает ничего.
	root, err := f.boards.AddComment(f.ctx, f.orgID, f.owner(), b.ID,
		ids["Выпустить релиз склада"],
		"Смежники обещали ответить до среды. Если не ответят — режем интеграцию из этого релиза.",
		nil, []string{f.people["boris@example.test"]})
	if err != nil {
		return err
	}
	if _, err := f.boards.AddComment(f.ctx, f.orgID, f.people["boris@example.test"], b.ID,
		ids["Выпустить релиз склада"],
		"Написал им ещё раз, приложил пример выгрузки.", &root.ID, nil); err != nil {
		return err
	}

	// Сохранённый вид, итерации — открытая и закрытая, архив карточек.
	if _, err := f.boards.SaveView(f.ctx, f.orgID, f.owner(), b.ID,
		"Мои срочные", "assignee=me&label=Срочно"); err != nil {
		return err
	}
	if err := f.iterations(b, ids); err != nil {
		return err
	}
	return f.archive(b, columns[queue.Name])
}

// iterations заводит закрытую итерацию с составом и идущую следом —
// на закрытой видно отчёт, на открытой то, как она выглядит в работе.
func (f *filler) iterations(b board.Info, ids map[string]string) error {
	past, err := f.boards.CreateIteration(f.ctx, f.orgID, f.owner(), b.ID,
		"Неделя 32", "Закрыть июльские хвосты",
		date(-21), date(-15))
	if err != nil {
		return err
	}
	for _, title := range []string{"Перевезти стенд в новый офис", "Закрыть акт за июль",
		"Проверить остатки на складе", "Обновить регламент приёмки"} {
		if _, err := f.apply(b.ID, "ADD_TO_ITERATION", map[string]any{
			"cardId": ids[title], "iterationId": past.ID}); err != nil {
			return err
		}
	}
	// Одну убрали по дороге — на отчёте видно долю выбывшего.
	if _, err := f.apply(b.ID, "REMOVE_FROM_ITERATION", map[string]any{
		"cardId": ids["Обновить регламент приёмки"], "iterationId": past.ID}); err != nil {
		return err
	}
	if err := f.boards.CloseIteration(f.ctx, f.orgID, f.owner(), b.ID, past.ID); err != nil {
		return err
	}

	now, err := f.boards.CreateIteration(f.ctx, f.orgID, f.owner(), b.ID,
		"Неделя 33", "Довести релиз склада до стенда", date(-4), date(2))
	if err != nil {
		return err
	}
	for _, title := range []string{"Выпустить релиз склада", "Разобрать обращения за неделю"} {
		if _, err := f.apply(b.ID, "ADD_TO_ITERATION", map[string]any{
			"cardId": ids[title], "iterationId": now.ID}); err != nil {
			return err
		}
	}
	return nil
}

func (f *filler) archive(b board.Info, queueID string) error {
	for _, title := range []string{"Старый регламент приёмки", "Отменённая закупка бытовки"} {
		res, err := f.apply(b.ID, "CREATE_CARD", map[string]any{
			"columnId": queueID, "title": title, "place": "end"})
		if err != nil {
			return err
		}
		if _, err := f.apply(b.ID, "ARCHIVE_CARD", map[string]any{
			"cardId": res.Patch.Cards[0].ID}); err != nil {
			return err
		}
	}
	return nil
}

func (f *filler) fillPlatforma(b board.Info) error {
	snap, err := f.boards.Snapshot(f.ctx, f.orgID, f.owner(), b.ID)
	if err != nil {
		return err
	}
	for _, c := range []struct {
		title  string
		column int
	}{
		{"Вынести очередь в отдельный сервис", 1},
		{"Обновить базу до 16-й версии", 0},
		{"Разобраться с ростом времени ответа", 0},
	} {
		if _, err := f.apply(b.ID, "CREATE_CARD", map[string]any{
			"columnId": snap.Columns[c.column].ID, "title": c.title, "place": "end"}); err != nil {
			return err
		}
	}
	return nil
}

// --- отметки времени ---

// backdate разносит отметки потока по прошлым неделям.
//
// Всё, что завёл стенд, случилось секунду назад: время цикла выходит
// в ноль дней, пропускная способность стоит одним столбиком, возраст
// работы — ноль у всех. По таким метрикам нельзя судить ни об их
// вёрстке, ни об их смысле. Здесь и только здесь данные правятся
// в обход операций: сочинить прошлое иначе нечем.
func (f *filler) backdate() error {
	return f.db.InTenant(f.ctx, f.orgID, f.owner(), func(tx pgx.Tx) error {
		// Завершённые растягиваются по трём неделям, начатые — по дням
		// перед завершением: из разницы и получается время цикла.
		if _, err := tx.Exec(f.ctx, `
			with numbered as (
				select id, row_number() over (order by created_at) as n
				  from cards where outcome = 'done'
			)
			update cards c
			   set finished_at = now() - (n.n * 3 || ' days')::interval,
			       started_at  = now() - (n.n * 3 + 2 + (n.n % 4) || ' days')::interval,
			       created_at  = now() - (n.n * 3 + 6 || ' days')::interval
			  from numbered n where n.id = c.id`); err != nil {
			return err
		}
		// Идущая работа стареет: без этого возраст незавершённого пуст,
		// а метка «дольше обещанного» не появляется никогда.
		if _, err := tx.Exec(f.ctx, `
			with numbered as (
				select id, row_number() over (order by created_at) as n
				  from cards
				 where started_at is not null and finished_at is null
			)
			update cards c
			   set started_at       = now() - (n.n * 5 || ' days')::interval,
			       column_entered_at = now() - (n.n * 5 || ' days')::interval,
			       created_at        = now() - (n.n * 5 + 3 || ' days')::interval
			  from numbered n where n.id = c.id`); err != nil {
			return err
		}
		// Журнал переходов сдвигается следом: лента доски, где всё
		// произошло в одну минуту, читается как сбой.
		_, err := tx.Exec(f.ctx, `
			update card_events e
			   set at = c.created_at + (e.id % 7 || ' hours')::interval
			  from cards c where c.id = e.card_id`)
		return err
	})
}

func (f *filler) apply(boardID, kind string, payload map[string]any) (board.Result, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return board.Result{}, err
	}
	return f.boards.Apply(f.ctx, f.orgID, f.owner(), boardID, board.Request{
		OperationID: uuid.NewString(), Type: kind, Payload: raw,
	})
}

func ptr[T any](v T) *T { return &v }

// date возвращает день со сдвигом от сегодня в том виде, в каком его
// принимают итерации.
func date(shiftDays int) string {
	return time.Now().AddDate(0, 0, shiftDays).Format("2006-01-02")
}
