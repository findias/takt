// Package idea — идеи от людей команды (ROADMAP 43): отдельный список
// организации, в который пишет каждый, в том числе наблюдатель.
//
// Идея — не карточка. У неё нет доски, исполнителя и оценки, она не
// входит в метрики потока и в «создано против сделано». Что с ней
// делать, решают потом (срез 2): берут в работу — тогда появляется
// карточка, — отказывают с причиной или склеивают с такой же.
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
)

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
	Votes  int      `json:"votes"`
	Voters []string `json:"voters"`
	// Voted — мой голос стоит.
	Voted     bool      `json:"voted"`
	CreatedAt time.Time `json:"createdAt"`
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

// ideaColumns — один запрос на идею и на список: число голосов, имена
// поддержавших и мой голос считаются подзапросами, чтобы список
// и ответ на голос не разошлись в форме.
const ideaColumns = `
	select i.id, i.title, i.body, i.author_id, u.name, i.team_id, tm.name,
	       (select count(*) from idea_votes v where v.idea_id = i.id),
	       (select coalesce(array_agg(coalesce(vu.name, '') order by v.created_at), '{}')
	          from idea_votes v left join users vu on vu.id = v.user_id
	         where v.idea_id = i.id),
	       exists (select 1 from idea_votes v where v.idea_id = i.id and v.user_id = $1),
	       i.created_at
	  from ideas i
	  left join users u on u.id = i.author_id
	  left join teams tm on tm.id = i.team_id`

func scanIdea(row pgx.Row) (Idea, error) {
	var out Idea
	err := row.Scan(&out.ID, &out.Title, &out.Body, &out.AuthorID, &out.Author, &out.TeamID, &out.Team,
		&out.Votes, &out.Voters, &out.Voted, &out.CreatedAt)
	if out.Voters == nil {
		out.Voters = []string{}
	}
	return out, err
}

// All — идеи организации, новые сверху. Сортировку «поддержанные»
// делает клиент: список целиком у него и так, а два порядка на сервере
// дали бы два запроса на одну таблицу.
func (s *Service) All(ctx context.Context, orgID, userID string) (List, error) {
	out := List{Ideas: []Idea{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, ideaColumns+`
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
		out, err = scanIdea(tx.QueryRow(ctx, ideaColumns+` where i.id = $2`, userID, id))
		return err
	})
	return out, err
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
		out, err = scanIdea(tx.QueryRow(ctx, ideaColumns+` where i.id = $2`, userID, ideaID))
		return err
	})
	return out, err
}
