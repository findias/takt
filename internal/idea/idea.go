// Package idea — идеи от людей команды (ROADMAP 43): отдельный список
// организации, в который пишет каждый, в том числе наблюдатель.
//
// Идея — не карточка. У неё нет доски, исполнителя и оценки, она не
// входит в метрики потока и в «создано против сделано». Что с ней
// делать, решают словами (decide.go): берут в работу — тогда появляется
// карточка, — отказывают с причиной, называют двойником или откладывают.
// Здесь — подать, увидеть и поддержать.
package idea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/webhook"
)

// События подписок (ROADMAP 43, срез 4): идея подана и по идее решили.
// Голос событием не рассылаем: получатель захлебнулся бы «+1», а решение
// и так несёт итоговое число. Список нужен контракту (EventName)
// и интерфейсу подписок — через webhook.Known.
const (
	EventCreated = "idea.created"
	EventDecided = "idea.decided"
)

// EventNames — события идей для подписки, в порядке жизни идеи.
func EventNames() []string { return []string{EventCreated, EventDecided} }

// Limit — сколько идей отдаём за раз. Больше таблица без листания
// не покажет, а организация в 130 человек столько живых идей не копит.
const Limit = 500

// Пределы длины — те же, что в проверках таблицы (0083), но отказ
// отсюда называет число человеку, а не базе.
const (
	MaxTitle = 200
	MaxBody  = 5000
)

var (
	ErrNotFound   = errors.New("такой идеи нет")
	ErrEmptyTitle = errors.New("назовите идею — хотя бы одной фразой")
	ErrTitleLong  = fmt.Errorf("название идеи — не длиннее %d знаков", MaxTitle)
	ErrBodyLong   = fmt.Errorf("описание идеи — не длиннее %d знаков", MaxBody)
	// ErrTeamUnknown — «кому» указано на подразделение, которого
	// в организации нет или которое убрано.
	ErrTeamUnknown = errors.New("такого подразделения нет — выберите из списка или оставьте идею общей")
)

// Idea — идея в списке организации.
type Idea struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Author пуст, если человека стёрли: идея остаётся, подпись — нет.
	AuthorID *string `json:"authorId"`
	Author   *string `json:"author"`
	// Team — подразделение, к которому идея обращена; пусто — всей
	// организации.
	TeamID *string `json:"teamId"`
	Team   *string `json:"team"`
	// Votes — сколько поддержало; Voters — кто, в порядке голосов:
	// «+1» без имён читалось бы как счётчик, а не как поддержка людей.
	// Голоса идей, названных двойниками этой, считаются здесь же
	// (см. decide.go): одна идея с тридцатью голосами, а не три по десять.
	Votes  int      `json:"votes"`
	Voters []string `json:"voters"`
	// Voted — мой голос стоит.
	Voted     bool      `json:"voted"`
	CreatedAt time.Time `json:"createdAt"`
	// Decision — нынешнее решение по идее; пусто — ещё не решали.
	Decision *Decision `json:"decision"`
	// CanDecide — спрашивающий вправе решить судьбу этой идеи.
	CanDecide bool `json:"canDecide"`
}

// List — идеи и то, всё ли показано.
type List struct {
	Ideas []Idea `json:"ideas"`
	// Truncated — идей больше Limit, показаны новейшие.
	Truncated bool `json:"truncated"`
}

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

// ideaQuery — один запрос на идею и на список: голоса, нынешнее решение
// и право решать считаются здесь, чтобы список и ответ на действие
// не разошлись в форме. $1 — спрашивающий.
//
// `current` — последняя запись решения по каждой идее. Голоса считаются
// по самой идее и по тем, чьё нынешнее решение — «уже есть» с указанием
// на неё: склейка живёт в запросе, а не в данных, и отменяется новой
// записью решения, ничего не теряя.
const ideaQuery = `
	with current as (
		select distinct on (idea_id)
		       idea_id, id, decision, note, author_id, created_at, card_id, into_idea_id
		  from idea_decisions
		 order by idea_id, created_at desc, id desc)
	select i.id, i.title, i.body, i.author_id, u.name, i.team_id, tm.name,
	       v.n, v.names, v.mine, i.created_at,
	       d.id, d.decision, d.note, d.author_id, du.name, d.created_at,
	       c.id, c.board_id, c.number, cb.name, cc.name, cc.kind,
	       c.id is not null and (c.outcome is not null or c.done_at is not null),
	       di.id, di.title,
	       (select app_is_owner())
	         or (i.team_id is not null and i.team_id = any (array(select unnest(app_admin_teams()))))
	  from ideas i
	  left join users u on u.id = i.author_id
	  left join teams tm on tm.id = i.team_id
	  left join current d on d.idea_id = i.id
	  left join users du on du.id = d.author_id
	  left join cards c on c.id = d.card_id and c.archived_at is null
	  left join boards cb on cb.id = c.board_id
	  left join board_columns cc on cc.id = c.column_id
	  left join ideas di on di.id = d.into_idea_id
	  cross join lateral (
		select count(*) as n,
		       coalesce(array_agg(coalesce(vu.name, '') order by one.created_at), '{}') as names,
		       coalesce(bool_or(one.user_id = $1), false) as mine
		  from (select distinct on (x.user_id) x.user_id, x.created_at
		          from idea_votes x
		         where x.idea_id = i.id
		            or x.idea_id in (select m.idea_id from current m
		                              where m.decision = 'duplicate' and m.into_idea_id = i.id)
		         order by x.user_id, x.created_at) one
		  left join users vu on vu.id = one.user_id) v`

func scanIdea(row pgx.Row) (Idea, error) {
	var out Idea
	var d Decision
	var dID, dKind, dNote *string
	var dAt *time.Time
	var cID, cBoard, cNumber, cBoardName, cColumn, cKind *string
	var cDone bool
	var intoID, intoTitle *string
	err := row.Scan(&out.ID, &out.Title, &out.Body, &out.AuthorID, &out.Author, &out.TeamID, &out.Team,
		&out.Votes, &out.Voters, &out.Voted, &out.CreatedAt,
		&dID, &dKind, &dNote, &d.AuthorID, &d.Author, &dAt,
		&cID, &cBoard, &cNumber, &cBoardName, &cColumn, &cKind, &cDone,
		&intoID, &intoTitle, &out.CanDecide)
	if err != nil {
		return out, err
	}
	if out.Voters == nil {
		out.Voters = []string{}
	}
	if dID != nil {
		d.ID, d.Kind, d.Note, d.At = *dID, *dKind, *dNote, *dAt
		if cID != nil {
			d.Card = &CardRef{ID: *cID, BoardID: *cBoard, Number: *cNumber, BoardName: *cBoardName,
				Column: *cColumn, ColumnKind: *cKind, Done: cDone}
		}
		if intoID != nil {
			d.Into = &IdeaRef{ID: *intoID, Title: *intoTitle}
		}
		out.Decision = &d
	}
	return out, nil
}

// one — идея по идентификатору глазами userID; нет или не видна — ErrNotFound.
func one(ctx context.Context, tx pgx.Tx, userID, ideaID string) (Idea, error) {
	i, err := scanIdea(tx.QueryRow(ctx, ideaQuery+` where i.id = $2`, userID, ideaID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Idea{}, ErrNotFound
	}
	return i, err
}

// All — идеи организации, новые сверху. Сортировку «поддержанные»
// делает клиент: список целиком у него и так, а два порядка на сервере
// дали бы два запроса на одну таблицу.
func (s *Service) All(ctx context.Context, orgID, userID string) (List, error) {
	out := List{Ideas: []Idea{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, ideaQuery+`
			 order by i.created_at desc, i.id desc
			 limit $2`, userID, Limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			i, err := scanIdea(rows)
			if err != nil {
				return err
			}
			if len(out.Ideas) == Limit {
				out.Truncated = true
				break
			}
			out.Ideas = append(out.Ideas, i)
		}
		return rows.Err()
	})
	return out, err
}

// Create заводит идею от имени userID. Подразделение «кому» проверяется
// среди живых подразделений организации: ссылку на чужое или убранное
// база бы приняла — внешний ключ политик не читает.
func (s *Service) Create(ctx context.Context, orgID, userID, title, body string, teamID *string) (Idea, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return Idea{}, ErrEmptyTitle
	}
	if utf8.RuneCountInString(title) > MaxTitle {
		return Idea{}, ErrTitleLong
	}
	if utf8.RuneCountInString(body) > MaxBody {
		return Idea{}, ErrBodyLong
	}
	if teamID != nil && strings.TrimSpace(*teamID) == "" {
		teamID = nil
	}
	var out Idea
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if teamID != nil {
			var known bool
			if err := tx.QueryRow(ctx,
				`select exists (select 1 from teams where id = $1 and archived_at is null)`,
				*teamID).Scan(&known); err != nil {
				return err
			}
			if !known {
				return ErrTeamUnknown
			}
		}
		var id string
		if err := tx.QueryRow(ctx, `
			insert into ideas (org_id, author_id, team_id, title, body)
			values ($1, $2, $3, $4, $5)
			returning id`, orgID, userID, teamID, title, body).Scan(&id); err != nil {
			return err
		}
		var err error
		out, err = one(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		return webhook.Enqueue(ctx, tx, orgID, EventCreated, map[string]any{"idea": out.Public()})
	})
	return out, err
}

// Public — идея для подписчика: без того, что зависит от спрашивающего
// (мой голос, моё право решать), — у подписки своего лица нет.
func (i Idea) Public() map[string]any {
	return map[string]any{
		"id": i.ID, "title": i.Title, "body": i.Body,
		"authorId": i.AuthorID, "author": i.Author,
		"teamId": i.TeamID, "team": i.Team,
		"votes": i.Votes, "voters": i.Voters, "createdAt": i.CreatedAt,
		"decision": i.Decision,
	}
}

// Vote ставит или снимает голос userID. Повтор ничего не меняет:
// «поддержать» дважды — один голос, снять без голоса — не ошибка.
// Отвечает идеей целиком, чтобы экран показал число и имена те же,
// что увидят остальные.
func (s *Service) Vote(ctx context.Context, orgID, userID, ideaID string, on bool) (Idea, error) {
	var out Idea
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from ideas where id = $1)`, ideaID).
			Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var err error
		if on {
			_, err = tx.Exec(ctx, `
				insert into idea_votes (org_id, idea_id, user_id) values ($1, $2, $3)
				on conflict do nothing`, orgID, ideaID, userID)
		} else {
			_, err = tx.Exec(ctx, `delete from idea_votes where idea_id = $1 and user_id = $2`, ideaID, userID)
		}
		if err != nil {
			return err
		}
		out, err = one(ctx, tx, userID, ideaID)
		return err
	})
	return out, err
}
