package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

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
		return s.storedResult(ctx, req.OperationID)
	}

	var version int64
	err = tx.QueryRow(ctx, `
		select version from boards
		 where id = $1 and org_id = $2 and archived_at is null
		 for update`, boardID, orgID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}

	patch, err := s.dispatch(ctx, tx, orgID, actorID, boardID, req)
	if err != nil {
		var conflict *ConflictError
		if errors.As(err, &conflict) {
			conflict.Version = version
			if conflict.ColumnID != "" && conflict.Order == nil {
				// читаем текущий порядок отдельным запросом: наша транзакция
				// вот-вот откатится, а клиенту нужны актуальные данные
				if order, oErr := s.columnOrderOutside(ctx, conflict.ColumnID); oErr == nil {
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

func (s *Service) storedResult(ctx context.Context, operationID string) (Result, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`select result from operations where operation_id = $1`, operationID).Scan(&raw)
	if err != nil {
		return Result{}, fmt.Errorf("чтение результата повторённой операции: %w", err)
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

func (s *Service) columnOrderOutside(ctx context.Context, columnID string) ([]CardOrder, error) {
	rows, err := s.pool.Query(ctx, `
		select id, position from cards
		 where column_id = $1 and archived_at is null
		 order by position`, columnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CardOrder{}
	for rows.Next() {
		var c CardOrder
		if err := rows.Scan(&c.ID, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
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
	if err := requireColumn(ctx, tx, boardID, p.ColumnID); err != nil {
		return Patch{}, err
	}

	pos, err := nextPosition(ctx, tx, p.ColumnID, p.Placement, "")
	if err != nil {
		return Patch{}, err
	}

	var c Card
	err = tx.QueryRow(ctx, `
		insert into cards (org_id, board_id, column_id, position, title)
		values ($1, $2, $3, $4, $5)
		returning id, column_id, position, title, description, version`,
		orgID, boardID, p.ColumnID, pos, p.Title).
		Scan(&c.ID, &c.ColumnID, &c.Position, &c.Title, &c.Description, &c.Version)
	if err != nil {
		return Patch{}, err
	}

	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, "created", nil, &p.ColumnID, nil); err != nil {
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
	if err := requireColumn(ctx, tx, boardID, p.ToColumnID); err != nil {
		return Patch{}, err
	}

	var fromColumn string
	err := tx.QueryRow(ctx, `
		select column_id from cards
		 where id = $1 and board_id = $2 and archived_at is null`,
		p.CardID, boardID).Scan(&fromColumn)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf(p.ToColumnID, "карточка уже удалена или перенесена на другую доску")
	}
	if err != nil {
		return Patch{}, err
	}

	pos, err := nextPosition(ctx, tx, p.ToColumnID, p.Placement, p.CardID)
	if err != nil {
		return Patch{}, err
	}

	var c Card
	err = tx.QueryRow(ctx, `
		update cards
		   set column_id = $2, position = $3, version = version + 1, updated_at = now()
		 where id = $1
		 returning id, column_id, position, title, description, version`,
		p.CardID, p.ToColumnID, pos).
		Scan(&c.ID, &c.ColumnID, &c.Position, &c.Title, &c.Description, &c.Version)
	if err != nil {
		return Patch{}, err
	}

	// Событие пишется на каждое перемещение, даже внутри одной колонки:
	// именно из этого журнала потом считаются cycle time и диаграмма потока.
	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, "moved", &fromColumn, &p.ToColumnID, nil); err != nil {
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

	var c Card
	err := tx.QueryRow(ctx, `
		update cards
		   set title       = coalesce($2, title),
		       description = coalesce($3, description),
		       version     = version + 1,
		       updated_at  = now()
		 where id = $1 and board_id = $4 and archived_at is null
		 returning id, column_id, position, title, description, version`,
		p.CardID, p.Title, p.Description, boardID).
		Scan(&c.ID, &c.ColumnID, &c.Position, &c.Title, &c.Description, &c.Version)
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
	var id, columnID string
	err := tx.QueryRow(ctx, `
		update cards set archived_at = now(), version = version + 1
		 where id = $1 and board_id = $2 and archived_at is null
		 returning id, column_id`, p.CardID, boardID).Scan(&id, &columnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}

	if err := logEvent(ctx, tx, orgID, boardID, id, actorID, "archived", &columnID, nil, nil); err != nil {
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

	var c Column
	err = tx.QueryRow(ctx, `
		insert into board_columns (org_id, board_id, name, position)
		values ($1, $2, $3, $4)
		returning id, name, position, wip_limit`, orgID, boardID, p.Name, pos).
		Scan(&c.ID, &c.Name, &c.Position, &c.WIPLimit)
	if err != nil {
		return Patch{}, err
	}
	return Patch{Columns: []Column{c}}, nil
}

type renameColumnPayload struct {
	ColumnID string `json:"columnId"`
	Name     string `json:"name"`
}

func renameColumn(ctx context.Context, tx pgx.Tx, _, boardID string, raw json.RawMessage) (Patch, error) {
	var p renameColumnPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор RENAME_COLUMN: %v", err)
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Patch{}, badRequestf("у колонки должно быть название")
	}

	var c Column
	err := tx.QueryRow(ctx, `
		update board_columns set name = $3
		 where id = $1 and board_id = $2 and archived_at is null
		 returning id, name, position, wip_limit`, p.ColumnID, boardID, p.Name).
		Scan(&c.ID, &c.Name, &c.Position, &c.WIPLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "колонка уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}
	return Patch{Columns: []Column{c}}, nil
}

// --- вспомогательное ---

func requireColumn(ctx context.Context, tx pgx.Tx, boardID, columnID string) error {
	if columnID == "" {
		return badRequestf("не указана колонка")
	}
	var exists bool
	err := tx.QueryRow(ctx, `
		select exists (
			select 1 from board_columns
			 where id = $1 and board_id = $2 and archived_at is null)`,
		columnID, boardID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return conflictf("", "колонка не найдена на этой доске")
	}
	return nil
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
