package board

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAvailabilityChanged = errors.New("запись уже изменена: обновите данные и повторите правку")
	ErrVacationOverlap     = errors.New("эти даты пересекаются с другим вашим отпуском")
	ErrCoverageInvalid     = errors.New("замещение недоступно: проверьте отпуск, даты, исполнителя и доступ заместителя к доске")
)

type Vacation struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	StartsOn  string `json:"startsOn"`
	EndsOn    string `json:"endsOn"`
	Note      string `json:"note"`
	Cancelled bool   `json:"cancelled"`
	Version   int64  `json:"version"`
}

type Availability struct {
	Eligible  []string   `json:"eligible"`
	Today     string     `json:"today"`
	TimeZone  string     `json:"timeZone"`
	Vacations []Vacation `json:"vacations"`
	Coverages []Coverage `json:"coverages"`
}

func validDays(start, end string) bool {
	a, e1 := time.Parse(time.DateOnly, start)
	b, e2 := time.Parse(time.DateOnly, end)
	return e1 == nil && e2 == nil && a.Year() >= 1 && b.Year() <= 9999 && start <= end
}

// Один замок на организацию сериализует даты отпуска и ответы на замещение:
// проверка доступности не может разминуться с отпуском заместителя.
func lockAvailability(ctx context.Context, tx pgx.Tx, orgID string) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 91))`, orgID)
	return err
}

func availabilityClock(ctx context.Context, tx pgx.Tx, orgID string) (string, string, error) {
	var zone string
	if err := tx.QueryRow(ctx, `select time_zone from orgs where id=$1`, orgID).Scan(&zone); err != nil {
		return "", "", err
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", "", err
	}
	return time.Now().In(loc).Format(time.DateOnly), zone, nil
}

func (s *Service) Availability(ctx context.Context, orgID, userID string, boardID ...string) (Availability, error) {
	out := Availability{Eligible: []string{}, Vacations: []Vacation{}, Coverages: []Coverage{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		out.Today, out.TimeZone, err = availabilityClock(ctx, tx, orgID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select id,user_id,to_char(starts_on,'YYYY-MM-DD'),to_char(ends_on,'YYYY-MM-DD'),note,cancelled,version
            from vacations where org_id=$1 order by starts_on,id`, orgID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v Vacation
			if err := rows.Scan(&v.ID, &v.UserID, &v.StartsOn, &v.EndsOn, &v.Note, &v.Cancelled, &v.Version); err != nil {
				rows.Close()
				return err
			}
			out.Vacations = append(out.Vacations, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(boardID) > 0 && boardID[0] != "" {
			var visible bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from boards where id=$1)`, boardID[0]).Scan(&visible); err != nil {
				return err
			}
			if !visible {
				return ErrNotFound
			}
			candidates, e := tx.Query(ctx, `select m.user_id from memberships m join users u on u.id=m.user_id where m.org_id=$1 and u.kind='person' and app_writes_board_as(m.user_id,$2)`, orgID, boardID[0])
			if e != nil {
				return e
			}
			for candidates.Next() {
				var id string
				if e := candidates.Scan(&id); e != nil {
					candidates.Close()
					return e
				}
				out.Eligible = append(out.Eligible, id)
			}
			candidates.Close()
			if e := candidates.Err(); e != nil {
				return e
			}
		}
		out.Coverages, err = readCoverages(ctx, tx)
		return err
	})
	return out, err
}

func (s *Service) SaveVacation(ctx context.Context, orgID, userID string, v Vacation) (Vacation, error) {
	v.Note = strings.TrimSpace(v.Note)
	if !validDays(v.StartsOn, v.EndsOn) || utf8.RuneCountInString(v.Note) > 500 {
		return Vacation{}, badRequestf("укажите даты отпуска по порядку и примечание до 500 знаков")
	}
	v.UserID = userID
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := lockAvailability(ctx, tx, orgID); err != nil {
			return err
		}
		var old Vacation
		err := tx.QueryRow(ctx, `select user_id,to_char(starts_on,'YYYY-MM-DD'),to_char(ends_on,'YYYY-MM-DD'),note,cancelled,version from vacations where id=$1`, v.ID).
			Scan(&old.UserID, &old.StartsOn, &old.EndsOn, &old.Note, &old.Cancelled, &old.Version)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			if old.UserID != userID {
				return ErrReadOnlyBoard
			}
			if old.StartsOn == v.StartsOn && old.EndsOn == v.EndsOn && old.Note == v.Note && old.Cancelled == v.Cancelled {
				v.Version = old.Version
				return nil
			}
			if old.Version != v.Version {
				return ErrAvailabilityChanged
			}
		} else if v.Version != 0 {
			return ErrNotFound
		}
		if !v.Cancelled {
			var overlap bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from vacations where user_id=$1 and id<>$2 and not cancelled and starts_on<=$4::date and ends_on>=$3::date)`, userID, v.ID, v.StartsOn, v.EndsOn).Scan(&overlap); err != nil {
				return err
			}
			if overlap {
				return ErrVacationOverlap
			}
		}
		if old.Version == 0 {
			err = tx.QueryRow(ctx, `insert into vacations(id,org_id,user_id,starts_on,ends_on,note,cancelled) values($1,$2,$3,$4::date,$5::date,$6,$7) returning version`, v.ID, orgID, userID, v.StartsOn, v.EndsOn, v.Note, v.Cancelled).Scan(&v.Version)
		} else {
			err = tx.QueryRow(ctx, `update vacations set starts_on=$2::date,ends_on=$3::date,note=$4,cancelled=$5,version=version+1 where id=$1 returning version`, v.ID, v.StartsOn, v.EndsOn, v.Note, v.Cancelled).Scan(&v.Version)
		}
		if err != nil {
			return err
		}
		// Даже скрытые договорённости становятся недействительны по версии
		// отпуска. Видимые участники узнают об изменении в той же транзакции.
		rows, err := tx.Query(ctx, `select cc.id,c.board_id,cc.card_id,cc.substitute_id,cc.created_by,original.user_id from card_coverages cc join cards c on c.id=cc.card_id join vacations original on original.id=cc.vacation_id
            where (cc.vacation_id=$1 or (cc.substitute_id=$2 and cc.starts_on<=$4::date and cc.ends_on>=$3::date)) and cc.status in ('proposed','accepted')`, v.ID, userID, v.StartsOn, v.EndsOn)
		if err != nil {
			return err
		}
		type notice struct {
			id, board, card, sub, absent string
			author                       *string
		}
		notices := []notice{}
		for rows.Next() {
			var n notice
			if err := rows.Scan(&n.id, &n.board, &n.card, &n.sub, &n.author, &n.absent); err != nil {
				rows.Close()
				return err
			}
			notices = append(notices, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, n := range notices {
			recipients := []string{n.sub}
			if n.absent != n.sub {
				recipients = append(recipients, n.absent)
			}
			if n.author != nil && *n.author != n.sub && *n.author != n.absent {
				recipients = append(recipients, *n.author)
			}
			if err := notify(ctx, tx, orgID, n.board, n.card, userID, "coverage_changed", "vacation:"+v.ID+":"+strconv.FormatInt(v.Version, 10)+":"+n.id, recipients); err != nil {
				return err
			}
		}
		return nil
	})
	return v, err
}

func (s *Service) SetAvailabilityZone(ctx context.Context, orgID, userID, zone string) error {
	if _, err := time.LoadLocation(zone); err != nil || zone == "" || zone == "Local" {
		return badRequestf("выберите часовой пояс IANA, например Europe/Moscow")
	}
	return s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := lockAvailability(ctx, tx, orgID); err != nil {
			return err
		}
		var owner bool
		if err := tx.QueryRow(ctx, `select app_is_owner()`).Scan(&owner); err != nil {
			return err
		}
		if !owner {
			return ErrReadOnlyBoard
		}
		_, err := tx.Exec(ctx, `update orgs set time_zone=$2 where id=$1`, orgID, zone)
		return err
	})
}
