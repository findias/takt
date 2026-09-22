package board

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/rank"
	"github.com/findias/takt/internal/realtime"
)

// ImportTarget — куда переносим: в новую доску (имя) или в существующую.
type ImportTarget struct {
	BoardID      string
	NewBoardName string
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
	// Уже перенесённые прежним прогоном — по внешнему ключу.
	Skipped    []ImportSkip   `json:"skipped"`
	NewColumns []ImportColumn `json:"newColumns"`
	NewLabels  []string       `json:"newLabels"`
	// Метки, найденные только в архиве: вешать убранное молча нельзя.
	ArchivedLabels []string              `json:"archivedLabels"`
	MissingPeople  []MissingPerson       `json:"missingPeople"`
	Problems       []importer.Problem    `json:"problems"`
	Dates          []importer.DateFormat `json:"dates"`
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

// ImportColumn — колонка, которую импорт заведёт.
type ImportColumn struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// MissingPerson — почта из файла, которой нет в организации.
type MissingPerson struct {
	Email string `json:"email"`
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

	// Доска и её колонки.
	var columns []Column
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
		added, err := addMissingColumns(ctx, tx, orgID, target.BoardID, columns, plan.Columns)
		if err != nil {
			return rep, err
		}
		for _, c := range added {
			rep.NewColumns = append(rep.NewColumns, ImportColumn{Name: c.Name, Kind: c.Kind})
		}
		if columns, err = boardColumns(ctx, tx, target.BoardID); err != nil {
			return rep, err
		}
	} else {
		name := strings.TrimSpace(target.NewBoardName)
		if name == "" {
			return rep, badRequestf("у доски должно быть название")
		}
		wanted := newBoardColumns(ctx, plan.Columns)
		var b Info
		if b, columns, err = createBoard(ctx, tx, orgID, name, "", wanted); err != nil {
			return rep, err
		}
		rep.BoardID, rep.BoardName, rep.NewBoard = b.ID, b.Name, true
		for _, c := range columns {
			rep.NewColumns = append(rep.NewColumns, ImportColumn{Name: c.Name, Kind: c.Kind})
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
	rows, err := tx.Query(ctx, `
		select c.external_id, c.number, b.name
		  from cards c join boards b on b.id = c.board_id
		 where c.external_source = $1 and c.external_id = any($2)`, plan.Source, keys)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var k string
		var sk ImportSkip
		if err := rows.Scan(&k, &sk.Number, &sk.Board); err != nil {
			rows.Close()
			return rep, err
		}
		already[k] = sk
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
			if named, ok := byName[strings.ToLower(c.Column)]; ok {
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

	for i, c := range plan.Cards {
		if sk, ok := already[c.ExternalID]; ok {
			sk.Row, sk.Title = c.Row, c.Title
			rep.Skipped = append(rep.Skipped, sk)
			continue
		}
		col := placeOf[i]
		pos := positions[col.ID][0]
		positions[col.ID] = positions[col.ID][1:]

		created, err := insertImported(ctx, tx, orgID, actorID, rep.BoardID, col, pos, c, plan.Source, started(col))
		if err != nil {
			return rep, err
		}
		if created == "" {
			// Двойник на доске, которой спрашивающий не видит: индекс
			// его нашёл, а мы — нет. Назвать доску нельзя, но сказать,
			// что строка уже переехала, — можно и нужно.
			rep.Skipped = append(rep.Skipped, ImportSkip{Row: c.Row, Title: c.Title})
			continue
		}
		rep.Created++

		for _, e := range c.Assignees {
			id, ok := people[e]
			if !ok {
				missing[e]++
				continue
			}
			if _, err := tx.Exec(ctx, `
				insert into card_assignees (org_id, card_id, user_id, added_by)
				values ($1, $2, $3, $4)
				on conflict (card_id, user_id) do nothing`, orgID, created, id, actorID); err != nil {
				return rep, err
			}
		}
		for _, name := range c.Labels {
			id, ok := labels[strings.ToLower(name)]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `
				insert into card_labels (org_id, card_id, label_id, added_by)
				values ($1, $2, $3, $4)
				on conflict (card_id, label_id) do nothing`, orgID, created, id, actorID); err != nil {
				return rep, err
			}
		}
	}
	for e, n := range missing {
		rep.MissingPeople = append(rep.MissingPeople, MissingPerson{Email: e, Cards: n})
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

// insertImported заводит одну карточку. Пустой идентификатор — такой
// ключ уже есть в организации.
func insertImported(
	ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string,
	col Column, pos string, c importer.Card, source string, started bool,
) (string, error) {
	var number string
	if err := tx.QueryRow(ctx, `
		update boards set card_seq = card_seq + 1
		 where id = $1
		returning key || '-' || card_seq`, boardID).Scan(&number); err != nil {
		return "", err
	}

	created := time.Now()
	if c.Created != nil {
		created = *c.Created
	}
	// Сделанной считается карточка в колонке финиша — как и у заведённой
	// руками. Дата завершения из файла ставится моментом финиша;
	// карточка в другой колонке с датой завершения получает отметку
	// «сделано», а поток о ней не знает — как у подзадачи.
	var finishedAt, doneAt, startedAt *time.Time
	if col.IsFinishedPoint {
		at := time.Now()
		if c.Done != nil {
			at = *c.Done
		}
		finishedAt = &at
	} else if c.Done != nil {
		doneAt = c.Done
	}
	// Начало работы в чужой системе неизвестно — считаем его моментом
	// заведения. Время цикла у перенесённых поэтому ближе ко времени
	// выполнения заказа, и это сказано в справке.
	if started || col.IsFinishedPoint {
		startedAt = &created
	}
	priority := c.Priority
	if priority == "" {
		priority = "medium"
	}
	var due *string
	if c.Due != nil {
		d := c.Due.Format("2006-01-02")
		due = &d
	}

	var id string
	err := tx.QueryRow(ctx, `
		insert into cards (org_id, board_id, number, column_id, position, title, description,
		                   created_at, started_at, finished_at, outcome, done_at,
		                   estimate, priority, due_on, external_source, external_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        case when $10::timestamptz is not null then 'done' end,
		        $11, $12, $13, $14::date, $15, $16)
		on conflict (org_id, external_source, external_id) where external_id is not null
		do nothing
		returning id`,
		orgID, boardID, number, col.ID, pos, c.Title, c.Description,
		created, startedAt, finishedAt, doneAt, c.Estimate, priority, due,
		source, c.ExternalID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Событие заведения — с отметкой переезда: журнал честно говорит,
	// что карточка появилась здесь сегодня, а не тогда, когда её
	// завели в чужой системе.
	if err := logEvent(ctx, tx, orgID, boardID, id, actorID, "created", nil, &col.ID,
		map[string]any{"to": columnFact(col), "imported": source}); err != nil {
		return "", err
	}
	return id, nil
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
func newBoardColumns(ctx context.Context, names []string) []Column {
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
		return cmp.Compare(a.Email, b.Email)
	})
}
