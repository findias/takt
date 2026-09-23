package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Ссылки карточки на заявки внешних систем.
//
// Своё поле держит одно значение, а у одной работы бывает три ЗНО
// и изменение, которым её выкатят. Поэтому ссылки — списком, у каждой
// вид. Виды закрыты: отчёт «работа по ЗНИ» обязан сравнивать вид,
// а не одинаковые подписи.

const (
	RefRDS     = "rds"
	RefZNO     = "zno"
	RefZNI     = "zni"
	RefProblem = "problem"
)

// RefKinds — виды в том порядке, в каком их показывают: от запроса
// к изменению и проблеме.
var RefKinds = []string{RefRDS, RefZNO, RefZNI, RefProblem}

// refLimit — длина ссылки. Адрес заявки с параметрами бывает длинным,
// но пятьсот знаков — уже не ссылка, а вставленный по ошибке текст.
// Число повторено в тексте отказа и в ограничении таблицы.
const refLimit = 500

type CardRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type addRefPayload struct {
	CardID string `json:"cardId"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
}

func addCardRef(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p addRefPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор ADD_CARD_REF: %v", err)
	}
	p.Ref = strings.TrimSpace(p.Ref)
	if p.CardID == "" {
		return Patch{}, badRequestf("нужна карточка")
	}
	if !validRefKind(p.Kind) {
		return Patch{}, badRequestf("неизвестный вид ссылки %q: бывают rds, zno, zni, problem", p.Kind)
	}
	if p.Ref == "" {
		return Patch{}, badRequestf("впишите номер заявки или её адрес")
	}
	if len([]rune(p.Ref)) > refLimit {
		return Patch{}, badRequestf("ссылка длиннее 500 знаков — похоже, вставлен не тот текст")
	}

	tag, err := tx.Exec(ctx, `
		insert into card_refs (org_id, card_id, kind, ref, created_by)
		select $1, $2, $3, $4, $5
		 where exists (select 1 from cards
		                where id = $2 and board_id = $6 and archived_at is null)`,
		orgID, p.CardID, p.Kind, p.Ref, actorID, boardID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Patch{}, conflictf("", "эта заявка уже есть на карточке")
		}
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		return Patch{}, conflictf("", "карточка не найдена или уже удалена")
	}
	return Patch{}, logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "ref_added",
		nil, nil, map[string]any{"kind": p.Kind, "ref": p.Ref})
}

type removeRefPayload struct {
	CardID string `json:"cardId"`
	RefID  string `json:"refId"`
}

func removeCardRef(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p removeRefPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор REMOVE_CARD_REF: %v", err)
	}
	if p.CardID == "" || p.RefID == "" {
		return Patch{}, badRequestf("нужны карточка и ссылка")
	}

	var kind, ref string
	err := tx.QueryRow(ctx, `
		delete from card_refs r
		 using cards c
		 where r.id = $1 and r.card_id = $2
		   and c.id = r.card_id and c.board_id = $3
		returning r.kind, r.ref`,
		p.RefID, p.CardID, boardID).Scan(&kind, &ref)
	// Ссылки уже нет — её убрал кто-то раньше. Желаемое состояние
	// достигнуто, и отказ здесь только напугал бы второго.
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, nil
	}
	if err != nil {
		return Patch{}, err
	}
	return Patch{}, logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "ref_removed",
		nil, nil, map[string]any{"kind": kind, "ref": ref})
}

func validRefKind(kind string) bool {
	for _, k := range RefKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// loadRefs добавляет к снимку ссылки всех карточек доски, по виду
// и в порядке появления.
func loadRefs(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		select r.card_id, r.id, r.kind, r.ref
		  from card_refs r
		  join cards c on c.id = r.card_id
		 where c.board_id = $1 and c.archived_at is null
		 order by r.created_at, r.id`, boardID)
	if err != nil {
		return err
	}
	defer rows.Close()

	snap.CardRefs = map[string][]CardRef{}
	for rows.Next() {
		var cardID string
		var r CardRef
		if err := rows.Scan(&cardID, &r.ID, &r.Kind, &r.Ref); err != nil {
			return err
		}
		snap.CardRefs[cardID] = append(snap.CardRefs[cardID], r)
	}
	return rows.Err()
}
