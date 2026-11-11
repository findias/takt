package board

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type Coverage struct {
	ID              string `json:"id"`
	VacationID      string `json:"vacationId"`
	VacationVersion int64  `json:"vacationVersion"`
	UserID          string `json:"userId"`
	CardID          string `json:"cardId"`
	CardNumber      string `json:"cardNumber"`
	CardTitle       string `json:"cardTitle"`
	BoardID         string `json:"boardId"`
	SubstituteID    string `json:"substituteId"`
	StartsOn        string `json:"startsOn"`
	EndsOn          string `json:"endsOn"`
	Status          string `json:"status"`
	Version         int64  `json:"version"`
	Valid           bool   `json:"valid"`
	CanManage       bool   `json:"canManage"`
}

// Проверка считается при чтении: смена прав, исполнителя, архивация,
// перенос карточки и новый отпуск заместителя не оставляют ложного
// обещания «задача передана». Названия скрытых карточек отсеивает RLS.
const coverageValid = `not v.cancelled and cc.vacation_version=v.version
    and cc.starts_on>=v.starts_on and cc.ends_on<=v.ends_on
    and c.archived_at is null and b.archived_at is null and c.outcome is null and c.done_at is null
    and exists(select 1 from card_assignees a where a.card_id=c.id and a.user_id=v.user_id)
    and exists(select 1 from memberships m join users u on u.id=m.user_id
        where m.org_id=cc.org_id and m.user_id=cc.substitute_id and u.kind='person')
    and app_writes_board_as(cc.substitute_id,c.board_id)
    and not exists(select 1 from vacations other where other.user_id=cc.substitute_id
        and not other.cancelled and other.starts_on<=cc.ends_on and other.ends_on>=cc.starts_on)`

func readCoverages(ctx context.Context, tx pgx.Tx) ([]Coverage, error) {
	rows, err := tx.Query(ctx, `select cc.id,cc.vacation_id,cc.vacation_version,v.user_id,cc.card_id,c.number,c.title,c.board_id,
        cc.substitute_id,to_char(cc.starts_on,'YYYY-MM-DD'),to_char(cc.ends_on,'YYYY-MM-DD'),cc.status,cc.version,
        (`+coverageValid+`), (v.user_id=app_current_user() or c.board_id=any(app_writable_boards()))
        from card_coverages cc join vacations v on v.id=cc.vacation_id
        join cards c on c.id=cc.card_id join boards b on b.id=c.board_id order by cc.created_at,cc.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Coverage{}
	for rows.Next() {
		var c Coverage
		if err := rows.Scan(&c.ID, &c.VacationID, &c.VacationVersion, &c.UserID, &c.CardID, &c.CardNumber, &c.CardTitle, &c.BoardID, &c.SubstituteID, &c.StartsOn, &c.EndsOn, &c.Status, &c.Version, &c.Valid, &c.CanManage); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) ProposeCoverage(ctx context.Context, orgID, userID string, c Coverage) (Coverage, error) {
	if !validDays(c.StartsOn, c.EndsOn) {
		return c, badRequestf("укажите даты замещения по порядку")
	}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := lockAvailability(ctx, tx, orgID); err != nil {
			return err
		}
		// Один идентификатор сохраняется при повторе после потери ответа.
		var vacation, card, sub, start, end string
		var version int64
		err := tx.QueryRow(ctx, `select vacation_id,card_id,substitute_id,to_char(starts_on,'YYYY-MM-DD'),to_char(ends_on,'YYYY-MM-DD'),vacation_version from card_coverages where id=$1`, c.ID).Scan(&vacation, &card, &sub, &start, &end, &version)
		if err == nil {
			if vacation != c.VacationID || card != c.CardID || sub != c.SubstituteID || start != c.StartsOn || end != c.EndsOn || version != c.VacationVersion {
				return ErrAvailabilityChanged
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var allowed bool
		err = tx.QueryRow(ctx, `select exists(select 1 from vacations v join cards c on c.id=$2 join boards b on b.id=c.board_id
            where v.id=$1 and not v.cancelled and v.version=$6 and v.user_id<>$3
            and c.board_id=any(app_writable_boards()) and c.archived_at is null and b.archived_at is null
            and c.outcome is null and c.done_at is null and $4::date>=v.starts_on and $5::date<=v.ends_on
            and exists(select 1 from card_assignees a where a.card_id=c.id and a.user_id=v.user_id)
            and exists(select 1 from memberships m join users u on u.id=m.user_id where m.org_id=$7 and m.user_id=$3 and u.kind='person')
            and app_writes_board_as($3,c.board_id)
            and not exists(select 1 from vacations other where other.user_id=$3 and not other.cancelled and other.starts_on<=$5::date and other.ends_on>=$4::date))`,
			c.VacationID, c.CardID, c.SubstituteID, c.StartsOn, c.EndsOn, c.VacationVersion, orgID).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrCoverageInvalid
		}
		var overlap bool
		err = tx.QueryRow(ctx, `select exists(select 1 from card_coverages where vacation_id=$1 and card_id=$2 and vacation_version=$5 and status in ('proposed','accepted') and starts_on<=$4::date and ends_on>=$3::date)`, c.VacationID, c.CardID, c.StartsOn, c.EndsOn, c.VacationVersion).Scan(&overlap)
		if err != nil {
			return err
		}
		if overlap {
			return ErrAvailabilityChanged
		}
		_, err = tx.Exec(ctx, `insert into card_coverages(id,org_id,vacation_id,vacation_version,card_id,substitute_id,starts_on,ends_on,created_by) values($1,$2,$3,$4,$5,$6,$7::date,$8::date,$9)`, c.ID, orgID, c.VacationID, c.VacationVersion, c.CardID, c.SubstituteID, c.StartsOn, c.EndsOn, userID)
		if err != nil {
			return err
		}
		var board string
		if err := tx.QueryRow(ctx, `select board_id from cards where id=$1`, c.CardID).Scan(&board); err != nil {
			return err
		}
		return notify(ctx, tx, orgID, board, c.CardID, userID, "coverage_changed", "coverage:"+c.ID+":1", []string{c.SubstituteID})
	})
	if err != nil {
		return c, err
	}
	return s.coverageByID(ctx, orgID, userID, c.ID)
}

func (s *Service) coverageByID(ctx context.Context, orgID, userID, id string) (Coverage, error) {
	var out Coverage
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		list, err := readCoverages(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range list {
			if c.ID == id {
				out = c
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

func (s *Service) RespondCoverage(ctx context.Context, orgID, userID, id, status string, version int64) (Coverage, error) {
	if status != "accepted" && status != "declined" && status != "cancelled" {
		return Coverage{}, badRequestf("выберите ответ на замещение: accepted, declined или cancelled")
	}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		if err := lockAvailability(ctx, tx, orgID); err != nil {
			return err
		}
		list, err := readCoverages(ctx, tx)
		if err != nil {
			return err
		}
		var current *Coverage
		for i := range list {
			if list[i].ID == id {
				current = &list[i]
				break
			}
		}
		if current == nil {
			return ErrNotFound
		}
		c := *current
		if status == "cancelled" {
			if !c.CanManage && c.SubstituteID != userID {
				return ErrReadOnlyBoard
			}
		} else if c.SubstituteID != userID {
			return ErrReadOnlyBoard
		}
		if c.Status == status {
			return nil
		}
		if c.Version != version {
			return ErrAvailabilityChanged
		}
		if status != "cancelled" && c.Status != "proposed" {
			return ErrAvailabilityChanged
		}
		if status == "accepted" && !c.Valid {
			return ErrCoverageInvalid
		}
		if _, err := tx.Exec(ctx, `update card_coverages set status=$2,version=version+1 where id=$1`, id, status); err != nil {
			return err
		}
		recipients := []string{c.SubstituteID}
		if c.UserID != c.SubstituteID {
			recipients = append(recipients, c.UserID)
		}
		var author *string
		if err := tx.QueryRow(ctx, `select created_by from card_coverages where id=$1`, id).Scan(&author); err != nil {
			return err
		}
		if author != nil && *author != c.UserID && *author != c.SubstituteID {
			recipients = append(recipients, *author)
		}
		return notify(ctx, tx, orgID, c.BoardID, c.CardID, userID, "coverage_changed", "coverage:"+id+":"+strconv.FormatInt(c.Version+1, 10), recipients)
	})
	if err != nil {
		return Coverage{}, err
	}
	return s.coverageByID(ctx, orgID, userID, id)
}
