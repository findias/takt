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

// ErrLabelExists — метка с таким названием уже действует там же. Две
// одинаковые метки начинают вешать вперемешку, а фильтр показывает
// половину. Конкретный отказ — LabelTakenError: он называет, где метка
// уже есть.
var ErrLabelExists = errors.New("метка с таким названием уже есть")

// ErrLabelNotFound — метки нет или она не видна. Своя ошибка, а не общее
// «не найдено»: то отвечает «доска не найдена» и отправляет человека
// проверять доску, с которой всё в порядке.
var ErrLabelNotFound = errors.New("метка не найдена")

// ErrLabelNotYours — метку видно, но её область не ваша. Отказ называет,
// кто может: иначе он читается как поломка.
var ErrLabelNotYours = errors.New("эту метку заводят и убирают те, кто отвечает за её область: " +
	"метку подразделения — его участники и администраторы, метку доски — те, кто в неё пишет")

// LabelTakenError называет, где уже есть метка с тем же названием.
type LabelTakenError struct {
	Name  string
	Where string
}

func (e *LabelTakenError) Error() string {
	return "метка «" + e.Name + "» уже есть " + e.Where + " и действует здесь же — " +
		"вторая с тем же названием на одной карточке была бы неотличима"
}

func (e *LabelTakenError) Is(target error) bool { return target == ErrLabelExists }

// Tones — закрытый набор оттенков, тот же, что у аватаров.
var Tones = []string{"slate", "green", "blue", "violet", "rose", "amber", "teal", "brown"}

// Области метки. Организация действует на всех досках, подразделение —
// на своих и на досках всех вложенных, доска — только на себе.
const (
	ScopeOrg   = "org"
	ScopeTeam  = "team"
	ScopeBoard = "board"
)

// Label — метка вместе с тем, откуда она: без этого две «Срочно»
// из разных мест на экране не различить, а «почему этой метки нет
// на соседней доске» не на что ответить.
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tone string `json:"tone"`
	// org, team или board.
	Scope string `json:"scope"`
	// Подразделение или доска, которым метка принадлежит, и их название.
	// У метки организации пусто.
	ScopeID   *string `json:"scopeId,omitempty"`
	ScopeName *string `json:"scopeName,omitempty"`
	// Убрана в архив: больше не предлагается, но остаётся там,
	// где уже висит.
	Archived bool `json:"archived"`
}

// BoardLabel — метка в снимке доски.
type BoardLabel struct {
	Label
	// Можно ли повесить её на этой доске. Нельзя — у убранной и у той,
	// что осталась на карточке из чужой области: доску передали другому
	// подразделению или подразделение перенесли в дереве.
	// Такие метки в снимке есть ради карточек, на которых висят.
	Offered bool `json:"offered"`
}

// ManagedLabel — метка в списке управления.
type ManagedLabel struct {
	Label
	// Может ли спрашивающий её убрать и вернуть. Решает политика базы;
	// здесь ответ заранее, чтобы экран не предлагал кнопку, которая
	// откажет.
	CanManage bool `json:"canManage"`
}

// LabelPlace — где человек может завести метку.
type LabelPlace struct {
	Scope string  `json:"scope"`
	ID    *string `json:"id,omitempty"`
	Name  string  `json:"name"`
}

// LabelDraft — новая метка. TeamID и BoardID взаимоисключающие; оба
// пустые — метка организации.
type LabelDraft struct {
	Name    string
	Tone    string
	TeamID  string
	BoardID string
}

// Общая часть чтения метки: область и её название. Названия берутся
// соединением, а не хранятся в метке: подразделение переименуют,
// и метка должна назвать его по-новому.
const labelColumns = `
	l.id, l.name, l.tone,
	case when l.board_id is not null then 'board'
	     when l.team_id  is not null then 'team'
	     else 'org' end,
	coalesce(l.board_id, l.team_id),
	coalesce(lb.name, lt.name),
	l.archived_at is not null`

const labelJoins = `
	  from labels l
	  left join teams  lt on lt.id = l.team_id
	  left join boards lb on lb.id = l.board_id`

// Порядок: сперва общие, потом подразделения, потом доски — от широкого
// к узкому, как их и читают.
const labelOrder = `
	 order by (l.team_id is not null)::int + 2 * (l.board_id is not null)::int,
	          coalesce(lb.name, lt.name), lower(l.name)`

type rowScanner interface{ Scan(dest ...any) error }

func scanLabel(row rowScanner, extra ...any) (Label, error) {
	var l Label
	dest := append([]any{&l.ID, &l.Name, &l.Tone, &l.Scope, &l.ScopeID, &l.ScopeName, &l.Archived}, extra...)
	err := row.Scan(dest...)
	return l, err
}

// Labels — все видимые метки, вместе с убранными: их возвращают из того
// же списка.
func (s *Service) Labels(ctx context.Context, orgID, userID string) ([]ManagedLabel, error) {
	out := []ManagedLabel{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+labelColumns+`, `+canManageLabel+labelJoins+labelOrder)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m ManagedLabel
			if m.Label, err = scanLabel(rows, &m.CanManage); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// canManageLabel повторяет условие политики manage из 0052. Повтор,
// а не общая функция, потому что политика — источник решения, а это
// только подсказка экрану: разойдутся — откажет база, и отказ будет
// внятным (ErrLabelNotYours), просто кнопка окажется лишней.
const canManageLabel = `
	(select app_can_write()) and (
	    (l.team_id is null and l.board_id is null)
	 or (l.team_id is not null and (
	        (select app_is_owner())
	     or l.team_id = any (array(select unnest(app_member_teams())))
	     or l.team_id = any (array(select unnest(app_admin_teams())))))
	 or (l.board_id is not null and l.board_id = any (array(select unnest(app_writable_boards())))))`

// LabelPlaces — где спрашивающий может завести метку: организация,
// подразделения и доски. Считается сервером, потому что права
// на подразделение наследуются вниз по дереву, а дерево у клиента
// неполное.
func (s *Service) LabelPlaces(ctx context.Context, orgID, userID string) ([]LabelPlace, error) {
	out := []LabelPlace{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select 'org', null::uuid, ''
			 where (select app_can_write())
			union all
			(select 'team', t.id, t.name
			   from teams t
			  where t.archived_at is null
			    and (select app_can_write())
			    and ((select app_is_owner())
			         or t.id = any (array(select unnest(app_member_teams())))
			         or t.id = any (array(select unnest(app_admin_teams()))))
			  order by t.ancestor_ids, t.name)
			union all
			(select 'board', b.id, b.name
			   from boards b
			  where b.archived_at is null
			    and b.id = any (array(select unnest(app_writable_boards())))
			  order by lower(b.name))`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p LabelPlace
			if err := rows.Scan(&p.Scope, &p.ID, &p.Name); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateLabel(ctx context.Context, orgID, actorID string, d LabelDraft) (Label, error) {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return Label{}, badRequestf("у метки должно быть название")
	}
	if d.TeamID != "" && d.BoardID != "" {
		return Label{}, badRequestf("метка принадлежит либо подразделению, либо доске")
	}
	tone := d.Tone
	if tone == "" {
		tone = Tones[0]
	}
	var l Label
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Область обязана существовать и быть видна: внешний ключ
		// пропустил бы подразделение чужой организации, а политика —
		// доску, которой уже нет.
		if d.TeamID != "" {
			var ok bool
			if err := tx.QueryRow(ctx, `select exists (select 1 from teams
			    where id = $1 and archived_at is null)`, d.TeamID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return badRequestf("подразделения нет или оно убрано в архив")
			}
		}
		if d.BoardID != "" {
			var ok bool
			if err := tx.QueryRow(ctx, `select exists (select 1 from boards
			    where id = $1 and archived_at is null)`, d.BoardID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return ErrNotFound
			}
		}
		if err := labelNameFree(ctx, tx, name, d.TeamID, d.BoardID, ""); err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx, `
			insert into labels (org_id, name, tone, team_id, board_id)
			values ($1, $2, $3, nullif($4, '')::uuid, nullif($5, '')::uuid)
			returning id`, orgID, name, tone, d.TeamID, d.BoardID).Scan(&id); err != nil {
			return err
		}
		var err error
		l, err = scanLabel(tx.QueryRow(ctx, `select `+labelColumns+labelJoins+` where l.id = $1`, id))
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.ConstraintName == "labels_name_idx":
			return Label{}, ErrLabelExists
		case pgErr.ConstraintName == "labels_tone_valid":
			return Label{}, badRequestf("незнакомый оттенок метки")
		case pgErr.Code == "42501":
			return Label{}, ErrLabelNotYours
		}
	}
	return l, err
}

// labelNameFree проверяет, что метка с тем же названием не действует
// там же, где будет действовать новая.
//
// Уникальный индекс сторожит одну область; этого мало. «Срочно»
// организации и «Срочно» доски — разные области, но на карточке этой
// доски висели бы обе, неотличимые друг от друга. Пересекаются области,
// если путь одной — начало пути другой (label_path в 0052).
//
// Проверяется по видимому: метку закрытой доски, которой человек
// не видит, он не найдёт и здесь. Это в безопасную сторону —
// худшее, что случится, — одноимённая метка, помеченная своим
// происхождением; сказать «на закрытой доске уже есть такая» значило бы
// раскрыть закрытую доску.
func labelNameFree(ctx context.Context, tx pgx.Tx, name, teamID, boardID, exceptID string) error {
	var taken Label
	taken, err := scanLabel(tx.QueryRow(ctx, `
		select `+labelColumns+labelJoins+`
		 where l.archived_at is null
		   and lower(l.name) = lower($1)
		   and l.id::text <> $4
		   and (label_path_prefix(label_path(l.team_id, l.board_id),
		                          label_path(nullif($2, '')::uuid, nullif($3, '')::uuid))
		     or label_path_prefix(label_path(nullif($2, '')::uuid, nullif($3, '')::uuid),
		                          label_path(l.team_id, l.board_id)))
		 limit 1`, name, teamID, boardID, exceptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &LabelTakenError{Name: taken.Name, Where: labelWhere(taken)}
}

// labelWhere — «у организации», «у подразделения «Платформа»».
func labelWhere(l Label) string {
	name := ""
	if l.ScopeName != nil {
		name = *l.ScopeName
	}
	switch l.Scope {
	case ScopeTeam:
		return "у подразделения «" + name + "»"
	case ScopeBoard:
		return "у доски «" + name + "»"
	default:
		return "у организации"
	}
}

// ArchiveLabel убирает метку из обихода, не снимая её с карточек.
//
// Удалять нельзя: карточка, помеченная «срочно» полгода назад, объясняет
// этим своё время в очереди, и стирание метки задним числом делает
// историю неверной. Убранная метка перестаёт предлагаться, но остаётся
// видимой там, где уже висит, и её можно вернуть.
func (s *Service) ArchiveLabel(ctx context.Context, orgID, actorID, labelID string) error {
	return s.setLabelArchived(ctx, orgID, actorID, labelID, true)
}

// RestoreLabel возвращает убранную метку. Пока она лежала в архиве,
// там же могли завести другую с тем же названием — тогда отказ
// называет, где она.
func (s *Service) RestoreLabel(ctx context.Context, orgID, actorID, labelID string) error {
	return s.setLabelArchived(ctx, orgID, actorID, labelID, false)
}

func (s *Service) setLabelArchived(ctx context.Context, orgID, actorID, labelID string, archive bool) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var (
			name            string
			teamID, boardID *string
			archived        bool
		)
		err := tx.QueryRow(ctx, `
			select name, team_id::text, board_id::text, archived_at is not null
			  from labels where id = $1`, labelID).Scan(&name, &teamID, &boardID, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLabelNotFound
		}
		if err != nil {
			return err
		}
		if archived == archive {
			// Повтор безобиден: кнопку нажали дважды или в двух окнах.
			return nil
		}
		if !archive {
			if err := labelNameFree(ctx, tx, name, deref(teamID), deref(boardID), labelID); err != nil {
				return err
			}
		}
		// Видимую, но чужую метку политика не даст тронуть, и update
		// просто не найдёт строку. Отличить это от «метки нет» можно
		// только здесь: выше метка уже прочитана.
		tag, err := tx.Exec(ctx, `
			update labels
			   set archived_at = case when $2 then now() end
			 where id = $1`, labelID, archive)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "labels_name_idx" {
				return ErrLabelExists
			}
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrLabelNotYours
		}
		return nil
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

	// Вешаем только на карточку этой доски и только меткой, которая
	// здесь действует: живой, из организации, из подразделения доски
	// или его старших, или своей доски. Всё — одним запросом: раздельные
	// проверки дали бы окно, в котором метку успевают убрать.
	tag, err := tx.Exec(ctx, `
		insert into card_labels (org_id, card_id, label_id, added_by)
		select $1, $2, $3, $4
		 where exists (select 1 from cards
		                where id = $2 and board_id = $5 and archived_at is null)
		   and exists (select 1 from labels
		                where id = $3 and archived_at is null
		                  and label_path_prefix(label_path(team_id, board_id),
		                                        label_path(null, $5)))
		on conflict (card_id, label_id) do nothing`,
		orgID, p.CardID, p.LabelID, actorID, boardID)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		// Либо метка уже висит — повтор безобиден, — либо повесить её
		// нельзя. Что именно нельзя, говорится словами: «метки нет»
		// про метку соседней доски отправило бы искать пропажу.
		var hung bool
		var where *Label
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from card_labels where card_id = $1 and label_id = $2)`,
			p.CardID, p.LabelID).Scan(&hung); err != nil {
			return Patch{}, err
		}
		if !hung {
			l, err := scanLabel(tx.QueryRow(ctx,
				`select `+labelColumns+labelJoins+` where l.id = $1`, p.LabelID))
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return Patch{}, err
			default:
				where = &l
			}
			switch {
			case where == nil:
				return Patch{}, conflictf("", "метки или карточки нет")
			case where.Archived:
				return Patch{}, conflictf("", "метка «%s» убрана в архив — вернуть её из архива можно на экране «Команда»", where.Name)
			default:
				return Patch{}, conflictf("", "метка «%s» принадлежит %s и на этой доске не действует",
					where.Name, strings.TrimPrefix(labelWhere(*where), "у "))
			}
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

// loadLabels кладёт в снимок словарь меток и то, что чем помечено.
// Раздельно, а не списком меток внутри каждой карточки: иначе название
// метки уезжало бы в снимок столько раз, на скольких карточках оно висит.
//
// В словаре — метки, действующие на доске, и вдобавок все, что висят
// на её карточках, даже убранные и чужие. Прежде словарь был только
// из живых, и убранная метка исчезала с карточек на экране, хотя
// в базе и в cardLabels оставалась: обещание «останется там, где
// висит» выполнялось всюду, кроме места, где его проверяет глаз.
func loadLabels(ctx context.Context, tx pgx.Tx, boardID string, snap *Snapshot) error {
	snap.Labels = []BoardLabel{}
	snap.CardLabels = map[string][]string{}

	rows, err := tx.Query(ctx, `
		select `+labelColumns+`, l.archived_at is null and `+labelApplies+labelJoins+`
		 where (l.archived_at is null and `+labelApplies+`)
		    or l.id in (select cl.label_id
		                  from card_labels cl join cards c on c.id = cl.card_id
		                 where c.board_id = $1 and c.archived_at is null)`+labelOrder, boardID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var l BoardLabel
		if l.Label, err = scanLabel(rows, &l.Offered); err != nil {
			return err
		}
		snap.Labels = append(snap.Labels, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

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

// labelApplies — действует ли метка l на доске $1.
const labelApplies = `label_path_prefix(label_path(l.team_id, l.board_id), label_path(null, $1::uuid))`
