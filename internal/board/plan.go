package board

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrPlanChanged = errors.New("план уже изменён: проверьте новые даты и повторите правку")

// Plan хранит дни: часы и часовые пояса в плане были бы выдумкой.
type Plan struct {
	Start *string `json:"plannedStartOn"`
	End   *string `json:"plannedEndOn"`
}

func setCardPlan(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p struct {
		CardID   string         `json:"cardId"`
		Start    optionalString `json:"plannedStartOn"`
		End      optionalString `json:"plannedEndOn"`
		Expected *Plan          `json:"expected"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор SET_CARD_PLAN: %v", err)
	}
	if !p.Start.Set || !p.End.Set || p.Expected == nil {
		return Patch{}, badRequestf("передайте обе даты плана и исходный план")
	}
	for _, day := range []*string{p.Start.Value, p.End.Value, p.Expected.Start, p.Expected.End} {
		if day == nil {
			continue
		}
		if _, err := time.Parse(time.DateOnly, *day); err != nil || *day < "0001-01-01" || *day > "9999-12-31" {
			return Patch{}, badRequestf("дата плана пишется как 2026-08-21")
		}
	}
	if p.Start.Value != nil && p.End.Value != nil && *p.Start.Value > *p.End.Value {
		return Patch{}, badRequestf("окончание плана не может быть раньше начала")
	}
	// Apply уже держит замок доски. Сравниваем именно план: чужая правка
	// названия не должна мешать, а чужие даты нельзя молча перезаписать.
	c, err := scanCard(tx.QueryRow(ctx, `select `+cardFields+` from cards where id=$1 and board_id=$2 and archived_at is null`, p.CardID, boardID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка не найдена или уже удалена")
	}
	if err != nil {
		return Patch{}, err
	}
	same := func(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
	if !same(c.PlannedStartOn, p.Expected.Start) || !same(c.PlannedEndOn, p.Expected.End) {
		return Patch{}, ErrPlanChanged
	}
	c, err = scanCard(tx.QueryRow(ctx, `update cards set planned_start_on=$2::date, planned_end_on=$3::date, version=version+1, updated_at=now() where id=$1 returning `+cardFields, c.ID, p.Start.Value, p.End.Value))
	if err != nil {
		return Patch{}, err
	}
	if err := completeCard(ctx, tx, &c); err != nil {
		return Patch{}, err
	}
	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, "planned", nil, nil, map[string]any{"plannedStartOn": p.Start.Value, "plannedEndOn": p.End.Value}); err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}
