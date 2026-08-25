// Package audit — чтение журнала административных действий.
//
// Пишет журнал база триггерами, здесь только чтение. Кто его читает,
// решает политика: владелец организации и наблюдатель всей организации.
// Недоступность выглядит как пустая лента, а не как отказ — подтверждать
// существование того, чего не видно, незачем.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
)

// Limit — размер страницы. Лента листается курсором по идентификатору:
// журнал только дописывается, поэтому идентификатор монотонный, а смещение
// по номеру страницы на растущей ленте врёт.
const Limit = 50

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

type Entry struct {
	ID int64 `json:"id"`
	// Пусто, если действие сделано без установленной личности: миграцией
	// или служебной задачей. Подделать подпись нельзя, а не назваться —
	// можно, и такие записи видно как есть.
	Actor     *string         `json:"actor"`
	Action    string          `json:"action"`
	Subject   string          `json:"subject"`
	SubjectID *string         `json:"subjectId"`
	Payload   json.RawMessage `json:"payload"`
	At        time.Time       `json:"at"`
}

type Page struct {
	Entries []Entry `json:"entries"`
	Next    *int64  `json:"next"`
}

func (s *Service) List(ctx context.Context, orgID, userID string, before *int64) (Page, error) {
	page := Page{Entries: []Entry{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select a.id, u.name, a.action, a.subject, a.subject_id::text, a.payload, a.at
			  from audit_events a
			  left join users u on u.id = a.actor_id
			 where ($1::bigint is null or a.id < $1)
			 order by a.id desc
			 limit $2`, before, Limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Entry
			if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Subject,
				&e.SubjectID, &e.Payload, &e.At); err != nil {
				return err
			}
			page.Entries = append(page.Entries, e)
		}
		return rows.Err()
	})
	if err != nil {
		return Page{}, err
	}

	if len(page.Entries) > Limit {
		page.Entries = page.Entries[:Limit]
		last := page.Entries[len(page.Entries)-1].ID
		page.Next = &last
	}
	return page, nil
}
