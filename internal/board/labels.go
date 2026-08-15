package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Метки.
//
// Устроены как свои поля карточки: определение принадлежит организации,
// а не доске, — иначе «срочно» на двух досках оказалось бы двумя разными
// метками, и фильтр по организации собрать было бы не из чего.
//
// Цвет — имя оттенка, а не значение. Хранить «#e07a5f» значит завести
// цвет, который в тёмной теме начнёт светиться, и правило «сырых цветов
// в правилах нет» перестанет действовать ровно там, где данные приходят
// из базы.

// ErrLabelExists — метка с таким названием уже есть. Две одинаковые
// метки начинают вешать вперемешку, а фильтр показывает половину.
var ErrLabelExists = errors.New("метка с таким названием уже есть")

// Tones — закрытый набор оттенков, тот же, что у аватаров.
var Tones = []string{"slate", "green", "blue", "violet", "rose", "amber", "teal", "brown"}

type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tone string `json:"tone"`
}

func (s *Service) Labels(ctx context.Context, orgID, userID string) ([]Label, error) {
	out := []Label{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, name, tone from labels
			 where archived_at is null
			 order by lower(name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l Label
			if err := rows.Scan(&l.ID, &l.Name, &l.Tone); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateLabel(ctx context.Context, orgID, actorID, name, tone string) (Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Label{}, badRequestf("у метки должно быть название")
	}
	if tone == "" {
		tone = Tones[0]
	}
	l := Label{Name: name, Tone: tone}
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			insert into labels (org_id, name, tone) values ($1, $2, $3)
			returning id, name, tone`, orgID, name, tone).Scan(&l.ID, &l.Name, &l.Tone)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.ConstraintName == "labels_name_idx":
			return Label{}, ErrLabelExists
		case pgErr.ConstraintName == "labels_tone_valid":
			return Label{}, badRequestf("незнакомый оттенок метки")
		}
	}
	return l, err
}

// ArchiveLabel убирает метку из обихода, не снимая её с карточек.
//
// Удалять нельзя: карточка, помеченная «срочно» полгода назад, объясняет
// этим своё время в очереди, и стирание метки задним числом делает
// историю неверной. Убранная метка перестаёт предлагаться, но остаётся
// видимой там, где уже висит.
func (s *Service) ArchiveLabel(ctx context.Context, orgID, actorID, labelID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update labels set archived_at = now() where id = $1 and archived_at is null`, labelID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// --- операции над карточкой ---

type labelCardPayload struct {
	CardID  string `json:"cardId"`
	LabelID string `json:"labelId"`
}

func labelCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	p, err := parseLabelCard(raw, "LABEL_CARD")
	if err != nil {
		return Patch{}, err
	}

	// Вешаем только на карточку этой доски и только существующей меткой.
	// Обе проверки — одним запросом: два раздельных дали бы окно,
	// в котором метку успевают убрать.
	tag, err := tx.Exec(ctx, `
		insert into card_labels (org_id, card_id, label_id, added_by)
		select $1, $2, $3, $4
		 where exists (select 1 from cards
		                where id = $2 and board_id = $5 and archived_at is null)
		   and exists (select 1 from labels where id = $3 and archived_at is null)
		on conflict (card_id, label_id) do nothing`,
		orgID, p.CardID, p.LabelID, actorID, boardID)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		// Либо метка уже висит — повтор безобиден, — либо карточки
		// или метки нет. Второе от первого отличает отдельная проверка:
		// молчать в ответ на «повесил на несуществующее» нельзя.
		var ok bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from card_labels where card_id = $1 and label_id = $2)`,
			p.CardID, p.LabelID).Scan(&ok); err != nil {
			return Patch{}, err
		}
		if !ok {
			return Patch{}, conflictf("", "метки или карточки нет")
		}
	}
	return labelPatch(ctx, tx, p.CardID)
}

func unlabelCard(ctx context.Context, tx pgx.Tx, orgID, boardID string, raw json.RawMessage) (Patch, error) {
	p, err := parseLabelCard(raw, "UNLABEL_CARD")
	if err != nil {
		return Patch{}, err
	}
	// Снятие несуществующей метки — не ошибка: повтор отмены обычное дело.
	if _, err = tx.Exec(ctx, `
		delete from card_labels
		 where org_id = $1 and card_id = $2 and label_id = $3
		   and exists (select 1 from cards where id = $2 and board_id = $4)`,
		orgID, p.CardID, p.LabelID, boardID); err != nil {
		return Patch{}, err
	}
	return labelPatch(ctx, tx, p.CardID)
}

// labelPatch отдаёт метки карточки целиком.
//
// Именно целиком, а не «добавили такую-то»: иначе догоняющий клиент,
// применивший патч дважды, получил бы задвоение, а пропустивший один —
// расхождение. Список меток на карточке короткий, и передать его
// полностью дешевле, чем рассуждать о порядке применения.
func labelPatch(ctx context.Context, tx pgx.Tx, cardID string) (Patch, error) {
	rows, err := tx.Query(ctx,
		`select label_id from card_labels where card_id = $1`, cardID)
	if err != nil {
		return Patch{}, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Patch{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return Patch{}, err
	}
	return Patch{CardLabels: map[string][]string{cardID: ids}}, nil
}

func parseLabelCard(raw json.RawMessage, op string) (labelCardPayload, error) {
	var p labelCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, badRequestf("разбор %s: %v", op, err)
	}
	if p.CardID == "" || p.LabelID == "" {
		return p, badRequestf("%s: нужны карточка и метка", op)
	}
	return p, nil
}

// loadLabels кладёт в снимок словарь меток организации и то, что чем
// помечено. Раздельно, а не списком меток внутри каждой карточки:
// иначе название метки уезжало бы в снимок столько раз, на скольких
// карточках оно висит.
func loadLabels(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	snap.Labels = []Label{}
	snap.CardLabels = map[string][]string{}

	rows, err := tx.Query(ctx, `
		select id, name, tone from labels
		 where archived_at is null
		 order by lower(name)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Tone); err != nil {
			return err
		}
		snap.Labels = append(snap.Labels, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Метки на карточках доски — вместе с убранными: карточка,
	// помеченная полгода назад, объясняет этим своё время в очереди.
	pairs, err := tx.Query(ctx, `
		select cl.card_id, cl.label_id
		  from card_labels cl join cards c on c.id = cl.card_id
		 where c.board_id = $1 and c.archived_at is null`, boardID)
	if err != nil {
		return err
	}
	defer pairs.Close()
	for pairs.Next() {
		var cardID, labelID string
		if err := pairs.Scan(&cardID, &labelID); err != nil {
			return err
		}
		snap.CardLabels[cardID] = append(snap.CardLabels[cardID], labelID)
	}
	return pairs.Err()
}
