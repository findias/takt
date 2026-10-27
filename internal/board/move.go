package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Перенос карточки на другую доску (ROADMAP 40).
//
// Переносится, а не копируется: история, обсуждение и файлы остаются
// одной карточкой, и ссылка на неё по идентификатору продолжает вести
// туда же. Номер выдаёт доска назначения — ключ доски в номере и есть
// ответ на вопрос «чья это работа», — а прежний остаётся в событии
// переноса, и поиск находит карточку и по нему: на старый номер
// ссылаются переписка и внешние системы.

// ErrMoveTargetMissing — доски назначения нет: убрана в архив, удалена
// или не видна спрашивающему. Отдельно от ErrNotFound: «карточка
// не найдена» про доску назначения отправило бы искать карточку.
var ErrMoveTargetMissing = errors.New("доски, куда переносить, нет: она убрана в архив или вам не видна")

type moveToBoardPayload struct {
	CardID  string `json:"cardId"`
	BoardID string `json:"boardId"`
	// Колонка доски назначения. Пусто — первая, как у подзадачи
	// на доске соседей.
	ToColumnID string `json:"toColumnId"`
}

// MovePreview — что случится с карточкой при переносе, до самого
// переноса. Подтверждение называет снимаемое поимённо: «метки снимутся»
// без имён отвечают не читая.
type MovePreview struct {
	// Колонки доски назначения — выбрать, куда лечь.
	Columns []Column `json:"columns"`
	// Метки, которые на доске назначения не действуют и будут сняты.
	DroppedLabels []string `json:"droppedLabels"`
	// Исполнители, которые доски назначения не видят. Пока список
	// не пуст, перенос откажет.
	Unseen []string `json:"unseen"`
	// Открытая итерация, из которой карточка выйдет. Пусто — ни в какой.
	Iteration string `json:"iteration"`
	// Сколько пунктов (подзадач на этой же доске) уедет вместе с ней.
	Items int `json:"items"`
	// Доска другого уровня: портфель и доска команды. Перенос откажет.
	OtherLevel bool `json:"otherLevel"`
}

// CardHome говорит, на какой доске карточка лежит сейчас.
//
// Ссылка на карточку несёт доску (`/board/{доска}/card/{карточка}`),
// и после переноса (ROADMAP 40) старая ссылка вела на доску, где
// карточки уже нет: панель молча не открывалась. Клиент спрашивает
// здесь и переходит туда, где карточка теперь. Что видно, решает
// политика: карточка на доске, которой спрашивающему не видно,
// отвечает «не найдена», как и всё недоступное.
func (s *Service) CardHome(ctx context.Context, orgID, userID, cardID string) (string, error) {
	var boardID string
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select c.board_id::text from cards c
			  join boards b on b.id = c.board_id
			 where c.id = $1 and c.archived_at is null and b.archived_at is null`,
			cardID).Scan(&boardID)
	})
	// Нечитаемый идентификатор из адресной строки — та же «не найдена»,
	// а не сбой сервера.
	var pgErr *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pgErr) && pgErr.Code == "22P02") {
		return "", ErrNotFound
	}
	return boardID, err
}

// PreviewMove отвечает, что произойдёт при переносе карточки на доску
// target, ничего не меняя.
func (s *Service) PreviewMove(ctx context.Context, orgID, userID, boardID, cardID, target string) (MovePreview, error) {
	out := MovePreview{Columns: []Column{}, DroppedLabels: []string{}, Unseen: []string{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := liveCardOn(ctx, tx, boardID, cardID); err != nil {
			return err
		}
		var ok bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from boards where id = $1 and archived_at is null)`,
			target).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrMoveTargetMissing
		}
		rows, err := tx.Query(ctx,
			`select `+columnFields+` from board_columns where board_id = $1 order by position`, target)
		if err != nil {
			return err
		}
		if out.Columns, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Column, error) {
			return scanColumn(r)
		}); err != nil {
			return err
		}
		moving, err := movingCards(ctx, tx, boardID, cardID)
		if err != nil {
			return err
		}
		out.Items = len(moving) - 1
		if out.OtherLevel, err = otherLevel(ctx, tx, boardID, target); err != nil {
			return err
		}
		if out.Unseen, err = unseenAssignees(ctx, tx, moving, target); err != nil {
			return err
		}
		if out.DroppedLabels, err = stringColumn(ctx, tx, `
			select distinct l.name from card_labels cl
			  join labels l on l.id = cl.label_id
			 where cl.card_id = $1
			   and not label_path_prefix(label_path(l.team_id, l.board_id), label_path(null, $2))
			 order by 1`, cardID, target); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			select i.name from iteration_cards ic
			  join iterations i on i.id = ic.iteration_id
			 where ic.card_id = $1 and ic.removed_at is null and i.closed_at is null`,
			cardID).Scan(&out.Iteration)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}

func moveCardToBoard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw json.RawMessage) (Patch, error) {
	var p moveToBoardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор MOVE_CARD_TO_BOARD: %v", err)
	}
	if p.CardID == "" || p.BoardID == "" {
		return Patch{}, badRequestf("не сказано, какую карточку и на какую доску переносить")
	}
	if p.BoardID == boardID {
		return Patch{}, badRequestf("карточка уже на этой доске — чтобы переставить её в другую колонку, перетащите её")
	}
	// Доска назначения уже заперта и проверена на запись в Apply
	// (targetBoard): иначе перенос «к соседям» и встречный «от них»
	// заперли бы пару досок в разном порядке.
	target := p.BoardID

	var oldNumber, fromColumnID string
	var wasFinished bool
	err := tx.QueryRow(ctx, `
		select number, column_id, finished_at is not null from cards
		 where id = $1 and board_id = $2 and archived_at is null`,
		p.CardID, boardID).Scan(&oldNumber, &fromColumnID, &wasFinished)
	if errors.Is(err, pgx.ErrNoRows) {
		return Patch{}, conflictf("", "карточка уже удалена, убрана в архив или перенесена на другую доску")
	}
	if err != nil {
		return Patch{}, err
	}
	// Эпик на доске команды — задача без эпика, задача на портфеле —
	// эпик без смысла: перенос между уровнями меняет то, чем карточка
	// является, а не только где лежит.
	if other, err := otherLevel(ctx, tx, boardID, target); err != nil {
		return Patch{}, err
	} else if other {
		return Patch{}, conflictf("", "портфель и доска команды — доски разного уровня: эпик не переносится на доску команды, а задача — на портфель. "+
			"Чтобы поставить работу эпика команде, заведите ему подзадачу на её доске")
	}

	col, err := subtaskColumn(ctx, tx, target, p.ToColumnID)
	if err != nil {
		return Patch{}, err
	}
	if err := enforceWIP(ctx, tx, col); err != nil {
		return Patch{}, err
	}
	from, err := loadColumn(ctx, tx, boardID, fromColumnID)
	if err != nil {
		return Patch{}, err
	}

	moving, err := movingCards(ctx, tx, boardID, p.CardID)
	if err != nil {
		return Patch{}, err
	}
	// Исполнитель, который доски не видит, не найдёт свою карточку
	// (0076). Снимать его молча нельзя — назначение решал не переносящий;
	// отказ называет, кого это касается и что сделать.
	unseen, err := unseenAssignees(ctx, tx, moving, target)
	if err != nil {
		return Patch{}, err
	}
	var targetName string
	if err := tx.QueryRow(ctx, `select name from boards where id = $1`, target).Scan(&targetName); err != nil {
		return Patch{}, err
	}
	var sourceName string
	if err := tx.QueryRow(ctx, `select name from boards where id = $1`, boardID).Scan(&sourceName); err != nil {
		return Patch{}, err
	}
	if len(unseen) > 0 {
		return Patch{}, conflictf("", "доску «%s» не видят исполнители: %s — снимите их с карточки или откройте им доску и повторите перенос",
			targetName, strings.Join(unseen, ", "))
	}

	// Метки чужой области на новой доске не действуют: висящая, она
	// показывалась бы меткой, которую там не повесить и не найти
	// в отборе. Снимаются и называются в событии.
	dropped, err := stringColumn(ctx, tx, `
		with gone as (
			delete from card_labels cl
			 using labels l
			 where l.id = cl.label_id and cl.card_id = any($1)
			   and not label_path_prefix(label_path(l.team_id, l.board_id), label_path(null, $2))
			returning cl.card_id, l.name
		)
		select distinct name from gone order by 1`, moving, target)
	if err != nil {
		return Patch{}, err
	}
	// Итерации — доски, и чужая итерация карточке ни к чему. Участие
	// закрывается моментом, а не стирается: отчёт закрытой итерации
	// смотрит на состав в момент закрытия и потому не меняется.
	left, err := stringColumn(ctx, tx, `
		with gone as (
			update iteration_cards set removed_at = now(), removed_by = $2
			 where card_id = any($1) and removed_at is null
			returning iteration_id
		)
		select distinct i.name from gone join iterations i on i.id = gone.iteration_id order by 1`,
		moving, actorID)
	if err != nil {
		return Patch{}, err
	}

	var card Card
	for i, id := range moving {
		pos, err := nextPosition(ctx, tx, col.ID, Placement{Place: PlaceEnd}, id)
		if err != nil {
			return Patch{}, err
		}
		var number string
		if err := tx.QueryRow(ctx, `
			update boards set card_seq = card_seq + 1
			 where id = $1
			returning key || '-' || card_seq`, target).Scan(&number); err != nil {
			return Patch{}, err
		}
		var was string
		if i == 0 {
			// Сама карточка ложится в выбранную колонку со всеми
			// отметками потока, как при переносе внутри доски: вход
			// в колонку — сейчас, начало работы — один раз и навсегда,
			// завершение — по смыслу новой колонки.
			was = oldNumber
			card, err = scanCard(tx.QueryRow(ctx, `
				update cards
				   set board_id = $2, number = $3, column_id = $4, position = $5,
				       column_entered_at = now(),
				       started_at  = case when $6::bool and started_at is null
				                          then now() else started_at end,
				       finished_at = case when $7::bool
				                          then coalesce(finished_at, now()) else null end,
				       outcome     = case when $7::bool then 'done'
				                          when outcome = 'done' then null
				                          else outcome end,
				       version = version + 1, updated_at = now()
				 where id = $1
				 returning `+cardFields,
				id, target, number, col.ID, pos, col.IsStartedPoint, col.IsFinishedPoint))
			if err != nil {
				return Patch{}, err
			}
		} else {
			// Пункт колонкой не живёт (IsItem): его колонка в базе ничего
			// не значит, и отметки потока у него не трогаются — иначе
			// сделанный пункт, перенесённый в «Новые», снова стал бы
			// несделанным.
			if err := tx.QueryRow(ctx, `select number from cards where id = $1`, id).Scan(&was); err != nil {
				return Patch{}, err
			}
			if _, err := tx.Exec(ctx, `
				update cards
				   set board_id = $2, number = $3, column_id = $4, position = $5,
				       version = version + 1, updated_at = now()
				 where id = $1`,
				id, target, number, col.ID, pos); err != nil {
				return Patch{}, err
			}
		}
		// Обсуждение, файлы и перенесённая история читаются по доске
		// карточки: оставленные на прежней, они пропали бы у тех, кто
		// видит только новую. Журнал событий остаётся где был — это
		// история той доски, и «откуда пришла» рассказывает событие
		// переноса.
		for _, table := range []string{"card_comments", "card_attachments", "card_source_history"} {
			// #sql-склейка: имя таблицы из постоянного списка выше, ввода в нём нет
			if _, err := tx.Exec(ctx, `update `+table+` set board_id = $2 where card_id = $1`,
				id, target); err != nil {
				return Patch{}, err
			}
		}
		var leftColumn *string
		if i == 0 {
			leftColumn = &fromColumnID
		}
		if err := logEvent(ctx, tx, orgID, boardID, id, actorID, "moved_out", leftColumn, nil,
			map[string]any{
				"to":     map[string]any{"id": target, "name": targetName},
				"number": number,
			}); err != nil {
			return Patch{}, err
		}
		in := map[string]any{
			"from":       map[string]any{"id": boardID, "name": sourceName},
			"fromNumber": was,
		}
		if i == 0 {
			in["to"] = columnFact(col)
			in["fromColumn"] = columnFact(from)
			in["droppedLabels"] = dropped
			in["iterations"] = left
		} else {
			in["withParent"] = p.CardID
		}
		if err := logEvent(ctx, tx, orgID, target, id, actorID, "moved_in", nil, &col.ID, in); err != nil {
			return Patch{}, err
		}
	}

	// Сделанная карточка отпускает тех, кто её ждал, — как при переносе
	// за финиш на своей доске.
	var released []Card
	if col.IsFinishedPoint && !wasFinished {
		if released, err = releaseHeldBy(ctx, tx, orgID, actorID, boardID, card.ID); err != nil {
			return Patch{}, err
		}
	}
	// Своя доска теряет карточку и её пункты; родитель на ней, если
	// карточка была его пунктом, — меняет долю разбиения: теперь это
	// работа соседей. Доска назначения патча не получает — у неё
	// пропуск в версиях, и она перечитает снимок.
	parents, err := parentCards(ctx, tx, boardID, card.ID)
	if err != nil {
		return Patch{}, err
	}
	return Patch{
		RemovedCardIDs: moving,
		Cards:          appendNew(parents, released),
	}, nil
}

// movingCards — карточка и её пункты: подзадачи на этой же доске, вниз
// по дереву, пока оно не уходит на другую доску. Пункт — строка внутри
// родителя, а не отдельная работа, и оставленный, он стал бы карточкой
// в колонке чужого ему потока. Части на других досках остаются, где
// лежат: связь с родителем живёт через доски.
//
// Убранные в архив пункты едут тоже: оставленные, они вернулись бы
// из архива на доску, где их родителя уже нет.
func movingCards(ctx context.Context, tx pgx.Tx, boardID, cardID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		with recursive items(card, depth) as (
			select $1::uuid, 0
			union all
			select l.to_card, i.depth + 1
			  from items i
			  join card_links l on l.from_card = i.card and l.kind = 'subtask'
			  join cards c on c.id = l.to_card and c.board_id = $2
			 where i.depth < $3
		)
		select card from items order by depth, card`, cardID, boardID, MaxSubtaskDepth)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// unseenAssignees — имена исполнителей этих карточек, которые доски
// target не видят. Спрашивается сама политика (app_sees_board_as,
// 0076), а не её копия в коде.
func unseenAssignees(ctx context.Context, tx pgx.Tx, cards []string, target string) ([]string, error) {
	return stringColumn(ctx, tx, `
		select distinct u.name from card_assignees a
		  join users u on u.id = a.user_id
		 where a.card_id = any($1) and not app_sees_board_as(a.user_id, $2)
		 order by 1`, cards, target)
}

// otherLevel — доски разного уровня: портфель и доска команды.
func otherLevel(ctx context.Context, tx pgx.Tx, a, b string) (bool, error) {
	var differ bool
	err := tx.QueryRow(ctx, `
		select (select level from boards where id = $1) is distinct from
		       (select level from boards where id = $2)`, a, b).Scan(&differ)
	return differ, err
}

// liveCardOn проверяет, что карточка лежит на этой доске и не убрана.
func liveCardOn(ctx context.Context, tx pgx.Tx, boardID, cardID string) error {
	var ok bool
	if err := tx.QueryRow(ctx, `
		select exists (select 1 from cards
		                where id = $1 and board_id = $2 and archived_at is null)`,
		cardID, boardID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// stringColumn читает столбец строк — имён меток, людей или итераций.
func stringColumn(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if names == nil {
		names = []string{}
	}
	return names, err
}
