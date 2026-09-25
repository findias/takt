package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

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

	fromBoard, err := cardBoard(ctx, tx, p.FromCard)
	if err != nil {
		return Patch{}, err
	}
	toBoard, err := cardBoard(ctx, tx, p.ToCard)
	if err != nil {
		return Patch{}, err
	}

	if p.Kind == LinkSubtask {
		if err := checkEpicStaysParent(ctx, tx, fromBoard, toBoard); err != nil {
			return Patch{}, err
		}
		if err := checkSubtaskTree(ctx, tx, p.FromCard, p.ToCard); err != nil {
			return Patch{}, err
		}
	}

	if err := insertLink(ctx, tx, orgID, actorID, p, fromBoard, toBoard); err != nil {
		return Patch{}, err
	}
	return linkPatch(ctx, tx, boardID, p)
}

// checkEpicStaysParent не даёт эпику встать под задачу команды.
//
// Эпик — карточка доски-портфеля, и в иерархии он всегда родитель:
// эпик › фича › задача. С доски команды можно было завести подзадачу
// на портфеле или связать карточку портфеля подзадачей задачи — эпик
// оказывался под задачей, прогресс и путь до корня шли наоборот
// (замечено владельцем 25.09.2026). Под эпиком эпик — можно: большой
// эпик делят на меньшие на том же портфеле.
func checkEpicStaysParent(ctx context.Context, tx pgx.Tx, parentBoard, childBoard string) error {
	var parentLevel, childLevel string
	if err := tx.QueryRow(ctx, `
		select (select level from boards where id = $1), (select level from boards where id = $2)`,
		parentBoard, childBoard).Scan(&parentLevel, &childLevel); err != nil {
		return err
	}
	if childLevel == LevelPortfolio && parentLevel != LevelPortfolio {
		return conflictf("", "эпик не может быть подзадачей задачи: эпик всегда родитель. "+
			"Чтобы связать задачу с эпиком, откройте задачу и в «Связать с существующей карточкой» выберите вид «Родитель»")
	}
	return nil
}

// cardBoard говорит, на какой доске лежит карточка, и заодно отвечает
// на вопрос, есть ли она вовсе.
//
// Доска нужна событиям. Событие принадлежит доске, на которой лежит его
// карточка, а не доске, с которой пришла операция: связь через границу
// команд иначе писала бы соседям в ленту событие о карточке, которой
// у них нет, и то же событие не попадало бы в ленту той доски, где
// работа на самом деле лежит.
func cardBoard(ctx context.Context, tx pgx.Tx, cardID string) (string, error) {
	var boardID string
	err := tx.QueryRow(ctx,
		`select board_id from cards where id = $1 and archived_at is null`,
		cardID).Scan(&boardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", conflictf("", "карточка не найдена или уже удалена")
	}
	if err != nil {
		return "", err
	}
	return boardID, nil
}

// insertLink кладёт связь и пишет события. Отдельно от linkCards потому,
// что связь возникает не только по просьбе связать: подзадача создаётся
// и связывается одной операцией.
func insertLink(
	ctx context.Context, tx pgx.Tx, orgID, actorID string, p linkPayload,
	fromBoard, toBoard string,
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
	if err := logEvent(ctx, tx, orgID, fromBoard, p.FromCard, actorID, "linked", nil, nil, payload); err != nil {
		return err
	}
	return logEvent(ctx, tx, orgID, toBoard, p.ToCard, actorID, "linked", nil, nil, payload)
}

type createSubtaskPayload struct {
	ParentCardID string `json:"parentCardId"`
	Title        string `json:"title"`
	// Куда положить подзадачу. Пусто — в первую колонку доски: чаще всего
	// подзадачу заводят из панели родителя, где колонку не выбирают,
	// а «начало доски» — единственный ответ, не требующий догадок.
	ColumnID string `json:"columnId"`
	// На какой доске завести подзадачу. Пусто — на этой.
	//
	// Не пусто — это постановка работы соседям, и устроена она тем же,
	// чем всякая работа: карточкой на доске исполнителя. Отдельной
	// сущности «заявка» нет намеренно — принятая заявка превратилась бы
	// в карточку, и две записи об одном деле были бы обязаны совпадать,
	// не будучи обязанными совпасть.
	//
	// Отдельного права тоже нет: нужна обычная запись в доску, а доску
	// с видимостью org она уже даёт. Правила доски-получателя при этом
	// не обходятся — карточка ложится в её колонку и под её лимит.
	BoardID string `json:"boardId"`
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

	parentBoard, err := cardBoard(ctx, tx, p.ParentCardID)
	if err != nil {
		return Patch{}, err
	}

	// Доска назначения уже заперта и проверена на запись в Apply: обе доски
	// берутся под замок до разбора операции, иначе две встречные постановки
	// заперли бы их в разном порядке.
	target := boardID
	if p.BoardID != "" {
		target = p.BoardID
	}

	if err := checkEpicStaysParent(ctx, tx, parentBoard, target); err != nil {
		return Patch{}, err
	}

	col, err := subtaskColumn(ctx, tx, target, p.ColumnID)
	if err != nil {
		return Patch{}, err
	}
	if err := enforceWIP(ctx, tx, col); err != nil {
		return Patch{}, err
	}

	card, err := insertCard(ctx, tx, orgID, actorID, target, col, p.Title, Placement{Place: "end"})
	if err != nil {
		return Patch{}, err
	}

	link := linkPayload{FromCard: p.ParentCardID, ToCard: card.ID, Kind: LinkSubtask}
	if err := checkSubtaskTree(ctx, tx, link.FromCard, link.ToCard); err != nil {
		return Patch{}, err
	}
	if err := insertLink(ctx, tx, orgID, actorID, link, parentBoard, target); err != nil {
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
		// Событие пишется доске самой карточки, а не доске операции — по той
		// же причине, что и у связывания. Архив здесь не отсекается: связь
		// снимают и с убранной карточки, а место событию всё равно на её
		// доске. Строки нет только если нет и карточки, а тогда каскад унёс
		// бы связь и удалять было бы нечего.
		var fromBoard string
		if err := tx.QueryRow(ctx,
			`select board_id from cards where id = $1`, p.FromCard).Scan(&fromBoard); err != nil {
			return Patch{}, err
		}
		payload := map[string]any{"kind": p.Kind, "fromCard": p.FromCard, "toCard": p.ToCard}
		if err := logEvent(ctx, tx, orgID, fromBoard, p.FromCard, actorID, "unlinked", nil, nil, payload); err != nil {
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
	// Новая или снятая часть меняет счёт поддерева у всех выше родителя,
	// а эпик — у всех ниже части (этап 33.3): привязали фичу к эпику —
	// метка эпика нужна и её задачам.
	if p.Kind == LinkSubtask {
		above, err := parentCards(ctx, tx, boardID, p.FromCard)
		if err != nil {
			return Patch{}, err
		}
		patch.Cards = append(patch.Cards, above...)
		below, err := childCards(ctx, tx, boardID, p.ToCard)
		if err != nil {
			return Patch{}, err
		}
		patch.Cards = append(patch.Cards, below...)
	}
	return patch, nil
}

// childCards — потомки карточки на этой доске, без неё самой, с пределом
// глубины дерева.
func childCards(ctx context.Context, tx pgx.Tx, boardID, cardID string) ([]Card, error) {
	rows, err := tx.Query(ctx, `
		with recursive down(card, depth) as (
			select to_card, 1 from card_links where from_card = $1 and kind = 'subtask'
			union all
			select l.to_card, d.depth + 1
			  from down d join card_links l on l.from_card = d.card and l.kind = 'subtask'
			 where d.depth < $2
		)
		select distinct card from down`, cardID, MaxSubtaskDepth)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var out []Card
	for _, id := range ids {
		c, err := readCard(ctx, tx, boardID, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// readCard читает карточку доски вместе с вычисляемым прогрессом и
// блокировкой — тем же составом, что отдаёт снимок доски.
// progressOf — доля разбиения одной карточки, ровно та же, что кладёт
// в снимок enrich.
//
// Считается тем же способом намеренно: раньше здесь стоял свой запрос,
// который всегда считал по штукам, а снимок при оценённых подзадачах
// считает по весу. Отметка одной части меняла не только долю,
// но и шкалу — «3 из 8» превращалось в «2 из 5», и полоса прыгала
// назад на глазах у того, кто только что отметил работу сделанной.
func progressOf(ctx context.Context, tx pgx.Tx, cardID string) (*Progress, error) {
	var total, done, unestimated int
	var weight, weightDone float64
	err := tx.QueryRow(ctx, `
		select count(*),
		       count(*) filter (where `+cardDone+`),
		       count(*) filter (where c.estimate is null),
		       coalesce(sum(c.estimate), 0),
		       coalesce(sum(c.estimate) filter (where `+cardDone+`), 0)
		  from card_links l
		  join cards c on c.id = l.to_card and c.archived_at is null
		 where l.from_card = $1 and l.kind = 'subtask'`, cardID).
		Scan(&total, &done, &unestimated, &weight, &weightDone)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, nil
	}
	// Вес берётся, только если оценены все подзадачи: иначе неоценённая
	// работа молча исчезла бы из знаменателя.
	if unestimated == 0 && weight > 0 {
		return &Progress{Done: weightDone, Total: weight, ByWeight: true}, nil
	}
	return &Progress{Done: float64(done), Total: float64(total)}, nil
}

func readCard(ctx context.Context, tx pgx.Tx, boardID, cardID string) (Card, error) {
	c, err := scanCard(tx.QueryRow(ctx, `
		select `+cardFields+` from cards
		 where id = $1 and board_id = $2 and archived_at is null`, cardID, boardID))
	if err != nil {
		return Card{}, err
	}
	return c, completeCard(ctx, tx, &c)
}

// completeCard дочитывает то, что живёт не в строке карточки: прогресс
// подзадач, поддерево, эпик, доли команд и блокировку.
//
// Всякая карточка, уходящая в патч, проходит через него. Клиент заменяет
// карточку тем, что пришло, и перенос с правкой, читавшие одну строку,
// стирали на экране блок подзадач, блокировку и метку эпика до
// перезагрузки страницы (замечено владельцем 25.09.2026).
func completeCard(ctx context.Context, tx pgx.Tx, c *Card) error {
	cardID := c.ID
	p, err := progressOf(ctx, tx, cardID)
	if err != nil {
		return err
	}
	c.Progress = p
	if c.Subtree, err = subtreeOf(ctx, tx, cardID); err != nil {
		return err
	}
	epic, err := epics(ctx, tx, nil, &cardID)
	if err != nil {
		return err
	}
	c.Epic = epic[cardID]
	shares, err := teams(ctx, tx, nil, &cardID)
	if err != nil {
		return err
	}
	c.Teams = shares[cardID]

	var b Block
	err = tx.QueryRow(ctx, `
		select id, reason, blocked_at, blocking_card, blocked_until from card_blocks
		 where card_id = $1 and unblocked_at is null`, cardID).
		Scan(&b.ID, &b.Reason, &b.BlockedAt, &b.BlockingCard, &b.Until)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	default:
		c.Blocked = &b
	}
	return nil
}

// setBlockUntil меняет срок открытой блокировки.
//
// Отдельная операция, а не повторный BLOCK_CARD: повторная блокировка
// отказывает конфликтом, и правильно — вторая причина дополняет первую,
// а не открывает новый интервал. Срок же во время блокировки меняют:
// поставку перенесли. Причину операция не трогает, пустой срок делает
// блокировку бессрочной.
func setBlockUntil(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p blockPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор SET_BLOCK_UNTIL: %v", err)
	}
	until, err := parseUntil(p.Until)
	if err != nil {
		return Patch{}, err
	}
	tag, err := tx.Exec(ctx, `
		update card_blocks set blocked_until = $3
		 where card_id = $1 and unblocked_at is null
		   and exists (select 1 from cards where id = $1 and board_id = $2)`,
		p.CardID, boardID, until)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		return Patch{}, conflictf("", "карточка не заблокирована — срок ставят вместе с блокировкой")
	}
	payload := map[string]any{"until": nil}
	if until != nil {
		payload["until"] = until.UTC()
	}
	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "block_until", nil, nil, payload); err != nil {
		return Patch{}, err
	}
	c, err := readCard(ctx, tx, boardID, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	return Patch{Cards: []Card{c}}, nil
}

// setBlockReason меняет причину открытой блокировки.
//
// Отдельной операцией, как и срок: прежде опечатку или устаревшую
// причину исправляли снятием и новой блокировкой, и время в блоке
// разрывалось надвое — метрики видели два коротких простоя вместо
// одного долгого (замечено владельцем 25.09.2026). Интервал остаётся
// тем же, меняется только текст.
func setBlockReason(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p blockPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор SET_BLOCK_REASON: %v", err)
	}
	p.Reason = strings.TrimSpace(p.Reason)
	if p.Reason == "" {
		return Patch{}, badRequestf("у блокировки должна быть причина")
	}
	tag, err := tx.Exec(ctx, `
		update card_blocks set reason = $3
		 where card_id = $1 and unblocked_at is null
		   and exists (select 1 from cards where id = $1 and board_id = $2)`,
		p.CardID, boardID, p.Reason)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		return Patch{}, conflictf("", "карточка не заблокирована — причину ставят вместе с блокировкой")
	}
	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "block_reason", nil, nil,
		map[string]any{"reason": p.Reason}); err != nil {
		return Patch{}, err
	}
	c, err := readCard(ctx, tx, boardID, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	return withParent(ctx, tx, boardID, c)
}

// --- отметка «сделано» ---

type setCardDonePayload struct {
	CardID string `json:"cardId"`
	// Отмечаем или снимаем отметку. Указывается явно, а не выводится
	// из текущего состояния: два человека, нажавшие одновременно, иначе
	// переключили бы отметку дважды и вернули её туда, откуда начали, —
	// а сказано было одно и то же.
	Done *bool `json:"done"`
}

// setCardDone отмечает работу сделанной, не двигая её по доске.
//
// Это не подмена потока: цикл и пропускная способность считаются по
// точке финиша, и отметка в них не входит. Она нужна разбиению —
// у подзадач вида «согласовать с юристами» переезда по колонкам нет,
// а вопрос «эта часть уже сделана» есть.
func setCardDone(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage,
) (Patch, error) {
	var p setCardDonePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор SET_CARD_DONE: %v", err)
	}
	if p.Done == nil {
		return Patch{}, badRequestf("не сказано, отметить или снять отметку")
	}

	// coalesce, а не now() безусловно: повторная отметка не должна
	// сдвигать момент — иначе «отмечена третьего дня» превращалось бы
	// в «отмечена только что» от одного лишнего нажатия.
	c, err := scanCard(tx.QueryRow(ctx, `
		update cards
		   set done_at    = case when $3::bool then coalesce(done_at, now()) end,
		       version    = version + 1,
		       updated_at = now()
		 where id = $1 and board_id = $2 and archived_at is null
		 returning `+cardFields,
		p.CardID, boardID, *p.Done))
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка не найдена или убрана с доски")
	}
	if err != nil {
		return Patch{}, err
	}

	kind := "undone"
	if *p.Done {
		kind = "done"
	}
	if err := logEvent(ctx, tx, orgID, boardID, c.ID, actorID, kind, nil, nil, nil); err != nil {
		return Patch{}, err
	}
	var released []Card
	if *p.Done {
		if released, err = releaseHeldBy(ctx, tx, orgID, actorID, boardID, c.ID); err != nil {
			return Patch{}, err
		}
	}
	if err := completeCard(ctx, tx, &c); err != nil {
		return Patch{}, err
	}

	// Родитель приезжает вместе с ней: доля разбиения у него только что
	// изменилась, а узнаёт он об этом ниоткуда — связь не его поле.
	patch, err := withParent(ctx, tx, boardID, c)
	patch.Cards = appendNew(patch.Cards, released)
	return patch, err
}

// appendNew дописывает карточки, которых в списке ещё нет. Держащая —
// часто подзадача, а ждущая — её родитель: он приезжает и родителем,
// и освобождённой, и дважды одна карточка в патче ни к чему.
func appendNew(cards, more []Card) []Card {
	seen := make(map[string]bool, len(cards))
	for _, c := range cards {
		seen[c.ID] = true
	}
	for _, c := range more {
		if !seen[c.ID] {
			cards = append(cards, c)
			seen[c.ID] = true
		}
	}
	return cards
}

// releaseHeldBy снимает блокировки, которые держала эта карточка, —
// в момент, когда её сделали.
//
// Блокировка с держащей карточкой означает «ждём её». Сделанную ждать
// нечего, а карточка оставалась «Заблокирована: ждёт задачу 2» и после
// того, как задачу 2 сделали, пока кто-нибудь не снимал блокировку руками
// (замечено владельцем 25.09.2026). Снятие пишется обычным «unblocked»
// от имени того, кто сделал держащую, — его действие её и сняло, — с
// пометкой, какая карточка отпустила.
//
// Возвращает освобождённые карточки этой доски — для патча; на чужих
// досках их перечитают по событию.
func releaseHeldBy(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID, cardID string) ([]Card, error) {
	rows, err := tx.Query(ctx, `
		update card_blocks b
		   set unblocked_at = now(), unblocked_by = $2
		  from cards c
		 where b.blocking_card = $1 and b.unblocked_at is null
		   and c.id = b.card_id and c.archived_at is null
		returning b.card_id, c.board_id`, cardID, actorID)
	if err != nil {
		return nil, err
	}
	type held struct{ card, board string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (held, error) {
		var h held
		return h, r.Scan(&h.card, &h.board)
	})
	if err != nil {
		return nil, err
	}
	var out []Card
	for _, h := range list {
		if err := logEvent(ctx, tx, orgID, h.board, h.card, actorID, "unblocked", nil, nil,
			map[string]any{"releasedBy": cardID}); err != nil {
			return nil, err
		}
		if h.board == boardID {
			c, err := readCard(ctx, tx, boardID, h.card)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// withParent добавляет к патчу предков этой карточки, если она чья-то
// часть. Всё, что меняет готовность или блокировку части, меняет и долю
// родителя, и счёт поддерева у всех, кто выше (этап 32.3), — а сами они
// об этом не узнают ниоткуда: связь не их поле. Без этого полоса
// разбиения оставалась прежней до перезагрузки страницы.
//
// Предок на чужой доске в патч не попадает: там его перечитают
// со снимком, как и всё остальное чужое.
func withParent(ctx context.Context, tx pgx.Tx, boardID string, c Card) (Patch, error) {
	parents, err := parentCards(ctx, tx, boardID, c.ID)
	if err != nil {
		return Patch{}, err
	}
	return Patch{Cards: append([]Card{c}, parents...)}, nil
}

// parentCards — предки этой карточки, живущие на этой же доске, от
// ближнего к корню. Подъём ограничен MaxSubtaskDepth, как и само дерево.
// Предок на чужой доске пропускается, но подъём идёт дальше: над ним
// может снова стоять карточка этой доски.
func parentCards(ctx context.Context, tx pgx.Tx, boardID, cardID string) ([]Card, error) {
	rows, err := tx.Query(ctx, `
		with recursive up(card, depth) as (
			select from_card, 1 from card_links where to_card = $1 and kind = 'subtask'
			union all
			select l.from_card, u.depth + 1
			  from up u join card_links l on l.to_card = u.card and l.kind = 'subtask'
			 where u.depth < $2
		)
		select card from up order by depth`, cardID, MaxSubtaskDepth)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var out []Card
	for _, id := range ids {
		parent, err := readCard(ctx, tx, boardID, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, parent)
	}
	return out, nil
}

// --- блокировки ---

type blockPayload struct {
	CardID string `json:"cardId"`
	Reason string `json:"reason"`
	// Работа, которая держит. Чаще всего — собственная часть этой же
	// задачи: разбили работу, одна часть оказалась поперёк остальных,
	// и родитель стоит из-за неё. Пусто — держит что-то, чему карточки
	// нет вовсе, и об этом сказано только словами причины.
	//
	// Причина остаётся обязательной и при ссылке: ссылка говорит, кого
	// ждём, а чего именно от него ждут — только слова.
	BlockingCard string `json:"blockingCard"`
	// Когда снимется сама, ISO 8601 с зоной. Пусто — пока не снимут.
	Until *string `json:"until"`
}

// parseUntil разбирает срок блокировки. Срок в прошлом — отказ,
// а не молчаливое снятие: он объясняет, что делать, и заодно ловит
// опечатку в годе.
func parseUntil(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, badRequestf("срок блокировки не разобран: нужен момент со временем и зоной, например 2026-09-24T18:00:00+03:00")
	}
	if !t.After(time.Now()) {
		return nil, badRequestf("срок блокировки должен быть позже текущего времени; чтобы снять блокировку сейчас, есть «Снять блокировку»")
	}
	return &t, nil
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
	until, err := parseUntil(p.Until)
	if err != nil {
		return Patch{}, err
	}
	if p.BlockingCard != "" {
		if p.BlockingCard == p.CardID {
			return Patch{}, badRequestf("карточка не может держать саму себя")
		}
		// Держащая карточка может лежать на чужой доске — так и бывает,
		// когда часть работы поставлена соседям. Проверяется только её
		// существование; видимость обеспечивают политики базы, и
		// недоступная сюда просто не попадёт.
		if _, err := cardBoard(ctx, tx, p.BlockingCard); err != nil {
			return Patch{}, err
		}
	}

	blocking := any(nil)
	if p.BlockingCard != "" {
		blocking = p.BlockingCard
	}
	tag, err := tx.Exec(ctx, `
		insert into card_blocks (org_id, card_id, reason, blocked_by, blocking_card, blocked_until)
		select $1, $2, $3, $4, $6, $7
		 where exists (select 1 from cards
		                where id = $2 and board_id = $5 and archived_at is null)
		   and not exists (select 1 from card_blocks
		                    where card_id = $2 and unblocked_at is null)`,
		orgID, p.CardID, p.Reason, actorID, boardID, blocking, until)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		// Отказ должен говорить, что делать, а «уже заблокирована»
		// и «недоступна» ведут в разные стороны: первую разблокируют,
		// вторую ищут не здесь. Различаем чтением, а не догадкой.
		var reason string
		err := tx.QueryRow(ctx, `
			select reason from card_blocks
			 where card_id = $1 and unblocked_at is null`, p.CardID).Scan(&reason)
		if errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, conflictf("", "карточка не найдена или убрана с доски")
		}
		if err != nil {
			return Patch{}, err
		}
		return Patch{}, conflictf("",
			"карточка уже заблокирована: %s. Снимите ту блокировку, прежде чем ставить эту", reason)
	}

	payload := map[string]any{"reason": p.Reason}
	if p.BlockingCard != "" {
		payload["blockingCard"] = p.BlockingCard
	}
	if until != nil {
		payload["until"] = until.UTC()
	}
	eventID, err := logEventID(ctx, tx, orgID, boardID, p.CardID, actorID, "blocked", nil, nil, payload)
	if err != nil {
		return Patch{}, err
	}
	// Твою работу остановили — это надо знать тому, кто её делает.
	working, err := assigneesOf(ctx, tx, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	if err := notify(ctx, tx, orgID, boardID, p.CardID, actorID, ReasonBlocked,
		eventSource(eventID), working); err != nil {
		return Patch{}, err
	}
	c, err := readCard(ctx, tx, boardID, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	// Блокировка части останавливает и предков (этап 17 на любой глубине,
	// 32.3): они узнают об этом только с патчем.
	return withParent(ctx, tx, boardID, c)
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
	return withParent(ctx, tx, boardID, c)
}
