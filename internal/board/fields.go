package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Свои поля карточки.
//
// Определения принадлежат организации, значения — карточке. Значение
// хранится в колонке своего типа, а не в общем jsonb: по числу в jsonb
// нельзя сравнивать без приведения на каждой строке, а сортировка дат,
// лежащих строками, даёт всем известный результат.

var (
	ErrFieldExists   = errors.New("поле с таким названием уже есть")
	ErrFieldMismatch = errors.New("значение не подходит виду поля")
)

const (
	FieldText     = "text"
	FieldNumber   = "number"
	FieldDate     = "date"
	FieldSelect   = "select"
	FieldCheckbox = "checkbox"
)

type Field struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Options []string `json:"options"`
}

// FieldValue — значение одного поля у одной карточки. Наружу отдаётся
// одним полем `value`: клиенту незачем знать, в какой колонке оно лежало.
type FieldValue struct {
	FieldID string `json:"fieldId"`
	Value   any    `json:"value"`
}

func (s *Service) Fields(ctx context.Context, orgID, userID string) ([]Field, error) {
	out := []Field{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, name, kind, options
			  from card_fields
			 where archived_at is null
			 order by created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f Field
			var options []byte
			if err := rows.Scan(&f.ID, &f.Name, &f.Kind, &options); err != nil {
				return err
			}
			if err := json.Unmarshal(options, &f.Options); err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateField(ctx context.Context, orgID, actorID, name, kind string, options []string) (Field, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Field{}, badRequestf("у поля должно быть название")
	}
	switch kind {
	case FieldText, FieldNumber, FieldDate, FieldSelect, FieldCheckbox:
	default:
		return Field{}, badRequestf("неизвестный вид поля %q", kind)
	}

	cleaned := []string{}
	seen := map[string]bool{}
	for _, option := range options {
		option = strings.TrimSpace(option)
		// Пустой и повторяющийся вариант — не выбор, а способ получить
		// два неразличимых значения в отчёте.
		if option == "" || seen[strings.ToLower(option)] {
			continue
		}
		seen[strings.ToLower(option)] = true
		cleaned = append(cleaned, option)
	}
	if kind == FieldSelect && len(cleaned) == 0 {
		return Field{}, badRequestf("у поля с выбором должны быть варианты")
	}
	if kind != FieldSelect {
		cleaned = []string{}
	}

	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return Field{}, err
	}

	var f Field
	f.Options = cleaned
	err = s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			insert into card_fields (org_id, name, kind, options)
			values ($1, $2, $3, $4::jsonb)
			returning id, name, kind`,
			orgID, name, kind, string(encoded)).Scan(&f.ID, &f.Name, &f.Kind)
	})
	return f, translateField(err)
}

// ArchiveField убирает поле из обихода, не трогая значения. Удалить его
// значило бы стереть данные карточек — а поле заводили как раз затем,
// чтобы эти данные были.
func (s *Service) ArchiveField(ctx context.Context, orgID, actorID, fieldID string) error {
	return translateField(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update card_fields set archived_at = now()
			 where id = $1 and archived_at is null`, fieldID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

// --- операция над карточкой ---

type setFieldPayload struct {
	CardID  string          `json:"cardId"`
	FieldID string          `json:"fieldId"`
	Value   json.RawMessage `json:"value"`
}

func setCardField(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p setFieldPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор SET_CARD_FIELD: %v", err)
	}
	if p.CardID == "" || p.FieldID == "" {
		return Patch{}, badRequestf("нужны карточка и поле")
	}

	var kind string
	err := tx.QueryRow(ctx,
		`select kind from card_fields where id = $1 and archived_at is null`,
		p.FieldID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, ErrNotFound
	}
	if err != nil {
		return Patch{}, err
	}

	// Пустое значение снимает поле: «поля нет» и «поле пустое» — одно
	// и то же, и заводить для этого третье состояние незачем.
	if len(p.Value) == 0 || string(p.Value) == "null" {
		if _, err := tx.Exec(ctx,
			`delete from card_field_values where card_id = $1 and field_id = $2`,
			p.CardID, p.FieldID); err != nil {
			return Patch{}, err
		}
		return Patch{}, logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "field_cleared",
			nil, nil, map[string]any{"fieldId": p.FieldID})
	}

	value, err := decodeFieldValue(kind, p.Value)
	if err != nil {
		return Patch{}, err
	}

	// Имя колонки выбирается перечислением, а не картой: у карты
	// неизвестный вид даёт пустую строку, и в запрос уезжает
	// `insert into … ( , updated_by)` — то есть синтаксическая ошибка
	// базы вместо отказа с объяснением. Нашлось проверкой на склейку
	// запросов: инъекции здесь не было (значения свои), а вот отказ
	// был не тот.
	column, err := valueColumn(kind)
	if err != nil {
		return Patch{}, err
	}

	// В excluded попадают все колонки: та, что перечислена во вставке,
	// со значением, остальные — пустыми. Поэтому при конфликте достаточно
	// переписать все пять, и смена вида поля не оставит хвоста от прежнего.
	//
	// #sql-склейка: имя колонки, а не значение — параметром его передать
	// нельзя. Приходит из valueColumn выше: перечисление из пяти наших
	// литералов, неизвестный вид отвергается отказом.
	_, err = tx.Exec(ctx, `
		insert into card_field_values (org_id, card_id, field_id, `+column+`, updated_by)
		select $1, $2, $3, $4, $5
		 where exists (select 1 from cards
		                where id = $2 and board_id = $6 and archived_at is null)
		on conflict (card_id, field_id) do update
		   set value_text   = excluded.value_text,
		       value_number = excluded.value_number,
		       value_date   = excluded.value_date,
		       value_bool   = excluded.value_bool,
		       value_option = excluded.value_option,
		       updated_at   = now(),
		       updated_by   = excluded.updated_by`,
		orgID, p.CardID, p.FieldID, value, actorID, boardID)
	if err != nil {
		return Patch{}, translateField(err)
	}

	return Patch{}, logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "field_set",
		nil, nil, map[string]any{"fieldId": p.FieldID, "value": value})
}

// decodeFieldValue переводит присланное значение в то, что примет колонка
// своего типа. Проверка вида здесь — ради внятного отказа; последнее слово
// всё равно за триггером базы.
// valueColumn — в какую колонку ложится значение поля этого вида.
// Перечислением и с отказом: список видов закрыт, и новый вид обязан
// добавиться сюда осознанно, а не подставиться пустой строкой.
func valueColumn(kind string) (string, error) {
	switch kind {
	case FieldText:
		return "value_text", nil
	case FieldNumber:
		return "value_number", nil
	case FieldDate:
		return "value_date", nil
	case FieldCheckbox:
		return "value_bool", nil
	case FieldSelect:
		return "value_option", nil
	default:
		return "", fmt.Errorf("поля вида %q не бывает", kind)
	}
}

func decodeFieldValue(kind string, raw json.RawMessage) (any, error) {
	switch kind {
	case FieldText, FieldSelect:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil || strings.TrimSpace(v) == "" {
			return nil, badRequestf("%s: ожидалась строка", ErrFieldMismatch)
		}
		return strings.TrimSpace(v), nil
	case FieldNumber:
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, badRequestf("%s: ожидалось число", ErrFieldMismatch)
		}
		return v, nil
	case FieldCheckbox:
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, badRequestf("%s: ожидалось да или нет", ErrFieldMismatch)
		}
		return v, nil
	case FieldDate:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, badRequestf("%s: ожидалась дата", ErrFieldMismatch)
		}
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return nil, badRequestf("%s: дата пишется как ГГГГ-ММ-ДД", ErrFieldMismatch)
		}
		return v, nil
	}
	return nil, badRequestf("неизвестный вид поля %q", kind)
}

// loadFieldValues добавляет к снимку значения полей всех карточек доски.
func loadFieldValues(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		select v.card_id, v.field_id,
		       v.value_text, v.value_number, v.value_date, v.value_bool, v.value_option
		  from card_field_values v
		  join cards c on c.id = v.card_id
		 where c.board_id = $1 and c.archived_at is null`, boardID)
	if err != nil {
		return err
	}
	defer rows.Close()

	snap.FieldValues = map[string][]FieldValue{}
	for rows.Next() {
		var cardID string
		var fv FieldValue
		var text, option *string
		var number *float64
		var date *time.Time
		var flag *bool
		if err := rows.Scan(&cardID, &fv.FieldID, &text, &number, &date, &flag, &option); err != nil {
			return err
		}
		switch {
		case text != nil:
			fv.Value = *text
		case number != nil:
			fv.Value = *number
		case date != nil:
			fv.Value = date.Format("2006-01-02")
		case flag != nil:
			fv.Value = *flag
		case option != nil:
			fv.Value = *option
		}
		snap.FieldValues[cardID] = append(snap.FieldValues[cardID], fv)
	}
	return rows.Err()
}

func translateField(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "card_fields_name_idx":
			return ErrFieldExists
		case pgErr.Code == "23514":
			return badRequestf("%s", pgErr.Message)
		}
	}
	return err
}

// loadFields кладёт в снимок словарь полей организации и значения карточек
// доски. Словарь идёт вместе со значениями: без названий и видов значения
// показать нечем, а отдельным запросом клиент получил бы их вразнобой.
func loadFields(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		select id, name, kind, options
		  from card_fields
		 where archived_at is null
		 order by created_at`)
	if err != nil {
		return err
	}
	defer rows.Close()
	snap.Fields = []Field{}
	for rows.Next() {
		var f Field
		var options []byte
		if err := rows.Scan(&f.ID, &f.Name, &f.Kind, &options); err != nil {
			return err
		}
		if err := json.Unmarshal(options, &f.Options); err != nil {
			return err
		}
		snap.Fields = append(snap.Fields, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	return loadFieldValues(ctx, tx, boardID, snap)
}
