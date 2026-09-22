package board

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/rank"
	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/webhook"
)

// ImportTarget — куда переносим: в новую доску (имя) или в существующую.
type ImportTarget struct {
	BoardID      string
	NewBoardName string
	// ColumnMap — куда ложатся значения колонки файла на существующей
	// доске: значение (без учёта регистра) → колонка доски. Значение
	// без записи ищется по названию, а не найдено — заводится новой
	// колонкой, как и раньше. «In Review» из Jira и наша «В работе» —
	// одно и то же, но угадать это по названию нельзя: решает человек.
	ColumnMap map[string]string
}

// ImportReport — что случилось или случится. Один и тот же ответ
// у предпросмотра и у переноса: человек видит заранее ровно то, что
// потом получит.
type ImportReport struct {
	Applied   bool   `json:"applied"`
	BoardID   string `json:"boardId,omitempty"`
	BoardName string `json:"boardName"`
	NewBoard  bool   `json:"newBoard"`
	Rows      int    `json:"rows"`
	Created   int    `json:"created"`
	// Что переезжает сверх карточек — из пакета переноса: подзадачи,
	// связи, реплики обсуждения.
	Parts    int `json:"parts"`
	Links    int `json:"links"`
	Comments int `json:"comments"`
	// Уже перенесённые прежним прогоном — по внешнему ключу.
	Skipped    []ImportSkip   `json:"skipped"`
	NewColumns []ImportColumn `json:"newColumns"`
	NewLabels  []string       `json:"newLabels"`
	// ColumnValues — значения колонки файла и куда каждое ляжет.
	ColumnValues []ImportValue `json:"columnValues"`
	// BoardColumns — колонки существующей доски, из которых выбирают,
	// куда положить значение. У новой доски пусто: там выбирать не из чего.
	BoardColumns []ImportColumnRef `json:"boardColumns"`
	// Метки, найденные только в архиве: вешать убранное молча нельзя.
	ArchivedLabels []string `json:"archivedLabels"`
	// Lost — что источник знает, а мы не переносим.
	Lost          []string              `json:"lost"`
	MissingPeople []MissingPerson       `json:"missingPeople"`
	Problems      []importer.Problem    `json:"problems"`
	Dates         []importer.DateFormat `json:"dates"`
}

// ImportSkip — строка, которая уже переехала раньше.
type ImportSkip struct {
	Row   int    `json:"row"`
	Title string `json:"title"`
	// Номер и доска прежней карточки; пусто — она на доске, которую
	// спрашивающий не видит.
	Number string `json:"number,omitempty"`
	Board  string `json:"board,omitempty"`
}

// ImportValue — значение колонки файла и колонка доски, куда оно ляжет.
type ImportValue struct {
	Value  string `json:"value"`
	Cards  int    `json:"cards"`
	Column string `json:"column"`
	// ColumnID — колонка существующей доски; у новой колонки пусто:
	// до переноса её нет, и ссылаться не на что.
	ColumnID string `json:"columnId,omitempty"`
	New      bool   `json:"new"`
}

// ImportColumnRef — колонка доски в списке выбора.
type ImportColumnRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ImportColumn — колонка, которую импорт заведёт.
type ImportColumn struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// MissingPerson — почта из файла, которой нет в организации, или имя
// человека, чью почту источник не отдал (Trello — не администратору).
type MissingPerson struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	Cards int    `json:"cards"`
}

// Import переносит разобранный файл на доску.
//
// Предпросмотр и перенос — одна и та же работа, и разница между ними
// только в последней строке: предпросмотр откатывает транзакцию.
// Отдельный «расчёт того, что будет» разошёлся бы с переносом
// на первом же крае — человека, которого нет, метки в архиве,
// карточки, перенесённой раньше на чужую доску.
//
// Человек сопоставляется по почте, и ненайденный не выдумывается:
// карточка едет без исполнителя, а почта — в отчёт. Завести человека
// молча значило бы подсунуть организации сотрудника, которого в ней
// нет. Назначение через импорт не шлёт уведомлений: двести известий
// «вас назначили» о задачах, которые человек вёл и вчера, — шум.
//
// История: даты переносятся, переходы — нет. Чужая разметка колонок
// не совпадает с нашей, и выданное за наши события дало бы метрику,
// которой нельзя верить.
func (s *Service) Import(
	ctx context.Context, orgID, actorID string, target ImportTarget, plan importer.Plan, apply bool,
) (ImportReport, error) {
	rep := ImportReport{
		Rows: plan.Rows, Problems: plan.Problems, Dates: plan.Dates,
		Skipped: []ImportSkip{}, NewColumns: []ImportColumn{}, NewLabels: []string{},
		ArchivedLabels: []string{}, MissingPeople: []MissingPerson{},
		ColumnValues: []ImportValue{}, BoardColumns: []ImportColumnRef{},
		Lost: append([]string{}, plan.Lost...),
	}
	if rep.Problems == nil {
		rep.Problems = []importer.Problem{}
	}
	if rep.Dates == nil {
		rep.Dates = []importer.DateFormat{}
	}

	tx, err := s.db.BeginTenant(ctx, orgID, actorID)
	if err != nil {
		return rep, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Доска и её колонки. fresh — заведённые этим переносом.
	var columns []Column
	fresh := map[string]bool{}
	if target.BoardID != "" {
		err := tx.QueryRow(ctx, `
			select name from boards
			 where id = $1 and archived_at is null
			 for update`, target.BoardID).Scan(&rep.BoardName)
		if errors.Is(err, pgx.ErrNoRows) {
			return rep, s.explainMissingBoard(ctx, orgID, actorID, target.BoardID)
		}
		if err != nil {
			return rep, err
		}
		rep.BoardID = target.BoardID
		if columns, err = boardColumns(ctx, tx, target.BoardID); err != nil {
			return rep, err
		}
		for _, c := range columns {
			rep.BoardColumns = append(rep.BoardColumns, ImportColumnRef{ID: c.ID, Name: c.Name})
		}
		// Значение, отданное человеком в колонку доски, новой колонки
		// не заводит. Колонку проверяем здесь: её могли убрать, пока
		// человек смотрел на предпросмотр.
		var unmapped []string
		for _, v := range plan.Columns {
			id, ok := target.ColumnMap[strings.ToLower(strings.TrimSpace(v))]
			if !ok || id == "" {
				unmapped = append(unmapped, v)
				continue
			}
			if !slices.ContainsFunc(columns, func(c Column) bool { return c.ID == id }) {
				return rep, badRequestf("колонки, выбранной для «%s», на доске уже нет — выберите другую", v)
			}
		}
		added, err := addMissingColumns(ctx, tx, orgID, target.BoardID, columns, unmapped)
		if err != nil {
			return rep, err
		}
		for _, c := range added {
			rep.NewColumns = append(rep.NewColumns, ImportColumn{Name: c.Name, Kind: c.Kind})
			fresh[c.ID] = true
		}
		if columns, err = boardColumns(ctx, tx, target.BoardID); err != nil {
			return rep, err
		}
	} else {
		name := strings.TrimSpace(target.NewBoardName)
		if name == "" {
			return rep, badRequestf("у доски должно быть название")
		}
		wanted := newBoardColumns(ctx, plan.Columns, plan.ColumnKinds)
		var b Info
		if b, columns, err = createBoard(ctx, tx, orgID, name, "", wanted); err != nil {
			return rep, err
		}
		rep.BoardID, rep.BoardName, rep.NewBoard = b.ID, b.Name, true
		for _, c := range columns {
			rep.NewColumns = append(rep.NewColumns, ImportColumn{Name: c.Name, Kind: c.Kind})
			fresh[c.ID] = true
		}
	}

	byName := map[string]Column{}
	var first, finish, start *Column
	for i := range columns {
		c := &columns[i]
		if _, ok := byName[strings.ToLower(c.Name)]; !ok {
			byName[strings.ToLower(c.Name)] = *c
		}
		if first == nil {
			first = c
		}
		if c.IsFinishedPoint && finish == nil {
			finish = c
		}
		if c.IsStartedPoint && start == nil {
			start = c
		}
	}
	if first == nil {
		return rep, badRequestf("на доске нет ни одной колонки — заведите колонку и повторите")
	}
	byID := map[string]Column{}
	for _, c := range columns {
		byID[c.ID] = c
	}
	// columnFor — куда ляжет значение: выбор человека, затем название.
	columnFor := func(value string) (Column, bool) {
		key := strings.ToLower(strings.TrimSpace(value))
		if id := target.ColumnMap[key]; id != "" && !rep.NewBoard {
			if c, ok := byID[id]; ok {
				return c, true
			}
		}
		c, ok := byName[key]
		return c, ok
	}
	counts := map[string]int{}
	for _, c := range plan.Cards {
		counts[strings.ToLower(c.Column)]++
	}
	for _, v := range plan.Columns {
		iv := ImportValue{Value: v, Cards: counts[strings.ToLower(v)]}
		if c, ok := columnFor(v); ok {
			iv.Column, iv.New = c.Name, fresh[c.ID]
			if !iv.New {
				iv.ColumnID = c.ID
			}
		}
		rep.ColumnValues = append(rep.ColumnValues, iv)
	}
	// Карточка начата, если стоит в колонке старта или правее: колонки
	// упорядочены позицией, и сравнение позиций это и отвечает.
	started := func(c Column) bool {
		return start != nil && c.Position >= start.Position
	}

	// Уже перенесённое — по внешнему ключу. Видно только то, что
	// видит спрашивающий; двойник на закрытой доске отсеет индекс.
	keys := make([]string, len(plan.Cards))
	for i, c := range plan.Cards {
		keys[i] = c.ExternalID
	}
	already := map[string]ImportSkip{}
	// Идентификаторы уже перенесённого — чтобы новая подзадача нашла
	// родителя, переехавшего прошлым прогоном.
	alreadyID := map[string]string{}
	rows, err := tx.Query(ctx, `
		select c.external_id, c.id, c.number, b.name
		  from cards c join boards b on b.id = c.board_id
		 where c.external_source = $1 and c.external_id = any($2)`, plan.Source, keys)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var k, id string
		var sk ImportSkip
		if err := rows.Scan(&k, &id, &sk.Number, &sk.Board); err != nil {
			rows.Close()
			return rep, err
		}
		already[k] = sk
		alreadyID[k] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	// Люди — по почте, среди участников организации.
	emails := map[string]bool{}
	for _, c := range plan.Cards {
		for _, e := range c.Assignees {
			emails[e] = true
		}
		for _, cm := range c.Comments {
			if cm.AuthorEmail != "" {
				emails[cm.AuthorEmail] = true
			}
		}
	}
	people := map[string]string{}
	if len(emails) > 0 {
		list := make([]string, 0, len(emails))
		for e := range emails {
			list = append(list, e)
		}
		rows, err := tx.Query(ctx, `
			select lower(u.email), u.id from users u
			  join memberships m on m.user_id = u.id and m.org_id = $1
			 where lower(u.email) = any($2)`, orgID, list)
		if err != nil {
			return rep, err
		}
		for rows.Next() {
			var e, id string
			if err := rows.Scan(&e, &id); err != nil {
				rows.Close()
				return rep, err
			}
			people[e] = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return rep, err
		}
	}

	labels, err := importLabels(ctx, tx, orgID, rep.BoardID, plan.Cards, &rep)
	if err != nil {
		return rep, err
	}

	// Позиции — одним заходом на колонку: по запросу на карточку
	// перенос тысячи строк шёл бы тысячу обходов колонки.
	perColumn := map[string]int{}
	placeOf := make([]Column, len(plan.Cards))
	missing := map[string]int{}
	for i, c := range plan.Cards {
		if _, ok := already[c.ExternalID]; ok {
			continue
		}
		col := *first
		switch {
		case c.Column != "":
			if named, ok := columnFor(c.Column); ok {
				col = named
			}
		case c.Done != nil && finish != nil:
			// Колонки в файле нет, а дата завершения есть — карточка сделана.
			col = *finish
		}
		placeOf[i] = col
		perColumn[col.ID]++
	}
	positions := map[string][]string{}
	for colID, n := range perColumn {
		var last string
		err := tx.QueryRow(ctx, `
			select coalesce(max(position), '') from cards
			 where column_id = $1 and archived_at is null`, colID).Scan(&last)
		if err != nil {
			return rep, err
		}
		if positions[colID], err = rank.NBetween(last, "", n); err != nil {
			return rep, err
		}
	}

	var todo []importRow
	for i, c := range plan.Cards {
		if sk, ok := already[c.ExternalID]; ok {
			sk.Row, sk.Title = c.Row, c.Title
			rep.Skipped = append(rep.Skipped, sk)
			continue
		}
		col := placeOf[i]
		todo = append(todo, importRow{card: c, col: col, pos: positions[col.ID][0], started: started(col)})
		positions[col.ID] = positions[col.ID][1:]
	}
	ids, err := insertImported(ctx, tx, orgID, actorID, rep.BoardID, plan.Source, todo)
	if err != nil {
		return rep, err
	}

	var links, assignees, labelCards, labelIDs []string
	nameless := map[string]int{}
	for i, r := range todo {
		id := ids[i]
		if id == "" {
			// Двойник на доске, которой спрашивающий не видит: индекс
			// его нашёл, а мы — нет. Назвать доску нельзя, но сказать,
			// что строка уже переехала, — можно и нужно.
			rep.Skipped = append(rep.Skipped, ImportSkip{Row: r.card.Row, Title: r.card.Title})
			continue
		}
		rep.Created++
		for _, e := range r.card.Assignees {
			if uid, ok := people[e]; ok {
				links, assignees = append(links, id), append(assignees, uid)
			} else {
				missing[e]++
			}
		}
		for _, name := range r.card.Labels {
			if lid, ok := labels[strings.ToLower(name)]; ok {
				labelCards, labelIDs = append(labelCards, id), append(labelIDs, lid)
			}
		}
		for _, name := range r.card.Unmatched {
			nameless[name]++
		}
	}
	// Исполнители и метки — тоже одним запросом на вид. Назначение
	// через перенос уведомлений не шлёт (см. выше), поэтому здесь нет
	// ничего, кроме вставки.
	if len(links) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into card_assignees (org_id, card_id, user_id, added_by)
			select $1, c, u, $4 from unnest($2::uuid[], $3::uuid[]) as t(c, u)
			on conflict (card_id, user_id) do nothing`, orgID, links, assignees, actorID); err != nil {
			return rep, err
		}
	}
	if len(labelIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into card_labels (org_id, card_id, label_id, added_by)
			select $1, c, l, $4 from unnest($2::uuid[], $3::uuid[]) as t(c, l)
			on conflict (card_id, label_id) do nothing`, orgID, labelCards, labelIDs, actorID); err != nil {
			return rep, err
		}
	}
	if err := importRelations(ctx, tx, orgID, actorID, rep.BoardID, plan, todo, ids, alreadyID, people, &rep); err != nil {
		return rep, err
	}
	for e, n := range missing {
		rep.MissingPeople = append(rep.MissingPeople, MissingPerson{Email: e, Cards: n})
	}
	for name, n := range nameless {
		rep.MissingPeople = append(rep.MissingPeople, MissingPerson{Name: name, Cards: n})
	}
	sortMissing(rep.MissingPeople)

	if !apply {
		return rep, nil
	}
	var version int64
	if err := tx.QueryRow(ctx,
		`update boards set version = version + 1 where id = $1 returning version`,
		rep.BoardID).Scan(&version); err != nil {
		return rep, err
	}
	// Открытая доска перечитывает снимок: патча на тысячу карточек
	// нет, и пропуск версии как раз означает «перечитай».
	if err := realtime.Notify(ctx, tx, realtime.Change{
		BoardID: rep.BoardID, Version: version, ActorID: actorID,
	}); err != nil {
		return rep, err
	}
	if err := tx.Commit(ctx); err != nil {
		return rep, err
	}
	rep.Applied = true
	return rep, nil
}

// importRow — карточка, готовая к вставке: колонка и место в ней
// уже выбраны.
type importRow struct {
	card    importer.Card
	col     Column
	pos     string
	started bool
}

// insertImported заводит карточки пачкой и возвращает их
// идентификаторы в том же порядке; пустой — такой ключ уже есть
// в организации.
//
// Пачкой, а не по одной: по запросу на номер, карточку, событие
// и доставку перенос пяти тысяч строк шёл сорок секунд (замер
// 22.09.2026), и столько же — предпросмотр. Запросов теперь столько
// же, сколько видов строк, а не сколько карточек.
func insertImported(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID, source string, rows []importRow,
) ([]string, error) {
	ids := make([]string, len(rows))
	if len(rows) == 0 {
		return ids, nil
	}

	// Номера — одним сдвигом счётчика доски. Строка, отсеянная потом
	// индексом как двойник, свой номер сжигает: выданное не
	// возвращается, как и при обычном заведении.
	var key string
	var last int64
	if err := tx.QueryRow(ctx, `
		update boards set card_seq = card_seq + $2
		 where id = $1
		returning key, card_seq`, boardID, len(rows)).Scan(&key, &last); err != nil {
		return nil, err
	}
	first := last - int64(len(rows)) + 1

	now := time.Now()
	n := len(rows)
	var (
		numbers, columns, positions, titles, descriptions = make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		priorities, externals                             = make([]string, n), make([]string, n)
		created                                           = make([]time.Time, n)
		startedAt, finishedAt, doneAt                     = make([]*time.Time, n), make([]*time.Time, n), make([]*time.Time, n)
		estimates                                         = make([]*float64, n)
		dues                                              = make([]*string, n)
	)
	for i, r := range rows {
		c := r.card
		numbers[i] = key + "-" + strconv.FormatInt(first+int64(i), 10)
		columns[i], positions[i], titles[i], descriptions[i] = r.col.ID, r.pos, c.Title, c.Description
		externals[i], estimates[i] = c.ExternalID, c.Estimate
		priorities[i] = c.Priority
		if priorities[i] == "" {
			priorities[i] = "medium"
		}
		created[i] = now
		if c.Created != nil {
			created[i] = *c.Created
		}
		// Сделанной считается карточка в колонке финиша — как и у
		// заведённой руками. Дата завершения из файла ставится моментом
		// финиша; карточка в другой колонке с датой завершения получает
		// отметку «сделано», а поток о ней не знает — как у подзадачи.
		if r.col.IsFinishedPoint {
			at := now
			if c.Done != nil {
				at = *c.Done
			}
			finishedAt[i] = &at
		} else if c.Done != nil {
			doneAt[i] = c.Done
		}
		// Начало работы в чужой системе неизвестно — считаем его моментом
		// заведения. Время цикла у перенесённых поэтому ближе ко времени
		// выполнения заказа, и это сказано в справке.
		if r.started || r.col.IsFinishedPoint {
			startedAt[i] = &created[i]
		}
		if c.Due != nil {
			d := c.Due.Format("2006-01-02")
			dues[i] = &d
		}
	}

	got, err := tx.Query(ctx, `
		insert into cards (org_id, board_id, number, column_id, position, title, description,
		                   created_at, started_at, finished_at, outcome, done_at,
		                   estimate, priority, due_on, external_source, external_id)
		select $1, $2, t.number, t.col, t.pos, t.title, t.descr,
		       t.created, t.started, t.finished,
		       case when t.finished is not null then 'done' end,
		       t.done, t.estimate::numeric, t.priority, t.due::date, $3, t.ext
		  from unnest($4::text[], $5::uuid[], $6::text[], $7::text[], $8::text[],
		              $9::timestamptz[], $10::timestamptz[], $11::timestamptz[], $12::timestamptz[],
		              $13::float8[], $14::text[], $15::text[], $16::text[])
		    as t(number, col, pos, title, descr, created, started, finished, done,
		         estimate, priority, due, ext)
		on conflict (org_id, external_source, external_id) where external_id is not null
		do nothing
		returning id, external_id`,
		orgID, boardID, source,
		numbers, columns, positions, titles, descriptions,
		created, startedAt, finishedAt, doneAt,
		estimates, priorities, dues, externals)
	if err != nil {
		return nil, err
	}
	byKey := map[string]string{}
	for got.Next() {
		var id, ext string
		if err := got.Scan(&id, &ext); err != nil {
			got.Close()
			return nil, err
		}
		byKey[ext] = id
	}
	got.Close()
	if err := got.Err(); err != nil {
		return nil, err
	}

	// Событие заведения — с отметкой переезда: журнал честно говорит,
	// что карточка появилась здесь сегодня, а не тогда, когда её
	// завели в чужой системе. Подписчики получают card.created, как
	// и за карточку, заведённую руками.
	var cardIDs, toColumns, payloads []string
	var hooks []any
	for i, r := range rows {
		id := byKey[r.card.ExternalID]
		ids[i] = id
		if id == "" {
			continue
		}
		fact := map[string]any{"to": columnFact(r.col), "imported": source}
		// Свой ключ файла — в событии: по нему карточку находят
		// в прежней системе. Выведенный из заголовка ключ ничего
		// не говорит человеку и в событие не идёт.
		if !strings.HasPrefix(r.card.ExternalID, "title:") {
			fact["externalId"] = r.card.ExternalID
		}
		body, err := json.Marshal(fact)
		if err != nil {
			return nil, err
		}
		cardIDs, toColumns, payloads = append(cardIDs, id), append(toColumns, r.col.ID), append(payloads, string(body))
		hooks = append(hooks, map[string]any{
			"event": EventPrefix + "created", "boardId": boardID, "cardId": id,
			"actorId": actorID, "payload": json.RawMessage(body), "at": now.UTC(),
		})
	}
	if len(cardIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into card_events (org_id, board_id, card_id, actor_id, type, to_column, payload)
			select $1, $2, t.card, $3, 'created', t.col, t.body::jsonb
			  from unnest($4::uuid[], $5::uuid[], $6::text[]) with ordinality as t(card, col, body, n)
			 order by t.n`,
			orgID, boardID, actorID, cardIDs, toColumns, payloads); err != nil {
			return nil, err
		}
	}
	if err := webhook.EnqueueMany(ctx, tx, orgID, EventPrefix+"created", hooks); err != nil {
		return nil, err
	}
	return ids, nil
}

func boardColumns(ctx context.Context, tx pgx.Tx, boardID string) ([]Column, error) {
	rows, err := tx.Query(ctx, `
		select `+columnFields+` from board_columns
		 where board_id = $1 and archived_at is null
		 order by position`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		c, err := scanColumn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// newBoardColumns размечает и расставляет колонки новой доски.
//
// Порядок появления в файле — это порядок строк, а не стадий: первая
// же задача «В работе» поставила бы работу левее очереди. Поэтому
// колонки встают по смыслу: очередь, работа, незнакомые стадии
// («Ревью», «На согласовании» — обычно середина потока), готово;
// внутри каждой группы — в порядке появления.
//
// Без точек старта и финиша метрики потока не считаются, поэтому
// разметка ставится сразу: стартом — первая колонка работы, финишем —
// первая готовая. Готовой в файле нет — в конец добавляется «Готово».
// Ошиблись — переразметить колонку на доске можно одной кнопкой.
func newBoardColumns(ctx context.Context, names []string, kinds map[string]string) []Column {
	if hinted(names, kinds) {
		return keptColumns(ctx, names, kinds)
	}
	if len(names) == 0 {
		return []Column{
			{Name: i18n.Name(ctx, "Очередь"), Kind: KindQueue},
			{Name: i18n.Name(ctx, "В работе"), Kind: KindInProgress, IsStartedPoint: true},
			{Name: i18n.Name(ctx, "Готово"), Kind: KindDone, IsFinishedPoint: true},
		}
	}
	var queue, work, unknown, done []Column
	for _, n := range names {
		switch importer.ColumnKind(n) {
		case KindDone:
			done = append(done, Column{Name: n, Kind: KindDone})
		case KindInProgress:
			work = append(work, Column{Name: n, Kind: KindInProgress})
		case KindQueue:
			queue = append(queue, Column{Name: n, Kind: KindQueue})
		default:
			unknown = append(unknown, Column{Name: n, Kind: KindInProgress})
		}
	}
	// Незнакомая колонка, с которой доска начинается, — очередь:
	// работа не может начинаться раньше, чем её взяли.
	if len(queue) == 0 && len(work) == 0 && len(unknown) > 0 {
		unknown[0].Kind = KindQueue
	}
	if len(done) == 0 {
		done = []Column{{Name: i18n.Name(ctx, "Готово"), Kind: KindDone}}
	}
	done[0].IsFinishedPoint = true
	out := append(append(append(queue, work...), unknown...), done...)
	for i := range out {
		if out[i].Kind == KindInProgress {
			out[i].IsStartedPoint = true
			return out
		}
	}
	// Работы нет вовсе — карточка начата, когда готова.
	for i := range out {
		if out[i].IsFinishedPoint {
			out[i].IsStartedPoint = true
		}
	}
	return out
}

// addMissingColumns заводит на существующей доске колонки, которых
// на ней нет, — перед колонкой финиша: новая стадия чужой доски —
// работа, а не готовое. Разметки старта и финиша не трогает: её
// на своей доске человек уже сделал.
func addMissingColumns(ctx context.Context, tx pgx.Tx, orgID, boardID string, have []Column, want []string) ([]Column, error) {
	known := map[string]bool{}
	for _, c := range have {
		known[strings.ToLower(c.Name)] = true
	}
	var names []string
	for _, n := range want {
		if !known[strings.ToLower(n)] {
			known[strings.ToLower(n)] = true
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	prev, next := "", ""
	for i, c := range have {
		if c.IsFinishedPoint || c.Kind == KindDone {
			next = c.Position
			if i > 0 {
				prev = have[i-1].Position
			}
			break
		}
		prev = c.Position
	}
	positions, err := rank.NBetween(prev, next, len(names))
	if err != nil {
		return nil, err
	}
	out := make([]Column, 0, len(names))
	for i, n := range names {
		kind := importer.ColumnKind(n)
		if kind != KindQueue {
			// Вторая колонка финиша перед первой была бы бессмыслицей,
			// а незнакомая стадия обычно середина потока: обе идут
			// работой, разметку человек поправит сам.
			kind = KindInProgress
		}
		c, err := scanColumn(tx.QueryRow(ctx, `
			insert into board_columns (org_id, board_id, name, position, kind)
			values ($1, $2, $3, $4, $5)
			returning `+columnFields, orgID, boardID, n, positions[i], kind))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// importLabels находит метки по названию среди действующих на доске
// и заводит недостающие — меткой этой доски. Метку завести не страшно:
// её видно, её можно убрать. Убранную в архив молча не возвращаем
// и не вешаем — она названа в отчёте.
func importLabels(ctx context.Context, tx pgx.Tx, orgID, boardID string, cards []importer.Card, rep *ImportReport) (map[string]string, error) {
	wanted := map[string]string{}
	for _, c := range cards {
		for _, l := range c.Labels {
			if _, ok := wanted[strings.ToLower(l)]; !ok {
				wanted[strings.ToLower(l)] = l
			}
		}
	}
	out := map[string]string{}
	if len(wanted) == 0 {
		return out, nil
	}
	lower := make([]string, 0, len(wanted))
	for k := range wanted {
		lower = append(lower, k)
	}
	rows, err := tx.Query(ctx, `
		select lower(name), id, archived_at is not null from labels
		 where lower(name) = any($1)
		   and label_path_prefix(label_path(team_id, board_id), label_path(null, $2))
		 order by archived_at nulls first`, lower, boardID)
	if err != nil {
		return nil, err
	}
	archived := map[string]bool{}
	for rows.Next() {
		var name, id string
		var gone bool
		if err := rows.Scan(&name, &id, &gone); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := out[name]; ok || archived[name] {
			continue
		}
		if gone {
			archived[name] = true
			continue
		}
		out[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, key := range sortedKeys(wanted) {
		if _, ok := out[key]; ok {
			continue
		}
		if archived[key] {
			rep.ArchivedLabels = append(rep.ArchivedLabels, wanted[key])
			continue
		}
		tone, err := freeTone(ctx, tx)
		if err != nil {
			return nil, err
		}
		var id string
		if err := tx.QueryRow(ctx, `
			insert into labels (org_id, name, tone, board_id)
			values ($1, $2, $3, $4) returning id`,
			orgID, wanted[key], tone, boardID).Scan(&id); err != nil {
			return nil, err
		}
		out[key] = id
		rep.NewLabels = append(rep.NewLabels, wanted[key])
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// Больше всего карточек — первым: с этой почтой разбираться в первую очередь.
func sortMissing(list []MissingPerson) {
	slices.SortFunc(list, func(a, b MissingPerson) int {
		if c := cmp.Compare(b.Cards, a.Cards); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Email, b.Email); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

// hinted — знает ли источник разметку своих колонок сам. Пакет
// переноса знает: у колонки есть вид, и порядок у неё тот, что был
// на доске, — угадывать по названию тогда незачем.
func hinted(names []string, kinds map[string]string) bool {
	for _, n := range names {
		if kinds[strings.ToLower(n)] != "" {
			return true
		}
	}
	return false
}

// keptColumns — колонки в порядке источника с его разметкой. Вид,
// которого источник не назвал, угадывается по названию; стартом
// становится первая колонка работы, финишем — первая готовая, а без
// готовой в конец добавляется «Готово».
func keptColumns(ctx context.Context, names []string, kinds map[string]string) []Column {
	out := make([]Column, 0, len(names)+1)
	for i, n := range names {
		kind := kinds[strings.ToLower(n)]
		if kind != KindQueue && kind != KindInProgress && kind != KindDone {
			kind = importer.ColumnKind(n)
		}
		if kind == "" {
			kind = KindInProgress
			if i == 0 {
				kind = KindQueue
			}
		}
		out = append(out, Column{Name: n, Kind: kind})
	}
	var started, finished bool
	for i := range out {
		if out[i].Kind == KindInProgress && !started {
			out[i].IsStartedPoint, started = true, true
		}
		if out[i].Kind == KindDone && !finished {
			out[i].IsFinishedPoint, finished = true, true
		}
	}
	if !finished {
		out = append(out, Column{Name: i18n.Name(ctx, "Готово"), Kind: KindDone, IsFinishedPoint: true})
	}
	if !started {
		for i := range out {
			if out[i].IsFinishedPoint {
				out[i].IsStartedPoint = true
			}
		}
	}
	return out
}

// importRelations переносит то, что связывает карточки и что сказано
// о них: подзадачи, связи, обсуждение. Только у карточек, заведённых
// этим прогоном: у переехавших раньше всё это уже было перенесено.
//
// Родитель ищется и среди заведённых сейчас, и среди переехавших
// прошлым прогоном: пакет могли собрать заново, дописав подзадачи
// к уже перенесённой задаче.
func importRelations(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, plan importer.Plan,
	todo []importRow, ids []string, alreadyID, people map[string]string, rep *ImportReport,
) error {
	byKey := map[string]string{}
	for k, id := range alreadyID {
		byKey[k] = id
	}
	parentOf := map[string]string{}
	for i, r := range todo {
		if ids[i] != "" {
			byKey[r.card.ExternalID] = ids[i]
		}
		if r.card.Parent != "" {
			parentOf[r.card.ExternalID] = r.card.Parent
		}
	}
	// Глубина дерева — тем же пределом, что у операции связывания:
	// дерево глубже не заводится руками и не должно приезжать извне.
	depth := func(key string) int {
		n := 1
		for at := parentOf[key]; at != "" && n <= MaxSubtaskDepth; at = parentOf[at] {
			n++
		}
		return n
	}

	// Отдельным выражением, а не внутри склейки: проверка переводов
	// видит слово только так, и английская реплика начиналась бы с «из».
	fromWord := i18n.Name(ctx, "из")

	var from, to, kinds []string
	var cCards []string
	var cAuthors []string
	var cTexts []string
	var cAt []time.Time
	for i, r := range todo {
		id := ids[i]
		if id == "" {
			continue
		}
		c := r.card
		if c.Parent != "" {
			parent := byKey[c.Parent]
			switch {
			case parent == "":
				rep.Problems = append(rep.Problems, importer.Problem{Row: c.Row, Value: c.Parent,
					Message: fmt.Sprintf("родитель «%s» не переехал — карточка переедет без него", c.Parent)})
			case depth(c.ExternalID) > MaxSubtaskDepth:
				rep.Problems = append(rep.Problems, importer.Problem{Row: c.Row, Value: c.Parent,
					Message: fmt.Sprintf("дерево подзадач глубже %d уровней — связь с родителем не переносится", MaxSubtaskDepth)})
			default:
				from, to, kinds = append(from, parent), append(to, id), append(kinds, "subtask")
				rep.Parts++
			}
		}
		for _, l := range c.Links {
			if target := byKey[l.To]; target != "" && target != id {
				from, to, kinds = append(from, id), append(to, target), append(kinds, l.Kind)
				rep.Links++
			}
		}
		for _, cm := range c.Comments {
			author, text := people[cm.AuthorEmail], cm.Text
			if cm.AuthorEmail == "" || author == "" {
				// Автора нет в организации: реплика от переносящего,
				// а чья она — сказано в ней самой. Выдумывать человека
				// нельзя, и приписать слова не тому — тоже.
				author = actorID
				if cm.AuthorName != "" {
					text = fromWord + " " + plan.SourceName + ": " + cm.AuthorName + "\n\n" + text
				}
			}
			at := cm.At
			if at.IsZero() {
				at = time.Now()
			}
			cCards, cAuthors, cTexts, cAt = append(cCards, id), append(cAuthors, author), append(cTexts, text), append(cAt, at)
			rep.Comments++
		}
	}
	if len(from) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into card_links (org_id, from_card, to_card, kind, created_by)
			select $1, f, t, k, $5 from unnest($2::uuid[], $3::uuid[], $4::text[]) as x(f, t, k)
			on conflict do nothing`, orgID, from, to, kinds, actorID); err != nil {
			return err
		}
	}
	if len(cCards) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into card_comments (org_id, board_id, card_id, author_id, body, created_at)
			select $1, $2, c, a, b, t
			  from unnest($3::uuid[], $4::uuid[], $5::text[], $6::timestamptz[]) as x(c, a, b, t)`,
			orgID, boardID, cCards, cAuthors, cTexts, cAt); err != nil {
			return err
		}
	}
	return nil
}
