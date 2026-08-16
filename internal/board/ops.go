package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/konkov/agile/internal/rank"
	"github.com/konkov/agile/internal/realtime"
	"github.com/konkov/agile/internal/webhook"
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
	// Метки карточки целиком, а не «повесили такую-то»: список короткий,
	// а разница между «добавь» и «вот как теперь» — это разница между
	// патчем, который надо применять по порядку, и патчем, который можно
	// применить дважды без вреда.
	CardLabels map[string][]string `json:"cardLabels,omitempty"`
	// Исполнители карточки — тоже списком целиком и по той же причине.
	CardAssignees map[string][]string `json:"cardAssignees,omitempty"`
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

	// Доски берутся первыми, и порядок здесь существенный. Журнал операций
	// под политикой видимости: запись в него для недоступной доски отлетает
	// нарушением RLS — то есть внутренней ошибкой вместо честного «не
	// найдена». Блокировка строки заодно сериализует и одновременный повтор
	// той же операции: второй запрос дождётся и увидит уже занятый
	// идентификатор.
	//
	// Досок может быть две: подзадача заводится на доске соседей. Тогда
	// замки берутся в порядке идентификаторов, а не в порядке «своя, потом
	// чужая». Иначе две встречные постановки — я ставлю им, они мне —
	// заперли бы одну и ту же пару в противоположном порядке, и одна из
	// них умерла бы взаимной блокировкой.
	boards := []string{boardID}
	if second := targetBoard(req); second != "" && second != boardID {
		boards = append(boards, second)
	}
	slices.Sort(boards)

	versions := make(map[string]int64, len(boards))
	for _, id := range boards {
		var v int64
		err = tx.QueryRow(ctx, `
			select version from boards
			 where id = $1 and archived_at is null
			 for update`, id).Scan(&v)
		if errors.Is(err, pgx.ErrNoRows) {
			// Строки нет по одной из двух причин, и различить их важно.
			// `for update` требует права на изменение, поэтому доска, доступная
			// только на чтение, сюда тоже не попадает — а отвечать «не найдена»
			// тому, кто эту доску прямо сейчас видит, значит отправить его
			// искать несуществующую поломку.
			//
			// Второй запрос делается только на этом пути: платить за него
			// в обычном случае незачем.
			return Result{}, s.explainMissingBoard(ctx, orgID, actorID, id)
		}
		if err != nil {
			return Result{}, err
		}
		versions[id] = v
	}
	version := versions[boardID]

	// Резервируем идентификатор операции. Ноль затронутых строк означает,
	// что эта операция уже выполнена — возвращаем сохранённый результат.
	tag, err := tx.Exec(ctx, `
		insert into operations (operation_id, org_id, actor_id, board_id, kind, result)
		values ($1, $2, $3, $4, $5, '{}'::jsonb)
		on conflict (org_id, operation_id) do nothing`,
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

	// Версию поднимают обе доски: на доске соседей появилась карточка,
	// и если её версия не сдвинется, работа приедет к ним молча — увидят
	// при следующей перезагрузке и не поймут, откуда взялась.
	for _, id := range boards {
		var v int64
		if err := tx.QueryRow(ctx,
			`update boards set version = version + 1 where id = $1 returning version`,
			id).Scan(&v); err != nil {
			return Result{}, err
		}
		versions[id] = v
	}
	version = versions[boardID]

	result := Result{Version: version, Patch: patch}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	// Версия кладётся отдельной колонкой, а не только внутрь результата:
	// по ней клиент догоняет пропущенное, не перечитывая доску целиком.
	if _, err := tx.Exec(ctx, `
		update operations set result = $2::jsonb, board_version = $3
		 where operation_id = $1`,
		req.OperationID, string(encoded), version); err != nil {
		return Result{}, err
	}

	// Оповещение уходит тем же коммитом, что и само изменение: отправленное
	// отдельно, оно рано или поздно уйдёт без изменения либо изменение
	// пройдёт без оповещения.
	//
	// Доске соседей оповещение уходит тоже, а операция записана на доску
	// операции — значит догнать патчем они не смогут и перечитают снимок.
	// Это уже предусмотренный путь: пропуск в версиях означает «патчами
	// не догнать», и молча отдать неполный список было бы хуже.
	for _, id := range boards {
		if err := realtime.Notify(ctx, tx, realtime.Change{
			BoardID: id, Version: versions[id], ActorID: actorID,
		}); err != nil {
			return Result{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	committed = true
	return result, nil
}

// targetBoard называет вторую доску, которую заденет операция, — пустую
// строку, если операция обходится одной.
//
// Читается из полезной нагрузки до разбора операции, потому что замки
// нужно взять раньше, чем что-либо будет прочитано или записано. Разбор
// здесь нестрогий намеренно: если нагрузка нечитаема, честную ошибку
// об этом выдаст сама операция, а не проверка порядка замков.
func targetBoard(req Request) string {
	if req.Type != "CREATE_SUBTASK" {
		return ""
	}
	var p struct {
		BoardID string `json:"boardId"`
	}
	_ = json.Unmarshal(req.Payload, &p)
	return p.BoardID
}

// Changes отдаёт то, что случилось с доской после названной версии.
//
// Клиент, узнав из потока «доска доехала до версии N», раньше запрашивал
// весь снимок. Здесь он получает только патчи — те самые, которые уже
// применял к себе, отвечая на собственные операции.
//
// Догнать удаётся не всегда: операции старше колонки board_version
// версии не имеют, а очень старые могут быть убраны. Тогда честный
// ответ — «догнать нельзя», и клиент перечитывает снимок. Молча отдать
// неполный список хуже: расхождение всплывёт позже и необъяснимо.
func (s *Service) Changes(ctx context.Context, orgID, userID, boardID string, since int64) (Catchup, error) {
	out := Catchup{Results: []Result{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			select version from boards
			 where id = $1 and archived_at is null`, boardID).Scan(&out.Version); err != nil {
			return err
		}
		if since >= out.Version {
			return nil
		}

		rows, err := tx.Query(ctx, `
			select result, board_version from operations
			 where board_id = $1 and board_version > $2
			 order by board_version`, boardID, since)
		if err != nil {
			return err
		}
		defer rows.Close()

		var last int64 = since
		for rows.Next() {
			var raw []byte
			var version int64
			if err := rows.Scan(&raw, &version); err != nil {
				return err
			}
			var result Result
			if err := json.Unmarshal(raw, &result); err != nil {
				return err
			}
			// Пропуск в версиях означает, что между ними было изменение,
			// патча от которого у нас нет.
			if version != last+1 {
				out.Full = true
				out.Results = nil
				return nil
			}
			last = version
			// Версия отдаётся вместе с патчем, а не выводится клиентом
			// из порядка: считать её на той стороне значит хранить
			// в двух местах одно и то же и однажды разойтись.
			result.Version = version
			out.Results = append(out.Results, result)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if last != out.Version {
			out.Full = true
			out.Results = nil
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Catchup{}, ErrNotFound
	}
	return out, err
}

// Catchup — чем догнать пропущенное. Full означает «патчами не догнать,
// перечитай снимок».
type Catchup struct {
	Version int64    `json:"version"`
	Results []Result `json:"results"`
	Full    bool     `json:"full"`
}

// explainMissingBoard отвечает на вопрос, почему доска не досталась
// на запись: её не видно вовсе или её видно, но менять нельзя.
func (s *Service) explainMissingBoard(ctx context.Context, orgID, actorID, boardID string) error {
	var visible bool
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select exists (select 1 from boards
			                where id = $1 and archived_at is null)`, boardID).Scan(&visible)
	})
	if err != nil {
		return err
	}
	if visible {
		return ErrReadOnlyBoard
	}
	return ErrNotFound
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
	case "RESTORE_CARD":
		return restoreCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "ASSIGN_CARD":
		return assignCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "UNASSIGN_CARD":
		return unassignCard(ctx, tx, orgID, boardID, req.Payload)
	case "LABEL_CARD":
		return labelCard(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "UNLABEL_CARD":
		return unlabelCard(ctx, tx, orgID, boardID, req.Payload)
	case "CREATE_COLUMN":
		return createColumn(ctx, tx, orgID, boardID, req.Payload)
	case "RENAME_COLUMN":
		return renameColumn(ctx, tx, orgID, boardID, req.Payload)
	case "UPDATE_COLUMN":
		return updateColumn(ctx, tx, orgID, boardID, req.Payload)
	case "CREATE_SUBTASK":
		return createSubtask(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "LINK_CARDS":
		return linkCards(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "UNLINK_CARDS":
		return unlinkCards(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "SET_CARD_FIELD":
		return setCardField(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "ADD_TO_ITERATION":
		return addToIteration(ctx, tx, orgID, actorID, boardID, req.Payload)
	case "REMOVE_FROM_ITERATION":
		return removeFromIteration(ctx, tx, orgID, actorID, boardID, req.Payload)
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

	c, err := insertCard(ctx, tx, orgID, actorID, boardID, col, p.Title, p.Placement)
	if err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}

// insertCard заводит карточку в уже проверенной колонке. Отдельно от
// createCard потому, что карточку заводит не только сама операция
// создания: подзадача создаётся и связывается одной транзакцией.
func insertCard(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string,
	col Column, title string, placement Placement,
) (Card, error) {
	pos, err := nextPosition(ctx, tx, col.ID, placement, "")
	if err != nil {
		return Card{}, err
	}

	// Номер выдаёт счётчик доски, а не count(*) по её карточкам:
	// посчитанный номер переиспользовался бы после архивации последней
	// карточки, и два разных дела получили бы одно имя. Выданное
	// не возвращается — счётчик только растёт.
	var number string
	err = tx.QueryRow(ctx, `
		update boards set card_seq = card_seq + 1
		 where id = $1
		returning key || '-' || card_seq`, boardID).Scan(&number)
	if err != nil {
		return Card{}, err
	}

	// Карточка, заведённая сразу в стадии работы, считается начатой в этот
	// же момент: иначе её время цикла окажется меньше реального.
	c, err := scanCard(tx.QueryRow(ctx, `
		insert into cards (org_id, board_id, number, column_id, position, title,
		                   started_at, finished_at)
		values ($1, $2, $3, $4, $5, $6,
		        case when $7::bool then now() end,
		        case when $8::bool then now() end)
		returning `+cardFields,
		orgID, boardID, number, col.ID, pos, title,
		col.IsStartedPoint, col.IsFinishedPoint))
	if err != nil {
		return Card{}, err
	}

	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, "created", nil, &col.ID,
		map[string]any{"to": columnFact(col)}); err != nil {
		return Card{}, err
	}
	return c, nil
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
	// Оценка различает «не трогать» и «снять»: у карточки без оценки
	// и у карточки, оценку которой не присылали, разная судьба.
	Estimate optionalFloat `json:"estimate"`
}

func updateCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p updateCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор UPDATE_CARD: %v", err)
	}
	if p.Title == nil && p.Description == nil && !p.Estimate.Set {
		return Patch{}, badRequestf("нечего изменять")
	}
	if p.Estimate.Set && p.Estimate.Value != nil && *p.Estimate.Value <= 0 {
		return Patch{}, badRequestf("оценка должна быть больше нуля")
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
		       estimate    = case when $5 then $6 else estimate end,
		       version     = version + 1,
		       updated_at  = now()
		 where id = $1 and board_id = $4 and archived_at is null
		 returning `+cardFields,
		p.CardID, p.Title, p.Description, boardID,
		p.Estimate.Set, p.Estimate.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}

	// Событие называет главное из изменённого. Оценка важнее описания:
	// по ней считается прогресс и прогноз, и её изменение — то, о чём
	// потом спрашивают.
	kind := "described"
	payload := map[string]any(nil)
	switch {
	case p.Title != nil:
		kind = "renamed"
		payload = map[string]any{"title": *p.Title}
	case p.Estimate.Set:
		kind = "estimated"
		payload = map[string]any{"estimate": p.Estimate.Value}
	}
	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, kind, nil, nil, payload); err != nil {
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

// restoreCard возвращает карточку на доску.
//
// Нужна ради отмены: убрать карточку — обычное действие, и спрашивать
// перед ним «вы уверены?» значит ставить вопрос там, где в девяти
// случаях из десяти ответ известен. Дешевле разрешить вернуть.
//
// Исход снимается вместе с архивацией: карточка, вернувшаяся на доску,
// не является ни сделанной, ни выброшенной — работа над ней продолжается.
// Оставить «discarded» значило бы испортить пропускную способность
// молча.
func restoreCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p archiveCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор RESTORE_CARD: %v", err)
	}

	var columnID string
	err := tx.QueryRow(ctx, `
		update cards
		   set archived_at = null,
		       outcome     = case when outcome = 'discarded' then null else outcome end,
		       version     = version + 1
		 where id = $1 and board_id = $2 and archived_at is not null
		 returning column_id`, p.CardID, boardID).Scan(&columnID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Карточки нет или она и так на доске: повтор отмены безобиден.
		return Patch{}, conflictf("", "карточка уже на доске")
	}
	if err != nil {
		return Patch{}, err
	}

	card, err := scanCard(tx.QueryRow(ctx,
		`select `+cardFields+` from cards where id = $1`, p.CardID))
	if err != nil {
		return Patch{}, err
	}
	col, err := loadColumn(ctx, tx, boardID, columnID)
	if err != nil {
		return Patch{}, err
	}
	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "restored", nil, &columnID,
		map[string]any{"to": columnFact(col)}); err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{card}}, nil
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

// optionalFloat различает «поле не прислали» и «прислали null» — как
// и optionalInt, но для дробных значений.
type optionalFloat struct {
	Set   bool
	Value *float64
}

func (o *optionalFloat) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
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
	if _, err := tx.Exec(ctx, `
		insert into card_events (org_id, board_id, card_id, actor_id, type, from_column, to_column, payload)
		values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		orgID, boardID, cardID, actorID, kind, from, to, string(body)); err != nil {
		return err
	}

	// Подписчики узнают о событии из той же транзакции, в которой оно
	// записано: доставка не может оказаться потерянной при записанном
	// событии, а событие — записанным без доставки. Отправит её потом
	// работник, поэтому чужой медленный сервер не задержит операцию.
	err := webhook.Enqueue(ctx, tx, orgID, "card."+kind, map[string]any{
		"event":   "card." + kind,
		"boardId": boardID,
		"cardId":  cardID,
		"actorId": actorID,
		"payload": json.RawMessage(body),
		"at":      time.Now().UTC(),
	})
	return err
}
