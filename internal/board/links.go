package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Связи карточек и блокировки.
//
// Подзадача — это связь, а не поле `parent_id`, по трём причинам: видов
// связи больше одного, появление связи само по себе является событием
// (из него считается доля разбиения задач), и подзадача может лежать на
// доске другой команды — то есть связь не принадлежит ни одной из досок.

type linkPayload struct {
	FromCard string `json:"fromCard"`
	ToCard   string `json:"toCard"`
	Kind     string `json:"kind"`
}

func (p *linkPayload) validate() error {
	if p.Kind == "" {
		p.Kind = LinkSubtask
	}
	switch p.Kind {
	case LinkSubtask, LinkBlocks, LinkRelates:
	default:
		return badRequestf("недопустимый вид связи %q, ожидалось %s, %s или %s",
			p.Kind, LinkSubtask, LinkBlocks, LinkRelates)
	}
	if p.FromCard == "" || p.ToCard == "" {
		return badRequestf("для связи нужны обе карточки")
	}
	if p.FromCard == p.ToCard {
		return badRequestf("карточка не может быть связана сама с собой")
	}
	return nil
}

// linkCards связывает две карточки. Карточки могут лежать на разных досках
// и в разных проектах — это и есть смысл операции; ограничение только одно,
// общая организация, и его обеспечивает изоляция на уровне базы.
func linkCards(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p linkPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор LINK_CARDS: %v", err)
	}
	if err := p.validate(); err != nil {
		return Patch{}, err
	}

	for _, id := range []string{p.FromCard, p.ToCard} {
		var exists bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from cards where id = $1 and archived_at is null)`,
			id).Scan(&exists); err != nil {
			return Patch{}, err
		}
		if !exists {
			return Patch{}, conflictf("", "карточка не найдена или уже удалена")
		}
	}

	if p.Kind == LinkSubtask {
		if err := checkSubtaskTree(ctx, tx, p.FromCard, p.ToCard); err != nil {
			return Patch{}, err
		}
	}

	if err := insertLink(ctx, tx, orgID, actorID, boardID, p); err != nil {
		return Patch{}, err
	}
	return linkPatch(ctx, tx, boardID, p)
}

// insertLink кладёт связь и пишет события. Отдельно от linkCards потому,
// что связь возникает не только по просьбе связать: подзадача создаётся
// и связывается одной операцией.
func insertLink(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, p linkPayload,
) error {
	tag, err := tx.Exec(ctx, `
		insert into card_links (org_id, from_card, to_card, kind, created_by)
		values ($1, $2, $3, $4, $5)
		on conflict (from_card, to_card, kind) do nothing`,
		orgID, p.FromCard, p.ToCard, p.Kind, actorID)
	if err != nil {
		// Единственный частичный индекс здесь — «у подзадачи один родитель».
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return conflictf("", "у этой карточки уже есть родительская задача")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		// Связь уже была — операция идемпотентна по смыслу, а не только
		// по идентификатору: повтор не ошибка.
		return nil
	}

	// Событие пишется обеим сторонам: и родитель, и подзадача участвуют
	// в разных отчётах, и связывать их постфактум по времени неправильно.
	payload := map[string]any{"kind": p.Kind, "fromCard": p.FromCard, "toCard": p.ToCard}
	if err := logEvent(ctx, tx, orgID, boardID, p.FromCard, actorID, "linked", nil, nil, payload); err != nil {
		return err
	}
	return logEvent(ctx, tx, orgID, boardID, p.ToCard, actorID, "linked", nil, nil, payload)
}

type createSubtaskPayload struct {
	ParentCardID string `json:"parentCardId"`
	Title        string `json:"title"`
	// Куда положить подзадачу. Пусто — в первую колонку доски: чаще всего
	// подзадачу заводят из панели родителя, где колонку не выбирают,
	// а «начало доски» — единственный ответ, не требующий догадок.
	ColumnID string `json:"columnId"`
}

// createSubtask заводит карточку и тут же связывает её с родителем.
//
// Одной операцией, а не двумя вызовами с клиента: два вызова означают
// два способа оборваться посередине, и оба оставляют мусор — карточку
// без родителя, о которой никто не просил, или связь на несозданное.
// Здесь всё в одной транзакции: либо подзадача есть, либо ничего
// не произошло.
func createSubtask(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage,
) (Patch, error) {
	var p createSubtaskPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор CREATE_SUBTASK: %v", err)
	}
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return Patch{}, badRequestf("у подзадачи должно быть название")
	}
	if p.ParentCardID == "" {
		return Patch{}, badRequestf("не сказано, чьей подзадачей она будет")
	}

	var parentExists bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from cards where id = $1 and archived_at is null)`,
		p.ParentCardID).Scan(&parentExists); err != nil {
		return Patch{}, err
	}
	if !parentExists {
		return Patch{}, conflictf("", "карточка не найдена или уже удалена")
	}

	col, err := subtaskColumn(ctx, tx, boardID, p.ColumnID)
	if err != nil {
		return Patch{}, err
	}
	if err := enforceWIP(ctx, tx, col); err != nil {
		return Patch{}, err
	}

	card, err := insertCard(ctx, tx, orgID, actorID, boardID, col, p.Title, Placement{Place: "end"})
	if err != nil {
		return Patch{}, err
	}

	link := linkPayload{FromCard: p.ParentCardID, ToCard: card.ID, Kind: LinkSubtask}
	if err := checkSubtaskTree(ctx, tx, link.FromCard, link.ToCard); err != nil {
		return Patch{}, err
	}
	if err := insertLink(ctx, tx, orgID, actorID, boardID, link); err != nil {
		return Patch{}, err
	}
	return linkPatch(ctx, tx, boardID, link)
}

// subtaskColumn выбирает колонку для подзадачи: названную или первую
// на доске.
func subtaskColumn(ctx context.Context, tx pgx.Tx, boardID, columnID string) (Column, error) {
	if columnID != "" {
		return loadColumn(ctx, tx, boardID, columnID)
	}
	var id string
	err := tx.QueryRow(ctx,
		`select id from board_columns where board_id = $1 order by position limit 1`,
		boardID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Column{}, badRequestf("на доске нет ни одной колонки")
	}
	if err != nil {
		return Column{}, err
	}
	return loadColumn(ctx, tx, boardID, id)
}

func unlinkCards(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p linkPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор UNLINK_CARDS: %v", err)
	}
	if err := p.validate(); err != nil {
		return Patch{}, err
	}

	tag, err := tx.Exec(ctx, `
		delete from card_links
		 where from_card = $1 and to_card = $2 and kind = $3`,
		p.FromCard, p.ToCard, p.Kind)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() > 0 {
		payload := map[string]any{"kind": p.Kind, "fromCard": p.FromCard, "toCard": p.ToCard}
		if err := logEvent(ctx, tx, orgID, boardID, p.FromCard, actorID, "unlinked", nil, nil, payload); err != nil {
			return Patch{}, err
		}
	}
	return linkPatch(ctx, tx, boardID, p)
}

// checkSubtaskTree не даёт замкнуть цикл и уйти глубже предела.
//
// Подзадачи — дерево: у карточки один родитель (это обеспечено частичным
// уникальным индексом), а здесь проверяется вторая половина — что новый
// родитель не находится ниже по этому же дереву.
func checkSubtaskTree(ctx context.Context, tx pgx.Tx, parent, child string) error {
	// Потомки ребёнка: если среди них будущий родитель — связь замкнёт цикл.
	var cycle bool
	err := tx.QueryRow(ctx, `
		with recursive descendants as (
		    select to_card from card_links
		     where from_card = $1 and kind = 'subtask'
		  union
		    select l.to_card from card_links l
		      join descendants d on d.to_card = l.from_card
		     where l.kind = 'subtask'
		)
		select exists (select 1 from descendants where to_card = $2)
		    or $1 = $2`, child, parent).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return conflictf("", "так связь замкнётся в цикл: карточка окажется своей же подзадачей")
	}

	// Глубина считается от корня будущего родителя вниз через ребёнка.
	var above, below int
	err = tx.QueryRow(ctx, `
		with recursive up as (
		    select from_card, 1 as depth from card_links
		     where to_card = $1 and kind = 'subtask'
		  union all
		    select l.from_card, u.depth + 1 from card_links l
		      join up u on u.from_card = l.to_card
		     where l.kind = 'subtask' and u.depth < 50
		)
		select coalesce(max(depth), 0) from up`, parent).Scan(&above)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `
		with recursive down as (
		    select to_card, 1 as depth from card_links
		     where from_card = $1 and kind = 'subtask'
		  union all
		    select l.to_card, d.depth + 1 from card_links l
		      join down d on d.to_card = l.from_card
		     where l.kind = 'subtask' and d.depth < 50
		)
		select coalesce(max(depth), 0) from down`, child).Scan(&below)
	if err != nil {
		return err
	}

	// above уровней над родителем + сам родитель + ребёнок + below под ним.
	if above+2+below > MaxSubtaskDepth {
		return conflictf("", "слишком глубокое дерево подзадач: предел — %d уровней", MaxSubtaskDepth)
	}
	return nil
}

// linkPatch возвращает обе затронутые карточки: у родителя изменился
// прогресс, а у подзадачи — состав связей.
func linkPatch(ctx context.Context, tx pgx.Tx, boardID string, p linkPayload) (Patch, error) {
	patch := Patch{}
	for _, id := range []string{p.FromCard, p.ToCard} {
		c, err := readCard(ctx, tx, boardID, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // карточка на другой доске — этому клиенту её не показываем
		}
		if err != nil {
			return Patch{}, err
		}
		patch.Cards = append(patch.Cards, c)
	}
	return patch, nil
}

// readCard читает карточку доски вместе с вычисляемым прогрессом и
// блокировкой — тем же составом, что отдаёт снимок доски.
func readCard(ctx context.Context, tx pgx.Tx, boardID, cardID string) (Card, error) {
	c, err := scanCard(tx.QueryRow(ctx, `
		select `+cardFields+` from cards
		 where id = $1 and board_id = $2 and archived_at is null`, cardID, boardID))
	if err != nil {
		return Card{}, err
	}

	var p Progress
	err = tx.QueryRow(ctx, `
		select count(*), count(*) filter (where c.outcome = 'done')
		  from card_links l
		  join cards c on c.id = l.to_card and c.archived_at is null
		 where l.from_card = $1 and l.kind = 'subtask'`, cardID).Scan(&p.Total, &p.Done)
	if err != nil {
		return Card{}, err
	}
	if p.Total > 0 {
		c.Progress = &p
	}

	var b Block
	err = tx.QueryRow(ctx, `
		select id, reason, blocked_at from card_blocks
		 where card_id = $1 and unblocked_at is null`, cardID).
		Scan(&b.ID, &b.Reason, &b.BlockedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Card{}, err
	default:
		c.Blocked = &b
	}
	return c, nil
}

// --- блокировки ---

type blockPayload struct {
	CardID string `json:"cardId"`
	Reason string `json:"reason"`
}

func blockCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p blockPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор BLOCK_CARD: %v", err)
	}
	p.Reason = strings.TrimSpace(p.Reason)
	if p.Reason == "" {
		return Patch{}, badRequestf("у блокировки должна быть причина")
	}

	tag, err := tx.Exec(ctx, `
		insert into card_blocks (org_id, card_id, reason, blocked_by)
		select $1, $2, $3, $4
		 where exists (select 1 from cards
		                where id = $2 and board_id = $5 and archived_at is null)
		   and not exists (select 1 from card_blocks
		                    where card_id = $2 and unblocked_at is null)`,
		orgID, p.CardID, p.Reason, actorID, boardID)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		return Patch{}, conflictf("", "карточка уже заблокирована или недоступна")
	}

	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "blocked", nil, nil,
		map[string]any{"reason": p.Reason}); err != nil {
		return Patch{}, err
	}
	c, err := readCard(ctx, tx, boardID, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}

func unblockCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p blockPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор UNBLOCK_CARD: %v", err)
	}

	// Интервал закрывается, а не удаляется: время, проведённое в блоке,
	// и есть то, ради чего блокировки моделируются интервалом.
	tag, err := tx.Exec(ctx, `
		update card_blocks set unblocked_at = now(), unblocked_by = $3
		 where card_id = $1 and unblocked_at is null
		   and exists (select 1 from cards where id = $1 and board_id = $2)`,
		p.CardID, boardID, actorID)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		return Patch{}, conflictf("", "карточка не заблокирована")
	}

	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "unblocked", nil, nil, nil); err != nil {
		return Patch{}, err
	}
	c, err := readCard(ctx, tx, boardID, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}
