package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/konkov/agile/internal/rank"
)

// Request — операция, присланная клиентом.
//
// Клиент отправляет намерение («переместить карточку после такой-то»),
// а не готовое состояние. OperationID стабилен между повторами: если ответ
// потерялся в сети, повтор с тем же идентификатором вернёт сохранённый
// результат, а не создаст вторую карточку.
type Request struct {
	OperationID string          `json:"operationId"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
}

// Patch — что изменилось. Клиент применяет патч к своей копии доски,
// не перезагружая её целиком.
type Patch struct {
	Cards          []Card   `json:"cards,omitempty"`
	Columns        []Column `json:"columns,omitempty"`
	RemovedCardIDs []string `json:"removedCardIds,omitempty"`
}

type Result struct {
	Version int64 `json:"version"`
	Patch   Patch `json:"patch"`
}

// ConflictError означает, что операция опиралась на устаревшее представление
// доски. Ответ несёт текущий порядок затронутой колонки, чтобы клиент
// пересобрался точечно — полная перезагрузка доски здесь не нужна.
type ConflictError struct {
	Message  string      `json:"error"`
	ColumnID string      `json:"columnId,omitempty"`
	Order    []CardOrder `json:"currentOrder,omitempty"`
	Version  int64       `json:"version"`
}

func (e *ConflictError) Error() string { return e.Message }

func conflictf(columnID, format string, args ...any) error {
	return &ConflictError{ColumnID: columnID, Message: fmt.Sprintf(format, args...)}
}

// ErrBadRequest — операция сформулирована неверно (в отличие от конфликта,
// повтор не поможет).
var ErrBadRequest = errors.New("некорректная операция")

func badRequestf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, fmt.Sprintf(format, args...))
}

// Apply выполняет операцию над доской.
//
// Все операции по одной доске сериализуются блокировкой её строки. На потоке
// в единицы операций в секунду это бесплатно и снимает целый класс гонок:
// вычисление позиции и запись всегда видят одно и то же состояние колонки.
func (s *Service) Apply(ctx context.Context, orgID, actorID, boardID string, req Request) (Result, error) {
	if req.OperationID == "" {
		return Result{}, badRequestf("не передан operationId")
	}

	tx, err := s.db.BeginTenant(ctx, orgID, actorID)
	if err != nil {
		return Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Доска берётся первой, и порядок здесь существенный. Журнал операций
	// под политикой видимости: запись в него для недоступной доски отлетает
	// нарушением RLS — то есть внутренней ошибкой вместо честного «не
	// найдена». Блокировка строки заодно сериализует и одновременный повтор
	// той же операции: второй запрос дождётся и увидит уже занятый
	// идентификатор.
	var version int64
	err = tx.QueryRow(ctx, `
		select version from boards
		 where id = $1 and archived_at is null
		 for update`, boardID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		// Чужая доска неотличима от несуществующей — как и в Snapshot.
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}

	// Резервируем идентификатор операции. Ноль затронутых строк означает,
	// что эта операция уже выполнена — возвращаем сохранённый результат.
	tag, err := tx.Exec(ctx, `
		insert into operations (operation_id, org_id, actor_id, board_id, kind, result)
		values ($1, $2, $3, $4, $5, '{}'::jsonb)
		on conflict (operation_id) do nothing`,
		req.OperationID, orgID, actorID, boardID, req.Type)
	if err != nil {
		return Result{}, fmt.Errorf("резервирование операции: %w", err)
	}
	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		return s.storedResult(ctx, orgID, actorID, req.OperationID)
	}

	patch, err := s.dispatch(ctx, tx, orgID, actorID, boardID, req)
	if err != nil {
		// Нечитаемый идентификатор — ошибка клиента, а не сбой сервера.
		// Ловим здесь, чтобы не проверять формат uuid в каждой операции.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return Result{}, badRequestf("некорректный идентификатор в операции %s", req.Type)
		}
		var conflict *ConflictError
		if errors.As(err, &conflict) {
			conflict.Version = version
			if conflict.ColumnID != "" && conflict.Order == nil {
				// читаем текущий порядок отдельным запросом: наша транзакция
				// вот-вот откатится, а клиенту нужны актуальные данные
				if order, oErr := s.columnOrderOutside(ctx, orgID, actorID, conflict.ColumnID); oErr == nil {
					conflict.Order = order
				}
			}
		}
		return Result{}, err
	}

	err = tx.QueryRow(ctx,
		`update boards set version = version + 1 where id = $1 returning version`,
		boardID).Scan(&version)
	if err != nil {
		return Result{}, err
	}

	result := Result{Version: version, Patch: patch}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx,
		`update operations set result = $2::jsonb where operation_id = $1`,
		req.OperationID, string(encoded)); err != nil {
		return Result{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	committed = true
	return result, nil
}

func (s *Service) storedResult(ctx context.Context, orgID, actorID, operationID string) (Result, error) {
	var raw []byte
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`select result from operations where operation_id = $1`, operationID).Scan(&raw)
	})
	if err != nil {
		return Result{}, fmt.Errorf("чтение результата повторённой операции: %w", err)
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

func (s *Service) columnOrderOutside(ctx context.Context, orgID, actorID, columnID string) ([]CardOrder, error) {
	out := []CardOrder{}
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, position from cards
			 where column_id = $1 and archived_at is null
			 order by position`, columnID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c CardOrder
			if err := rows.Scan(&c.ID, &c.Position); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) dispatch(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, req Request) (Patch, error) {
	switch req.Type {
	case "CREATE_CARD":
		return createCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "MOVE_CARD":
		return moveCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "UPDATE_CARD":
		return updateCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "ARCHIVE_CARD":
		return archiveCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "CREATE_COLUMN":
		return createColumn(ctx, tx, orgID, boardID, req.Payload)
	case "RENAME_COLUMN":
		return renameColumn(ctx, tx, orgID, boardID, req.Payload)
	case "UPDATE_COLUMN":
		return updateColumn(ctx, tx, orgID, boardID, req.Payload)
	case "LINK_CARDS":
		return linkCards(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "UNLINK_CARDS":
		return unlinkCards(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "BLOCK_CARD":
		return blockCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "UNBLOCK_CARD":
		return unblockCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	default:
		return Patch{}, badRequestf("неизвестный тип операции %q", req.Type)
	}
}

// --- операции над карточками ---

type createCardPayload struct {
	ColumnID  string `json:"columnId"`
	Title     string `json:"title"`
	Placement        // place + afterCardId
}

func createCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p createCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор CREATE_CARD: %v", err)
	}
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return Patch{}, badRequestf("у карточки должно быть название")
	}
	col, err := loadColumn(ctx, tx, boardID, p.ColumnID)
	if err != nil {
		return Patch{}, err
	}
	if err := enforceWIP(ctx, tx, col); err != nil {
		return Patch{}, err
	}

	pos, err := nextPosition(ctx, tx, p.ColumnID, p.Placement, "")
	if err != nil {
		return Patch{}, err
	}

	// Карточка, заведённая сразу в стадии работы, считается начатой в этот
	// же момент: иначе её время цикла окажется меньше реального.
	c, err := scanCard(tx.QueryRow(ctx, `
		insert into cards (org_id, board_id, column_id, position, title,
		                   started_at, finished_at)
		values ($1, $2, $3, $4, $5,
		        case when $6::bool then now() end,
		        case when $7::bool then now() end)
		returning `+cardFields,
		orgID, boardID, p.ColumnID, pos, p.Title,
		col.IsStartedPoint, col.IsFinishedPoint))
	if err != nil {
		return Patch{}, err
	}

	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, "created", nil, &p.ColumnID,
		map[string]any{"to": columnFact(col)}); err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}

type moveCardPayload struct {
	CardID     string `json:"cardId"`
	ToColumnID string `json:"toColumnId"`
	Placement         // place + afterCardId
}

func moveCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p moveCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор MOVE_CARD: %v", err)
	}
	to, err := loadColumn(ctx, tx, boardID, p.ToColumnID)
	if err != nil {
		return Patch{}, err
	}

	var fromColumn string
	err = tx.QueryRow(ctx, `
		select column_id from cards
		 where id = $1 and board_id = $2 and archived_at is null`,
		p.CardID, boardID).Scan(&fromColumn)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf(p.ToColumnID, "карточка уже удалена или перенесена на другую доску")
	}
	if err != nil {
		return Patch{}, err
	}
	from, err := loadColumn(ctx, tx, boardID, fromColumn)
	if err != nil {
		return Patch{}, err
	}

	// Лимит проверяется только на входе в колонку. Перестановка внутри неё
	// не добавляет работы и не должна упираться в лимит — тем более что
	// колонка могла переполниться раньше, пока лимит был мягким.
	if fromColumn != p.ToColumnID {
		if err := enforceWIP(ctx, tx, to); err != nil {
			return Patch{}, err
		}
	}

	pos, err := nextPosition(ctx, tx, p.ToColumnID, p.Placement, p.CardID)
	if err != nil {
		return Patch{}, err
	}

	// Отметки потока обновляются здесь же, одним запросом:
	//   вход в колонку — только при смене колонки, иначе перестановка внутри
	//     колонки обнуляла бы возраст карточки;
	//   начало работы — один раз и навсегда;
	//   завершение — снимается, если карточку забрали обратно в работу.
	c, err := scanCard(tx.QueryRow(ctx, `
		update cards
		   set column_id = $2,
		       position  = $3,
		       column_entered_at = case when column_id is distinct from $2
		                                then now() else column_entered_at end,
		       started_at  = case when $4::bool and started_at is null
		                          then now() else started_at end,
		       finished_at = case when $5::bool
		                          then coalesce(finished_at, now()) else null end,
		       -- Исход: пересечение точки финиша объявляет работу сделанной,
		       -- возврат в работу снимает это объявление. Отказ от карточки
		       -- ('discarded') отсюда не ставится — это отдельное намерение.
		       outcome     = case when $5::bool then 'done'
		                          when outcome = 'done' then null
		                          else outcome end,
		       version    = version + 1,
		       updated_at = now()
		 where id = $1
		 returning `+cardFields,
		p.CardID, p.ToColumnID, pos, to.IsStartedPoint, to.IsFinishedPoint))
	if err != nil {
		return Patch{}, err
	}

	// Событие пишется на каждое перемещение, даже внутри одной колонки:
	// именно из этого журнала потом считаются cycle time и диаграмма потока.
	// Имя и вид колонок кладутся снимком — переименование колонки не должно
	// задним числом переписывать историю.
	// Смысл перехода кладётся снимком вместе с ним: `kind` и точки потока —
	// изменяемые поля, и переразметка доски иначе задним числом переписала бы
	// весь исторический цикл.
	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, "moved", &fromColumn, &p.ToColumnID,
		map[string]any{
			"from":          columnFact(from),
			"to":            columnFact(to),
			"crossedStart":  to.IsStartedPoint && !from.IsStartedPoint,
			"crossedFinish": to.IsFinishedPoint && !from.IsFinishedPoint,
		}); err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}

type updateCardPayload struct {
	CardID      string  `json:"cardId"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

func updateCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p updateCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор UPDATE_CARD: %v", err)
	}
	if p.Title == nil && p.Description == nil {
		return Patch{}, badRequestf("нечего изменять")
	}
	if p.Title != nil {
		trimmed := strings.TrimSpace(*p.Title)
		if trimmed == "" {
			return Patch{}, badRequestf("у карточки должно быть название")
		}
		p.Title = &trimmed
	}

	c, err := scanCard(tx.QueryRow(ctx, `
		update cards
		   set title       = coalesce($2, title),
		       description = coalesce($3, description),
		       version     = version + 1,
		       updated_at  = now()
		 where id = $1 and board_id = $4 and archived_at is null
		 returning `+cardFields,
		p.CardID, p.Title, p.Description, boardID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}

	kind := "described"
	if p.Title != nil {
		kind = "renamed"
	}
	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, kind, nil, nil, nil); err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}

type archiveCardPayload struct {
	CardID string `json:"cardId"`
}

func archiveCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p archiveCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор ARCHIVE_CARD: %v", err)
	}

	// Мягкое удаление, а не DELETE: журнал событий должен продолжать
	// ссылаться на карточку, иначе история потока рассыплется.
	// Исход проставляется здесь же: карточка, убранная с доски незавершённой,
	// — это отказ от работы, а не сделанная работа. Без этого различия
	// пропускная способность считает выброшенное за сделанное.
	var id, columnID string
	err := tx.QueryRow(ctx, `
		update cards
		   set archived_at = now(),
		       outcome     = coalesce(outcome, 'discarded'),
		       version     = version + 1
		 where id = $1 and board_id = $2 and archived_at is null
		 returning id, column_id`, p.CardID, boardID).Scan(&id, &columnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}

	col, err := loadColumn(ctx, tx, boardID, columnID)
	if err != nil {
		return Patch{}, err
	}
	if err := logEvent(ctx, tx, orgID, boardID, id, actorID, "archived", &columnID, nil,
		map[string]any{"from": columnFact(col)}); err != nil {
		return Patch{}, err
	}
	return Patch{RemovedCardIDs: []string{id}}, nil
}

// --- операции над колонками ---

type createColumnPayload struct {
	Name          string  `json:"name"`
	AfterColumnID *string `json:"afterColumnId"`
}

func createColumn(ctx context.Context, tx pgx.Tx, orgID, boardID string, raw json.RawMessage) (Patch, error) {
	var p createColumnPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор CREATE_COLUMN: %v", err)
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Patch{}, badRequestf("у колонки должно быть название")
	}

	prev := ""
	if p.AfterColumnID != nil {
		err := tx.QueryRow(ctx, `
			select position from board_columns
			 where id = $1 and board_id = $2 and archived_at is null`,
			*p.AfterColumnID, boardID).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, conflictf("", "колонка, после которой вставляем, уже удалена")
		}
		if err != nil {
			return Patch{}, err
		}
	}
	var next string
	err := tx.QueryRow(ctx, `
		select position from board_columns
		 where board_id = $1 and archived_at is null and ($2 = '' or position > $2)
		 order by position limit 1`, boardID, prev).Scan(&next)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, err
	}

	pos, err := rank.Between(prev, next)
	if err != nil {
		return Patch{}, fmt.Errorf("вычисление позиции колонки: %w", err)
	}

	// Новая колонка — стадия работы без границ потока: объявить её началом
	// или концом можно потом, отдельным намерением.
	c, err := scanColumn(tx.QueryRow(ctx, `
		insert into board_columns (org_id, board_id, name, position, kind)
		values ($1, $2, $3, $4, $5)
		returning `+columnFields, orgID, boardID, p.Name, pos, KindInProgress))
	if err != nil {
		return Patch{}, err
	}
	return Patch{Columns: []Column{c}}, nil
}

type renameColumnPayload struct {
	ColumnID string `json:"columnId"`
	Name     string `json:"name"`
}

// renameColumn остаётся отдельной операцией: переименование — самое частое
// изменение колонки, и клиенту незачем присылать ради него всю форму.
func renameColumn(ctx context.Context, tx pgx.Tx, _, boardID string, raw json.RawMessage) (Patch, error) {
	var p renameColumnPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор RENAME_COLUMN: %v", err)
	}
	return applyColumnUpdate(ctx, tx, boardID, updateColumnPayload{
		ColumnID: p.ColumnID,
		Name:     &p.Name,
	})
}

// optionalInt различает «поле не прислали» и «прислали null». Для лимита
// это разные намерения: не трогать и снять.
type optionalInt struct {
	Set   bool
	Value *int
}

func (o *optionalInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
}

type updateColumnPayload struct {
	ColumnID        string      `json:"columnId"`
	Name            *string     `json:"name"`
	Kind            *string     `json:"kind"`
	IsStartedPoint  *bool       `json:"isStartedPoint"`
	IsFinishedPoint *bool       `json:"isFinishedPoint"`
	Policy          *string     `json:"policy"`
	WIPLimit        optionalInt `json:"wipLimit"`
	WIPLimitHard    *bool       `json:"wipLimitHard"`
}

func updateColumn(ctx context.Context, tx pgx.Tx, _, boardID string, raw json.RawMessage) (Patch, error) {
	var p updateColumnPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор UPDATE_COLUMN: %v", err)
	}
	return applyColumnUpdate(ctx, tx, boardID, p)
}

func applyColumnUpdate(ctx context.Context, tx pgx.Tx, boardID string, p updateColumnPayload) (Patch, error) {
	if p.Name != nil {
		trimmed := strings.TrimSpace(*p.Name)
		if trimmed == "" {
			return Patch{}, badRequestf("у колонки должно быть название")
		}
		p.Name = &trimmed
	}
	if p.Kind != nil {
		switch *p.Kind {
		case KindQueue, KindInProgress, KindDone:
		default:
			return Patch{}, badRequestf("недопустимый вид колонки %q, ожидалось %s, %s или %s",
				*p.Kind, KindQueue, KindInProgress, KindDone)
		}
	}
	if p.WIPLimit.Set && p.WIPLimit.Value != nil && *p.WIPLimit.Value < 1 {
		return Patch{}, badRequestf("лимит колонки должен быть положительным; чтобы снять лимит, пришлите null")
	}

	c, err := scanColumn(tx.QueryRow(ctx, `
		update board_columns
		   set name              = coalesce($3, name),
		       kind              = coalesce($4, kind),
		       is_started_point  = coalesce($5, is_started_point),
		       is_finished_point = coalesce($6, is_finished_point),
		       policy            = coalesce($7, policy),
		       wip_limit         = case when $8::bool then $9::int else wip_limit end,
		       wip_limit_hard    = coalesce($10, wip_limit_hard)
		 where id = $1 and board_id = $2 and archived_at is null
		 returning `+columnFields,
		p.ColumnID, boardID, p.Name, p.Kind, p.IsStartedPoint, p.IsFinishedPoint,
		p.Policy, p.WIPLimit.Set, p.WIPLimit.Value, p.WIPLimitHard))
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "колонка уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}
	return Patch{Columns: []Column{c}}, nil
}

// --- вспомогательное ---

// loadColumn читает колонку вместе с её правилами. Отдельной проверки
// «существует ли колонка» больше нет: всякая операция, которой нужен этот
// ответ, тут же нуждается и в виде колонки, и в её лимите.
func loadColumn(ctx context.Context, tx pgx.Tx, boardID, columnID string) (Column, error) {
	if columnID == "" {
		return Column{}, badRequestf("не указана колонка")
	}
	c, err := scanColumn(tx.QueryRow(ctx, `
		select `+columnFields+`
		  from board_columns
		 where id = $1 and board_id = $2 and archived_at is null`, columnID, boardID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Column{}, conflictf("", "колонка не найдена на этой доске")
	}
	if err != nil {
		return Column{}, err
	}
	return c, nil
}

// enforceWIP отвечает конфликтом, если карточка не помещается в колонку
// с жёстким лимитом. Мягкий лимит здесь намеренно не делает ничего: он нужен,
// чтобы команда видела перегрузку, а не чтобы мешать ей работать. Так же
// устроены Jira, Azure DevOps и GitHub; жёсткий лимит — выбор колонки.
func enforceWIP(ctx context.Context, tx pgx.Tx, col Column) error {
	if col.WIPLimit == nil || !col.WIPLimitHard {
		return nil
	}
	var count int
	err := tx.QueryRow(ctx, `
		select count(*) from cards
		 where column_id = $1 and archived_at is null`, col.ID).Scan(&count)
	if err != nil {
		return err
	}
	if count >= *col.WIPLimit {
		return conflictf(col.ID,
			"в колонке «%s» жёсткий лимит %d — сначала освободите место",
			col.Name, *col.WIPLimit)
	}
	return nil
}

// columnFact — снимок колонки для журнала событий. Хранить только
// идентификатор недостаточно: колонку переименуют или заархивируют, и
// диаграмма потока за прошлые месяцы останется без подписей.
func columnFact(c Column) map[string]any {
	return map[string]any{"id": c.ID, "name": c.Name, "kind": c.Kind}
}

func logEvent(ctx context.Context, tx pgx.Tx, orgID, boardID, cardID, actorID, kind string, from, to *string, payload map[string]any) error {
	body := []byte("{}")
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
		insert into card_events (org_id, board_id, card_id, actor_id, type, from_column, to_column, payload)
		values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		orgID, boardID, cardID, actorID, kind, from, to, string(body))
	return err
}
