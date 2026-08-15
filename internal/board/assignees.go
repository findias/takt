package board

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Исполнители карточки.
//
// Их может быть несколько — пара за одной задачей, смежник на день,
// проверяющий, который её же и доводит. Прежнее «один исполнитель»
// заставляло про это врать: назначать одного, а остальных дописывать
// в описание, где их не найдёт ни фильтр, ни отчёт. Разбор решения —
// в миграции 0031.
//
// Устройство как у меток: назначение и снятие — разные операции, а патч
// несёт список целиком. Список целиком, а не «добавили такого-то»,
// потому что такой патч можно применить дважды без вреда и не нужно
// рассуждать о порядке — то же правило, что для меток.

type assignPayload struct {
	CardID string `json:"cardId"`
	UserID string `json:"userId"`
}

func parseAssign(raw json.RawMessage, op string) (assignPayload, error) {
	var p assignPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, badRequestf("разбор %s: %v", op, err)
	}
	if p.CardID == "" || p.UserID == "" {
		return p, badRequestf("нужны карточка и человек")
	}
	return p, nil
}

// assignCard добавляет исполнителя.
//
// Проверка «состоит в организации» делается здесь, а не внешним ключом:
// ключ на memberships пришлось бы каскадно чистить при исключении
// человека, то есть переписывать историю. Здесь же отказ объясним:
// назначить можно только того, кто в организации есть.
//
// Назначение не событие потока: оно не меняет ни колонку, ни отметки
// работы, и класть его в журнал переходов значило бы засорять то,
// из чего считаются метрики.
func assignCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	p, err := parseAssign(raw, "ASSIGN_CARD")
	if err != nil {
		return Patch{}, err
	}

	var member bool
	if err := tx.QueryRow(ctx, `
		select exists (select 1 from memberships
		                where org_id = $1 and user_id = $2)`,
		orgID, p.UserID).Scan(&member); err != nil {
		return Patch{}, err
	}
	if !member {
		return Patch{}, badRequestf("назначить можно только участника организации")
	}

	// Карточка проверяется тем же запросом, что и вставка: раздельная
	// проверка дала бы окно, в котором карточку успевают убрать.
	tag, err := tx.Exec(ctx, `
		insert into card_assignees (org_id, card_id, user_id, added_by)
		select $1, $2, $3, $4
		 where exists (select 1 from cards
		                where id = $2 and board_id = $5 and archived_at is null)
		on conflict (card_id, user_id) do nothing`,
		orgID, p.CardID, p.UserID, actorID, boardID)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		// Либо человек уже назначен — повтор безобиден, — либо карточки
		// нет. Второе от первого отличает отдельная проверка: молчать
		// в ответ на «назначил на несуществующее» нельзя.
		var already bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from card_assignees
			                where card_id = $1 and user_id = $2)`,
			p.CardID, p.UserID).Scan(&already); err != nil {
			return Patch{}, err
		}
		if !already {
			return Patch{}, conflictf("", "карточки нет на доске")
		}
	}
	return assigneePatch(ctx, tx, p.CardID)
}

// unassignCard снимает исполнителя. Снятие того, кого не назначали, —
// не ошибка: результат тот же, которого просили.
func unassignCard(ctx context.Context, tx pgx.Tx, orgID, boardID string, raw json.RawMessage) (Patch, error) {
	p, err := parseAssign(raw, "UNASSIGN_CARD")
	if err != nil {
		return Patch{}, err
	}
	_ = orgID
	if _, err := tx.Exec(ctx, `
		delete from card_assignees
		 where card_id = $1 and user_id = $2
		   and card_id in (select id from cards where board_id = $3)`,
		p.CardID, p.UserID, boardID); err != nil {
		return Patch{}, err
	}
	return assigneePatch(ctx, tx, p.CardID)
}

// assigneePatch отдаёт список исполнителей карточки целиком — в том
// порядке, в котором их назначали: первый в списке тот, кто взялся
// первым, и это единственное, чем список отвечает на вопрос
// «кто отвечает».
func assigneePatch(ctx context.Context, tx pgx.Tx, cardID string) (Patch, error) {
	ids, err := assigneesOf(ctx, tx, cardID)
	if err != nil {
		return Patch{}, err
	}
	return Patch{CardAssignees: map[string][]string{cardID: ids}}, nil
}

func assigneesOf(ctx context.Context, tx pgx.Tx, cardID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`select user_id from card_assignees where card_id = $1 order by added_at, user_id`,
		cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// loadAssignees кладёт в снимок, кто над чем работает: cardId → люди
// в порядке назначения.
func loadAssignees(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	snap.CardAssignees = map[string][]string{}
	rows, err := tx.Query(ctx, `
		select a.card_id, a.user_id
		  from card_assignees a join cards c on c.id = a.card_id
		 where c.board_id = $1 and c.archived_at is null
		 order by a.added_at, a.user_id`, boardID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID, userID string
		if err := rows.Scan(&cardID, &userID); err != nil {
			return err
		}
		snap.CardAssignees[cardID] = append(snap.CardAssignees[cardID], userID)
	}
	return rows.Err()
}
