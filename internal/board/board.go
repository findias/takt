// Package board — доменная модель доски и применение операций над ней.
package board

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/rank"
	"github.com/findias/takt/internal/store"
)

var ErrNotFound = errors.New("доска не найдена")

// ErrArchivedBoard — доска есть и человеку видна, но убрана в архив.
// Отдельно от «не найдена» потому, что это разные положения дел и разные
// следующие шаги: несуществующую доску искать бесполезно, архивную —
// достаточно вернуть одной кнопкой. Пока их не различали, человек,
// открывший ссылку на убранную доску, читал «не найдена» и шёл искать
// поломку там, где её нет.
//
// Различать безопасно: до этой проверки доходит только тот, кому доска
// видна политикой, — существование чужих досок по-прежнему не
// подтверждается.
var ErrArchivedBoard = errors.New("доска в архиве")

// ErrReadOnlyBoard — доска видна, но изменять её нельзя: так у наблюдателя
// поддерева и у всякого, кому доска досталась только на чтение.
//
// Отдельная ошибка нужна ради честного ответа. Правило простое и общее:
// не видишь — «нет такой доски», видишь — «нельзя». Отвечать «не найдена»
// тому, кто эту доску прямо сейчас читает, значит отправить его искать
// несуществующую поломку.
var ErrReadOnlyBoard = errors.New("доска доступна вам только для чтения")

// ErrOwnerOnly — действие необратимо, и потому доступно одному владельцу
// организации. Администратор подразделения сюда не допущен намеренно:
// его полномочие про то, как идёт работа, а не про то, чтобы стереть
// её следы.
var ErrOwnerOnly = errors.New("удалить насовсем может только владелец организации")

// ErrBadKey — ключ доски не годится в префикс номера. ErrKeyTaken — годится,
// но уже занят. Разные ошибки потому, что и поправить их нужно по-разному:
// первую — переписав ключ, вторую — выбрав другой.
var (
	ErrBadKey   = errors.New("ключ доски — от двух до шести букв или цифр, начиная с буквы")
	ErrKeyTaken = errors.New("такой ключ уже занят другой доской")
)

type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
	// Обещание доски: «85% работы проходит доску за 8 дней». Пусто —
	// обещания нет, и это честное состояние: доска без истории обещать
	// не может.
	//
	// Хранится, а не считается на лету. Процентиль по истории едет вместе
	// с работой: команда, у которой дела пошли хуже, увидела бы выросший
	// процентиль и не заметила ухудшения. Обещание между пересмотрами
	// неподвижно, и по нему видно, что стало хуже.
	SLEDays        *int `json:"sleDays"`
	SLEProbability int  `json:"sleProbability"`
	// Ключ доски — префикс номеров её карточек: ПРО в ПРО-142. Задаётся
	// при создании и не меняется: номер карточки хранится целиком, и
	// смена ключа развела бы на одной доске два разных префикса.
	Key string `json:"key"`
	// Можно ли в доску писать. Спрашивает список досок — из него выбирают,
	// куда поставить работу соседям, и предлагать доску, которая откажет,
	// значит обещать невыполнимое.
	//
	// Пусто, а не «нельзя», когда не спрашивали: снимку доски этот ответ
	// не нужен, а стоит он прохода по всем доскам организации. Разница
	// между «нельзя» и «не спрашивали» важна ровно потому, что первое
	// закрывает выбор, а второе — нет.
	Writable *bool `json:"writable,omitempty"`
	// Кому доска видна и сколько на ней работы. Тоже только для списка:
	// список досок был беднее дерева подразделений, где у той же доски
	// написано «ПЛАТ · своей команде», — и выбирать доску приходилось
	// по одному названию.
	//
	// Пусто, когда не спрашивали, — по той же причине, что и `Writable`:
	// снимку доски эти ответы не нужны, а счёт карточек стоит прохода
	// по ним.
	Visibility *string `json:"visibility,omitempty"`
	Cards      *int    `json:"cards,omitempty"`
	// Какому подразделению доска видна, когда видимость — «своей
	// команде». Без него строка списка отвечает «своей» тому, кто
	// состоит в трёх подразделениях сразу, и ответом это не является.
	TeamID *string `json:"teamId,omitempty"`
}

// boardFields — общий список полей доски, по тем же соображениям, что
// columnFields и cardFields ниже: список повторяется в нескольких
// запросах, и разъехавшийся порядок ловится только в рантайме.
const boardFields = `id, name, version, sle_days, sle_probability, key`

func scanBoard(row pgx.Row) (Info, error) {
	var b Info
	err := row.Scan(&b.ID, &b.Name, &b.Version, &b.SLEDays, &b.SLEProbability, &b.Key)
	return b, err
}

// Вид колонки. Очередей и стадий работы на доске бывает много, поэтому вид
// колонки и границы потока — разные вещи: границы отмечаются отдельно,
// признаками IsStartedPoint и IsFinishedPoint.
const (
	KindQueue      = "queue"
	KindInProgress = "in_progress"
	KindDone       = "done"
)

type Column struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Kind     string `json:"kind"`
	// Точки старта и финиша задают, что считать началом и концом работы.
	// Без них не определены ни время цикла, ни возраст карточки.
	IsStartedPoint  bool   `json:"isStartedPoint"`
	IsFinishedPoint bool   `json:"isFinishedPoint"`
	Policy          string `json:"policy"`
	WIPLimit        *int   `json:"wipLimit"`
	// Мягкий лимит только подсвечивается; жёсткий отвечает конфликтом.
	WIPLimitHard bool `json:"wipLimitHard"`
}

// columnFields — общий список полей колонки. Он повторяется в полудюжине
// запросов, и разъехавшийся порядок полей здесь ловится только в рантайме.
const columnFields = `id, name, position, kind, is_started_point,
	is_finished_point, policy, wip_limit, wip_limit_hard`

func scanColumn(row pgx.Row) (Column, error) {
	var c Column
	err := row.Scan(&c.ID, &c.Name, &c.Position, &c.Kind, &c.IsStartedPoint,
		&c.IsFinishedPoint, &c.Policy, &c.WIPLimit, &c.WIPLimitHard)
	return c, err
}

type Card struct {
	ID string `json:"id"`
	// Номер задачи: ПРО-142. Единственное имя карточки, которое можно
	// назвать вслух, написать в переписке и ввести в поиск. Выдаётся при
	// создании и не меняется никогда — на него ссылаются снаружи.
	Number      string `json:"number"`
	ColumnID    string `json:"columnId"`
	Position    string `json:"position"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
	// Отметки потока. ColumnEnteredAt — не время создания карточки:
	// из него считается старение в текущей колонке.
	ColumnEnteredAt time.Time  `json:"columnEnteredAt"`
	StartedAt       *time.Time `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
	// Оценка карточки в единицах организации. Пусто — не оценена.
	Estimate *float64 `json:"estimate"`
	// Исход работы: done или discarded. Пока работа идёт — пусто.
	// Пропускная способность считается только по done, иначе выброшенные
	// карточки завышают её и все прогнозы по ней.
	Outcome *string `json:"outcome"`
	// Приоритет: низкий, средний, высокий, наивысший. Уровень говорит,
	// что важнее; порядок карточек в колонке остаётся ручным и говорит,
	// что взято в работу следующим.
	Priority string `json:"priority"`
	// Дата обязательства: то, что обещано наружу. Пусто у большинства
	// карточек — и это не «дата неизвестна», а «обязательства нет».
	DueOn *string `json:"dueOn"`
	// Отметка «сделано»: готовность, объявленная руками и не зависящая
	// от колонки. Нужна разбиению — пункты вида «согласовать с юристами»
	// не гоняют по доске, — и входит только в прогресс родителя. Поток
	// считается по FinishedAt и Outcome, и отметка их не подменяет.
	DoneAt *time.Time `json:"doneAt"`
	// Ниже — вычисляемое, в таблице карточек этого нет.

	// Прогресс по подзадачам. Пусто, если подзадач нет: хранить процент
	// руками — значит завести поле, которое никто не обновляет.
	Progress *Progress `json:"progress,omitempty"`
	// Открытая блокировка, если есть.
	Blocked *Block `json:"blocked,omitempty"`
	// Сколько реплик в обсуждении. Считается запросом на доску, как
	// и прогресс: хранимый счётчик пришлось бы поддерживать при каждой
	// правке и удалении реплики. Нужен он не самой карточке, а строке
	// подзадачи на доске — у подзадачи своё обсуждение, и без числа
	// о нём узнают, только зайдя внутрь.
	Comments int `json:"comments"`
}

// Progress — доля завершённой работы среди подзадач. Считается запросом,
// а не хранится: иначе пришлось бы поддерживать счётчики в согласованном
// состоянии при каждом изменении любой подзадачи, в том числе на чужой
// доске.
//
// Считается двумя способами, и это видно снаружи. Если оценены все
// подзадачи — по сумме оценок; если хотя бы одна не оценена — по их
// количеству. Смешивать нельзя: неоценённая подзадача с весом ноль
// исчезла бы из знаменателя, и прогресс показывал бы больше сделанного,
// чем есть. Ошибаться тут можно только в сторону осторожности.
type Progress struct {
	Done  float64 `json:"done"`
	Total float64 `json:"total"`
	// ByWeight говорит, чем измерен прогресс: весом или штуками. Клиенту
	// это нужно, чтобы подписать «3 из 5» или «12 из 20 очков».
	ByWeight bool `json:"byWeight"`
}

// Block — интервал блокировки. Именно интервал, а не флаг: из булева поля
// не посчитать ни время разрешения блокировок, ни эффективность потока,
// и восстановить это задним числом неоткуда.
type Block struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	BlockedAt time.Time `json:"blockedAt"`
	// Работа, которая держит. Пусто — держит что-то вне доски, и об
	// этом сказано только словами причины. Ссылка, а не название:
	// карточку переименуют, а журнал блокировок обязан остаться
	// правдой о прошлом — и заодно по ссылке видно, что с ней стало.
	BlockingCard *string `json:"blockingCard,omitempty"`
}

// Link — связь между карточками.
type Link struct {
	FromCard string `json:"fromCard"`
	ToCard   string `json:"toCard"`
	Kind     string `json:"kind"`
}

// Виды связей. subtask — дерево (у карточки один родитель), blocks и
// relates — произвольный граф.
const (
	LinkSubtask = "subtask"
	LinkBlocks  = "blocks"
	LinkRelates = "relates"
)

// cardDone — готовность подзадачи глазами родителя: карточка прошла
// точку финиша или её отметили руками. Выражение одно на все места,
// где прогресс считается, — их три, и разъехаться им нельзя: доска
// и панель карточки показывали бы разную долю сделанного.
//
// Псевдоним `c` здесь не случайность, а требование к вызывающему:
// все три запроса читают подзадачу под этим именем.
const cardDone = `(c.outcome = 'done' or c.done_at is not null)`

// MaxSubtaskDepth ограничивает глубину дерева подзадач. Предел нужен не
// ради красоты: без него обход дерева не ограничен, а цена ошибки в связях
// растёт вместе с глубиной. Azure DevOps держит планку «меньше 14»,
// GitHub — 8; на нашем масштабе хватает пяти.
const MaxSubtaskDepth = 5

const cardFields = `id, number, column_id, position, title, description, version,
	column_entered_at, started_at, finished_at, estimate, outcome, priority,
	to_char(due_on, 'YYYY-MM-DD'), done_at`

func scanCard(row pgx.Row) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.Number, &c.ColumnID, &c.Position, &c.Title, &c.Description,
		&c.Version, &c.ColumnEnteredAt, &c.StartedAt, &c.FinishedAt,
		&c.Estimate, &c.Outcome, &c.Priority, &c.DueOn, &c.DoneAt)
	return c, err
}

// Уровни приоритета. Четыре: середина нужна, чтобы не заставлять
// выбирать сторону там, где выбирать нечего, а пятый и шестой уровни
// в работе не различают — живут «обычное», «важное» и «горит».
const (
	PriorityHighest = "highest"
	PriorityHigh    = "high"
	PriorityMedium  = "medium"
	PriorityLow     = "low"
)

func knownPriority(name string) bool {
	switch name {
	case PriorityHighest, PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

// Snapshot — полный слепок доски. На нашем масштабе доска отдаётся одним
// ответом: колонка в сотни карточек — организационная патология, а не
// сценарий, ради которого стоит усложнять клиент постраничной загрузкой.
type Snapshot struct {
	Board   Info     `json:"board"`
	Columns []Column `json:"columns"`
	Cards   []Card   `json:"cards"`
	// Связи карточек доски с любыми другими — в том числе с карточками
	// на досках других команд. Клиенту нужны обе стороны, чтобы показать
	// подзадачу, которую делает кто-то ещё.
	Links []Link `json:"links"`
	// Карточки с других досок, на которые ведут связи. Своих здесь нет —
	// они уже в Cards. Без этого связь остаётся голым идентификатором,
	// и «подзадача у соседней команды» показать нечем.
	//
	// Сюда попадает не всё, на что ссылаются связи: карточка на закрытой
	// или чужой доске не пройдёт политику и просто не появится. Это
	// правильный ответ — существование связи скрывать незачем, а вот
	// содержимое недоступной доски раскрывать нельзя.
	Linked []LinkedCard `json:"linked"`
	// Итерации доски и текущее вхождение карточек. Состав на прошлые
	// моменты живёт в интервалах и читается отдельным запросом — снимку
	// доски он не нужен, ему нужно «сейчас».
	Iterations []Iteration `json:"iterations"`
	// cardId → iterationId для открытых вхождений.
	CardIterations map[string]string `json:"cardIterations"`
	// Свои поля организации и их значения: cardId → значения.
	Fields      []Field                 `json:"fields"`
	FieldValues map[string][]FieldValue `json:"fieldValues"`
	// Метки организации и то, что чем помечено. Раздельно, а не списком
	// меток внутри каждой карточки: иначе название метки уезжало бы
	// в снимок столько раз, на скольких карточках оно висит.
	Labels []Label `json:"labels"`
	// cardId → labelId
	CardLabels map[string][]string `json:"cardLabels"`
	// Кто над чем работает: cardId → люди в порядке назначения.
	// Первый в списке — тот, кого назначили первым; порядок несёт
	// смысл и потому сохраняется.
	CardAssignees map[string][]string `json:"cardAssignees"`
	// Кого можно назначить и чьи имена показывать. Список маленький —
	// установка рассчитана на сотню человек, — и он нужен на каждой доске:
	// без него исполнитель на карточке остался бы идентификатором.
	People []Person `json:"people"`
}

// LinkedCard — то немногое, что нужно знать о карточке с чужой доски:
// где она лежит, чья это команда, что с ней происходит и когда её ждать.
type LinkedCard struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	BoardID   string  `json:"boardId"`
	BoardName string  `json:"boardName"`
	TeamName  *string `json:"teamName"`
	Outcome   *string `json:"outcome"`
	// Отмечена сделанной руками. Отдельно от Outcome: у соседей своя
	// доска и свои колонки, и «часть готова» они могут объявить, не
	// двигая карточку, — а прогресс здесь обязан считать это готовым,
	// иначе доля разбиения врёт заказчику.
	Done    bool `json:"done"`
	Blocked bool `json:"blocked"`
	// Где она у них стоит. Одного «сделана или нет» мало: «третью неделю
	// в очереди» и «делают со вчера» выглядели одинаково, а разница между
	// ними и есть весь смысл спрашивать про чужую работу.
	ColumnName string `json:"columnName"`
	ColumnKind string `json:"columnKind"`
	// Обещание доски исполнителя. Срок принадлежит доске, на которой
	// работа лежит: заказчик его видит и не двигает — иначе это была бы
	// дата, под которой никто не подписывался. Пусто — обещания нет.
	SLEDays        *int `json:"sleDays"`
	SLEProbability int  `json:"sleProbability"`
	// Убрана в архив. Отказ соседей взять работу — это архивация
	// карточки, и он обязан отличаться от «доски вам не видно»: искать
	// недоступную бесполезно, а после отказа идут договариваться.
	// Пока архивные карточки просто выпадали из ответа, отказ читался
	// как отсутствие доступа.
	Archived bool `json:"archived"`
}

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

// Все запросы идут через store.InTenant: база сама отсекает чужие строки
// политиками RLS, а условие по org_id в SQL остаётся как читаемое намерение,
// а не как единственная линия обороны.

// List возвращает доски организации.
func (s *Service) List(ctx context.Context, orgID, userID string) ([]Info, error) {
	out := []Info{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		// Право записи считается здесь же, одним вызовом на запрос:
		// функция stable, и планировщик выносит её в InitPlan, а не зовёт
		// на каждую доску.
		rows, err := tx.Query(ctx, `
			select `+boardFields+`, id = any (app_writable_boards()),
			       visibility, team_id,
			       (select count(*) from cards c
			         where c.board_id = boards.id and c.archived_at is null)
			  from boards
			 where archived_at is null
			 order by created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Info
			var writable bool
			var visibility string
			var teamID *string
			var cards int
			if err := rows.Scan(&b.ID, &b.Name, &b.Version, &b.SLEDays,
				&b.SLEProbability, &b.Key, &writable, &visibility, &teamID, &cards); err != nil {
				return err
			}
			b.Writable = &writable
			b.Visibility = &visibility
			b.TeamID = teamID
			b.Cards = &cards
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// Create заводит доску с тремя колонками потока по умолчанию.
//
// Пустой key означает «выведи из названия»: ключ нужен всегда, но
// придумывать его при создании доски человека заставлять незачем —
// в подавляющем большинстве случаев подойдёт выведенный.
func (s *Service) Create(ctx context.Context, orgID, userID, name, key string) (Info, error) {
	var b Info
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var projectID string
		err := tx.QueryRow(ctx, `
			select id from projects
			 where archived_at is null
			 order by created_at limit 1`).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx,
				`insert into projects (org_id, name) values ($1, $2) returning id`,
				orgID, "Проекты").Scan(&projectID)
		}
		if err != nil {
			return err
		}

		b, err = insertBoard(ctx, tx, orgID, projectID, name, key)
		if err != nil {
			return err
		}

		// Колонки по умолчанию сразу размечены: без точек старта и финиша
		// журнал переходов копится, а метрики потока по нему не считаются.
		defaults := []Column{
			{Name: "Очередь", Kind: KindQueue},
			{Name: "В работе", Kind: KindInProgress, IsStartedPoint: true},
			{Name: "Готово", Kind: KindDone, IsFinishedPoint: true},
		}
		positions, err := rank.NBetween("", "", len(defaults))
		if err != nil {
			return err
		}
		for i, column := range defaults {
			_, err = tx.Exec(ctx, `
				insert into board_columns
					(org_id, board_id, name, position, kind,
					 is_started_point, is_finished_point)
				values ($1, $2, $3, $4, $5, $6, $7)`,
				orgID, b.ID, column.Name, positions[i], column.Kind,
				column.IsStartedPoint, column.IsFinishedPoint)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return b, err
}

// Snapshot читает доску целиком вместе с её версией.
func (s *Service) Snapshot(ctx context.Context, orgID, userID, boardID string) (Snapshot, error) {
	var snap Snapshot
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		info, err := scanBoard(tx.QueryRow(ctx, `
			select `+boardFields+` from boards
			 where id = $1 and archived_at is null`, boardID))
		snap.Board = info
		if errors.Is(err, pgx.ErrNoRows) {
			// Доска чужой организации неотличима от несуществующей —
			// и это правильный ответ: подтверждать её существование
			// постороннему не нужно. А вот свою, но убранную в архив,
			// назвать по имени можно и нужно.
			var archived bool
			if err := tx.QueryRow(ctx,
				`select exists (select 1 from boards where id = $1)`, boardID).
				Scan(&archived); err != nil {
				return err
			}
			if archived {
				return ErrArchivedBoard
			}
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		colRows, err := tx.Query(ctx, `
			select `+columnFields+`
			  from board_columns
			 where board_id = $1 and archived_at is null
			 order by position`, boardID)
		if err != nil {
			return err
		}
		defer colRows.Close()
		snap.Columns = []Column{}
		for colRows.Next() {
			c, err := scanColumn(colRows)
			if err != nil {
				return err
			}
			snap.Columns = append(snap.Columns, c)
		}
		if err := colRows.Err(); err != nil {
			return err
		}
		colRows.Close()

		cardRows, err := tx.Query(ctx, `
			select `+cardFields+`
			  from cards
			 where board_id = $1 and archived_at is null
			 order by column_id, position`, boardID)
		if err != nil {
			return err
		}
		defer cardRows.Close()
		snap.Cards = []Card{}
		for cardRows.Next() {
			c, err := scanCard(cardRows)
			if err != nil {
				return err
			}
			snap.Cards = append(snap.Cards, c)
		}
		if err := cardRows.Err(); err != nil {
			return err
		}
		cardRows.Close()

		if err := enrich(ctx, tx, boardID, &snap); err != nil {
			return err
		}
		if err := loadIterations(ctx, tx, boardID, &snap); err != nil {
			return err
		}
		if err := loadPeople(ctx, tx, orgID, &snap); err != nil {
			return err
		}
		if err := loadAssignees(ctx, tx, boardID, &snap); err != nil {
			return err
		}
		if err := loadLabels(ctx, tx, boardID, &snap); err != nil {
			return err
		}
		return loadFields(ctx, tx, boardID, &snap)
	})
	return snap, err
}

// enrich дочитывает то, чего нет в самой карточке: прогресс по подзадачам,
// открытые блокировки и связи. Три коротких запроса на доску вместо запроса
// на карточку — на нашем масштабе это дешевле любого кэша.
func enrich(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	byID := make(map[string]*Card, len(snap.Cards))
	for i := range snap.Cards {
		byID[snap.Cards[i].ID] = &snap.Cards[i]
	}

	// Прогресс. Подзадачи намеренно не ограничены доской родителя: смысл
	// связи в том и состоит, что работу может делать другая команда.
	progressRows, err := tx.Query(ctx, `
		select l.from_card,
		       count(*)                                   as total,
		       count(*) filter (where `+cardDone+`)       as done,
		       count(*) filter (where c.estimate is null) as unestimated,
		       coalesce(sum(c.estimate), 0)               as weight,
		       coalesce(sum(c.estimate) filter (where `+cardDone+`), 0) as weight_done
		  from card_links l
		  join cards c on c.id = l.to_card and c.archived_at is null
		 where l.kind = 'subtask'
		   and l.from_card in (select id from cards
		                        where board_id = $1 and archived_at is null)
		 group by l.from_card`, boardID)
	if err != nil {
		return err
	}
	defer progressRows.Close()
	for progressRows.Next() {
		var id string
		var total, done, unestimated int
		var weight, weightDone float64
		if err := progressRows.Scan(&id, &total, &done, &unestimated,
			&weight, &weightDone); err != nil {
			return err
		}
		p := Progress{Done: float64(done), Total: float64(total)}
		// Вес берётся, только если оценены все подзадачи. Иначе
		// неоценённая работа молча исчезла бы из знаменателя.
		if unestimated == 0 && weight > 0 {
			p = Progress{Done: weightDone, Total: weight, ByWeight: true}
		}
		if card := byID[id]; card != nil {
			card.Progress = &p
		}
	}
	if err := progressRows.Err(); err != nil {
		return err
	}
	progressRows.Close()

	// Число реплик. Удалённые не считаются: обсуждение — это то, что
	// в нём осталось, а не то, что когда-то было написано.
	commentRows, err := tx.Query(ctx, `
		select card_id, count(*)
		  from card_comments
		 where board_id = $1 and deleted_at is null
		 group by card_id`, boardID)
	if err != nil {
		return err
	}
	defer commentRows.Close()
	for commentRows.Next() {
		var id string
		var count int
		if err := commentRows.Scan(&id, &count); err != nil {
			return err
		}
		if card := byID[id]; card != nil {
			card.Comments = count
		}
	}
	if err := commentRows.Err(); err != nil {
		return err
	}
	commentRows.Close()

	blockRows, err := tx.Query(ctx, `
		select b.card_id, b.id, b.reason, b.blocked_at, b.blocking_card
		  from card_blocks b
		  join cards c on c.id = b.card_id
		 where c.board_id = $1 and b.unblocked_at is null`, boardID)
	if err != nil {
		return err
	}
	defer blockRows.Close()
	for blockRows.Next() {
		var cardID string
		var b Block
		if err := blockRows.Scan(&cardID, &b.ID, &b.Reason, &b.BlockedAt, &b.BlockingCard); err != nil {
			return err
		}
		if card := byID[cardID]; card != nil {
			card.Blocked = &b
		}
	}
	if err := blockRows.Err(); err != nil {
		return err
	}
	blockRows.Close()

	linkRows, err := tx.Query(ctx, `
		select from_card, to_card, kind from card_links
		 where from_card in (select id from cards where board_id = $1)
		    or to_card   in (select id from cards where board_id = $1)`, boardID)
	if err != nil {
		return err
	}
	defer linkRows.Close()
	snap.Links = []Link{}
	for linkRows.Next() {
		var l Link
		if err := linkRows.Scan(&l.FromCard, &l.ToCard, &l.Kind); err != nil {
			return err
		}
		snap.Links = append(snap.Links, l)
	}
	if err := linkRows.Err(); err != nil {
		return err
	}

	// Карточки с других досок. Один запрос на всю доску, а не по одному
	// на связь: связей у доски десятки, а запросов должно остаться
	// столько же, сколько было.
	others := make([]string, 0, len(snap.Links)*2)
	for _, l := range snap.Links {
		others = append(others, l.FromCard, l.ToCard)
	}
	snap.Linked = []LinkedCard{}
	if len(others) == 0 {
		return nil
	}

	// Архивные отсюда не отсекаются намеренно: убранная карточка, выпав
	// из ответа, оставляла связь без второй стороны, и клиенту оставалась
	// одна ветка — «доска вам не видна». Отказ соседей показывался как
	// отсутствие доступа. Различает их признак, а не пропажа.
	foreignRows, err := tx.Query(ctx, `
		select c.id, c.title, c.board_id, b.name, t.name, c.outcome,
		       c.done_at is not null,
		       exists (select 1 from card_blocks cb
		                where cb.card_id = c.id and cb.unblocked_at is null),
		       col.name, col.kind, b.sle_days, b.sle_probability,
		       c.archived_at is not null
		  from cards c
		  join boards b on b.id = c.board_id
		  join board_columns col on col.id = c.column_id
		  left join teams t on t.id = b.team_id
		 where c.id = any ($1) and c.board_id <> $2`,
		others, boardID)
	if err != nil {
		return err
	}
	defer foreignRows.Close()
	for foreignRows.Next() {
		var c LinkedCard
		if err := foreignRows.Scan(&c.ID, &c.Title, &c.BoardID, &c.BoardName,
			&c.TeamName, &c.Outcome, &c.Done, &c.Blocked, &c.ColumnName, &c.ColumnKind,
			&c.SLEDays, &c.SLEProbability, &c.Archived); err != nil {
			return err
		}
		snap.Linked = append(snap.Linked, c)
	}
	if err := foreignRows.Err(); err != nil {
		return err
	}
	foreignRows.Close()

	return nil
}

// loadIterations читает итерации доски и текущие вхождения карточек.
//
// Отдельной функцией, а не хвостом enrich: там есть ранний выход для
// доски без связей, и итерации на такой доске просто не доходили бы
// до ответа. Тест это и поймал.
// loadPeople отдаёт тех, кого можно назначить. Список маленький —
// установка рассчитана на сотню человек, — и без него исполнитель
// на карточке остался бы идентификатором.
func loadPeople(ctx context.Context, tx pgx.Tx, orgID string, snap *Snapshot) error {
	snap.People = []Person{}
	// Ключи интеграций сюда не попадают, хотя состоят в организации
	// наравне с людьми: работу назначают человеку, а «назначить задачу
	// ключу» — это предложение, за которым ничего нет. По той же причине
	// их нет и в отборе по исполнителю.
	rows, err := tx.Query(ctx, `
		select u.id, u.name
		  from memberships m join users u on u.id = m.user_id
		 where m.org_id = $1 and u.kind = 'person'
		 order by u.name`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.UserID, &p.Name); err != nil {
			return err
		}
		snap.People = append(snap.People, p)
	}
	return rows.Err()
}

func loadIterations(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	iterRows, err := tx.Query(ctx, `
		select `+iterationFields+`,
		       (select count(*) from iteration_cards ic
		         where ic.iteration_id = i.id and ic.removed_at is null)
		  from iterations i
		 where i.board_id = $1
		 order by i.starts_on desc, i.created_at desc`, boardID)
	if err != nil {
		return err
	}
	defer iterRows.Close()
	snap.Iterations = []Iteration{}
	for iterRows.Next() {
		var it Iteration
		if err := iterRows.Scan(&it.ID, &it.Name, &it.Goal, &it.StartsOn,
			&it.EndsOn, &it.ClosedAt, &it.CardCount); err != nil {
			return err
		}
		snap.Iterations = append(snap.Iterations, it)
	}
	if err := iterRows.Err(); err != nil {
		return err
	}
	iterRows.Close()

	memberRows, err := tx.Query(ctx, `
		select ic.card_id, ic.iteration_id
		  from iteration_cards ic
		  join iterations i on i.id = ic.iteration_id
		 where i.board_id = $1 and ic.removed_at is null`, boardID)
	if err != nil {
		return err
	}
	defer memberRows.Close()
	snap.CardIterations = map[string]string{}
	for memberRows.Next() {
		var cardID, iterationID string
		if err := memberRows.Scan(&cardID, &iterationID); err != nil {
			return err
		}
		snap.CardIterations[cardID] = iterationID
	}
	return memberRows.Err()
}

// CardOrder — текущий порядок колонки, который возвращается клиенту
// вместе с ответом 409, чтобы он пересобрался без полной перезагрузки доски.
type CardOrder struct {
	ID       string `json:"id"`
	Position string `json:"position"`
}

// Placement — куда положить карточку в колонке.
//
// Клиент присылает намерение, а не вычисленную позицию: если опорная карточка
// к моменту обработки сдвинулась, посчитанный клиентом ключ был бы неверным.
// Место указывается явно, без умолчаний «по отсутствию поля» — из-за них
// одна и та же запись означала бы «в начало» в одной операции и «в конец»
// в другой.
type Placement struct {
	// Place: start — в начало колонки, end — в конец, after — сразу
	// за карточкой AfterCardID. Пустое значение читается как end.
	Place       string  `json:"place"`
	AfterCardID *string `json:"afterCardId"`
}

const (
	PlaceStart = "start"
	PlaceEnd   = "end"
	PlaceAfter = "after"
)

// neighbours находит соседей будущей позиции карточки.
//
// exclude пуст при создании карточки (исключать нечего) и содержит id при
// перемещении: соседа ищем среди остальных карточек, иначе перемещаемая
// карточка окажется сама себе границей.
func neighbours(ctx context.Context, tx pgx.Tx, columnID string, pl Placement, exclude string) (prev, next string, err error) {
	var excludeID *string
	if exclude != "" {
		excludeID = &exclude
	}

	place := pl.Place
	if place == "" {
		place = PlaceEnd
	}

	switch place {
	case PlaceEnd:
		err = tx.QueryRow(ctx, `
			select position from cards
			 where column_id = $1 and archived_at is null
			   and ($2::uuid is null or id <> $2::uuid)
			 order by position desc limit 1`, columnID, excludeID).Scan(&prev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
		return prev, "", nil

	case PlaceStart:
		err = tx.QueryRow(ctx, `
			select position from cards
			 where column_id = $1 and archived_at is null
			   and ($2::uuid is null or id <> $2::uuid)
			 order by position limit 1`, columnID, excludeID).Scan(&next)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
		return "", next, nil

	case PlaceAfter:
		if pl.AfterCardID == nil {
			return "", "", badRequestf("для place = %q нужен afterCardId", PlaceAfter)
		}
		err = tx.QueryRow(ctx, `
			select position from cards
			 where id = $1 and column_id = $2 and archived_at is null`,
			*pl.AfterCardID, columnID).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", conflictf(columnID, "карточка, после которой вставляем, уже перемещена или удалена")
		}
		if err != nil {
			return "", "", err
		}
		err = tx.QueryRow(ctx, `
			select position from cards
			 where column_id = $1 and archived_at is null
			   and ($2::uuid is null or id <> $2::uuid)
			   and position > $3
			 order by position limit 1`, columnID, excludeID, prev).Scan(&next)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
		return prev, next, nil

	default:
		return "", "", badRequestf("недопустимое значение place: %q, ожидалось start, end или after", pl.Place)
	}
}

func nextPosition(ctx context.Context, tx pgx.Tx, columnID string, pl Placement, exclude string) (string, error) {
	prev, next, err := neighbours(ctx, tx, columnID, pl, exclude)
	if err != nil {
		return "", err
	}
	pos, err := rank.Between(prev, next)
	if err != nil {
		return "", fmt.Errorf("вычисление позиции: %w", err)
	}
	return pos, nil
}
