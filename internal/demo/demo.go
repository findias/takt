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

	"github.com/findias/takt/internal/apiclient"
	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/org"
	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/team"
	"github.com/findias/takt/internal/webhook"
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
	if err := f.settleDeliveries(); err != nil {
		return fmt.Errorf("доставки подписки: %w", err)
	}
	return nil
}

// settleDeliveries доводит доставки недостижимой подписки до конца сразу.
//
// Иначе стенд оставляет в общей очереди два десятка доставок, которые
// созревают все разом. Дорого это не стенду, а проверкам: они ходят
// по этой же базе, работник берёт пачкой десять самых старых, и каждая
// попытка стучится в пустоту до десяти секунд — своя доставка проверки
// не отправляется вовсе, пока её срок ожидания не выйдет. Так месяцами
// мигала проверка доставки вебхука, и списывалось это на соседей.
//
// Экран от этого не беднеет: сдавшаяся доставка показывает и ошибку,
// и отключённую подписку, и кнопку «повторить» — то есть ровно то,
// ради чего подписка на недостижимый адрес и заведена. Разница только
// в том, что этого состояния стенд достигает сразу, а не через два часа
// удвоений.
func (f *filler) settleDeliveries() error {
	const reason = `Post "https://example.test/hooks/board": dial tcp: lookup example.test: no such host`
	return f.db.InTenant(f.ctx, f.orgID, f.owner(), func(tx pgx.Tx) error {
		// Восемь попыток — столько же, после скольких сдаётся работник:
		// число видно на экране, и оно должно значить то же самое.
		if _, err := tx.Exec(f.ctx, `
			update webhook_deliveries
			   set attempts = 8, failed_at = now(), last_error = $1
			 where delivered_at is null and failed_at is null`, reason); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `
			update webhooks set disabled_at = now(), last_error = $1
			 where disabled_at is null`, reason)
		return err
	})
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
	// ни отключения, ни кнопки «повторить». До конца эти доставки
	// доводит не работник, а сам стенд — см. settleDeliveries.
	hooks := webhook.New(f.db, board.EventNames())
	if _, err := hooks.Create(f.ctx, f.orgID, f.owner(), "Оповещение дежурного",
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
	if _, err := f.teams.Grant(f.ctx, f.orgID, f.owner(),
		f.people["gleb@example.test"], nil); err != nil {
		return err
	}

	// Одно подразделение — из каталога: у такого состав ведёт провайдер,
	// и экран об этом предупреждает. Без такого узла предупреждение
	// не показывалось бы, а значит, не попадало бы и в снимки — вид,
	// которого нет в снимках, не смотрит никто.
	//
	// Метка ставится запросом, а не операцией: заводить группу
	// по-настоящему пришлось бы ключом каталога, а это ключ, который
	// дальше /scim/v2 не пускают, — стенду он ни к чему.
	if err := f.db.InTenant(f.ctx, f.orgID, f.owner(), func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update teams set external_id = 'dir-prodazhi' where id = $1`, prodazhi.ID)
		return err
	}); err != nil {
		return err
	}

	// Убранное подразделение — ради экрана, а не ради данных.
	// Раздел «Убранные подразделения» показывается только тогда, когда
	// в архиве что-то есть, и без этого узла его не видел бы никто:
	// в наборе снимков он просто не появлялся бы. Вид, которого нет
	// в снимках, — это вид, который никто ни разу не смотрел.
	kurs, err := f.teams.Create(f.ctx, f.orgID, f.owner(), "Курсы", &razrabotka.ID)
	if err != nil {
		return err
	}
	return f.teams.Archive(f.ctx, f.orgID, f.owner(), kurs.ID)
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
	// Части лежат на разных людях, и у одной идёт разговор: без этого
	// на доске не увидеть, ради чего в строке подзадачи стоят аватар
	// и счётчик реплик.
	parts := map[string]string{
		"Собрать сборку":       "boris@example.test",
		"Прогнать нагрузочные": "vera@example.test",
	}
	partIDs := map[string]string{}
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
		partIDs[title] = part
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
	// Часть, которая по доске не ездит и отмечена сделанной руками:
	// согласование текста — не работа на неделю, и гонять её по колонкам
	// никто не станет. Без такой части на экране не увидеть ни
	// зачёркнутой строки, ни того, что прогресс родителя считает отметку
	// наравне с колонкой финиша.
	res, err := f.apply(b.ID, "CREATE_SUBTASK", map[string]any{
		"parentCardId": ids["Выпустить релиз склада"],
		"title":        "Согласовать текст письма клиентам"})
	if err != nil {
		return err
	}
	for _, c := range res.Patch.Cards {
		if c.ID == ids["Выпустить релиз склада"] {
			continue
		}
		if _, err := f.apply(b.ID, "SET_CARD_DONE", map[string]any{
			"cardId": c.ID, "done": true}); err != nil {
			return err
		}
	}
	// Одна часть держит другую: нагрузочные не прогнать, пока не собрана
	// сборка. Без такой связи на доске не увидеть, зачем она нужна
	// с обеих сторон — «держит 1» у одной и «ждёт» у другой.
	if _, err := f.apply(b.ID, "LINK_CARDS", map[string]any{
		"fromCard": partIDs["Собрать сборку"],
		"toCard":   partIDs["Прогнать нагрузочные"],
		"kind":     "blocks"}); err != nil {
		return err
	}

	if _, err := f.apply(b.ID, "CREATE_SUBTASK", map[string]any{
		"parentCardId": ids["Выпустить релиз склада"],
		"title":        "Поднять квоту на хранилище", "boardId": neighbour.ID}); err != nil {
		return err
	}

	// Задача стоит из-за собственной части, и часть эта — у соседей.
	// Блокировка названа ссылкой: без неё «ждём смежников» не проверить
	// глазами — куда идти и что там сейчас, видно только по карточке.
	//
	// Идентификатор берётся из снимка, а не из патча: карточка легла
	// на чужую доску, и патч доски-заказчика её не несёт — по той же
	// причине, по которой чужую работу здесь и показывают отдельно.
	withParts, err := f.boards.Snapshot(f.ctx, f.orgID, f.owner(), b.ID)
	if err != nil {
		return err
	}
	holder := ""
	for _, c := range withParts.Linked {
		if c.Title == "Поднять квоту на хранилище" {
			holder = c.ID
		}
	}
	if holder == "" {
		return fmt.Errorf("часть у соседей не нашлась в снимке доски")
	}
	if _, err := f.apply(b.ID, "BLOCK_CARD", map[string]any{
		"cardId":       ids["Выпустить релиз склада"],
		"reason":       "смежники не подтвердили формат выгрузки",
		"blockingCard": holder}); err != nil {
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
		//
		// Не `update`: у card_events нет политики на изменение, и это
		// не упущение — журнал только дописывается. Прежняя версия
		// обновляла ноль строк и молчала об этом, как всякая правка
		// данных под RLS; поломку нашла проверка стенда 19 августа,
		// а глазами она выглядела как «вся история доски случилась
		// в одну минуту».
		//
		// Поэтому строки перекладываются: удалить (владельцу можно)
		// и вставить заново с нужным временем (дописывать можно тому,
		// кто пишет в доску). Порядок вставки — по новому времени,
		// чтобы растущий id шёл в ту же сторону, что и лента.
		// Читаем, удаляем, вставляем заново — тремя шагами, а не одним
		// запросом с data-modifying CTE: тот однажды удалил строки
		// и не вставил их обратно, и понять почему по одному запросу
		// нельзя. Демонстрационных событий сотня, скорость здесь
		// не стоит ни одной непонятной строки.
		type event struct {
			org, board, card string
			actor            *string
			kind             string
			from, to         *string
			payload          []byte
			at               time.Time
		}
		rows, err := tx.Query(f.ctx, `
			select e.org_id, e.board_id, e.card_id, e.actor_id, e.type,
			       e.from_column, e.to_column, e.payload,
			       c.created_at + (e.id % 7 || ' hours')::interval
			  from card_events e join cards c on c.id = e.card_id
			 order by 9`)
		if err != nil {
			return err
		}
		var events []event
		for rows.Next() {
			var e event
			if err := rows.Scan(&e.org, &e.board, &e.card, &e.actor, &e.kind,
				&e.from, &e.to, &e.payload, &e.at); err != nil {
				rows.Close()
				return err
			}
			events = append(events, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		if _, err := tx.Exec(f.ctx, `delete from card_events`); err != nil {
			return err
		}
		for _, e := range events {
			if _, err := tx.Exec(f.ctx, `
				insert into card_events
					(org_id, board_id, card_id, actor_id, type,
					 from_column, to_column, payload, at)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				e.org, e.board, e.card, e.actor, e.kind,
				e.from, e.to, e.payload, e.at); err != nil {
				return err
			}
		}
		return nil
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
