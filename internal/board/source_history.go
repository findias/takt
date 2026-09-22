package board

import (
	"cmp"
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer"
)

// История задачи в системе, откуда карточку перенесли (ROADMAP 23.7,
// миграция 0065). Пакет с историей пишет её при переносе; перенос
// из YouGile по API — фоном, после переноса карточек: чат каждой задачи
// — отдельный запрос, а YouGile отвечает не чаще 50 раз в минуту.

// SourceHistoryEntry — запись истории источника на карточке.
type SourceHistoryEntry struct {
	At     time.Time `json:"at"`
	Author string    `json:"author"`
	Text   string    `json:"text"`
}

// SourceHistory — история источника у карточки, по времени, и откуда
// карточка (external_source): под этим именем карточка её и показывает.
type SourceHistory struct {
	Source  string               `json:"source"`
	Entries []SourceHistoryEntry `json:"entries"`
	// Pending — историю ещё дотягивают (или дотянуть не успели).
	Pending bool `json:"pending"`
}

func readSourceHistory(ctx context.Context, tx pgx.Tx, cardID string) (*SourceHistory, error) {
	var source *string
	var at *time.Time
	if err := tx.QueryRow(ctx,
		`select external_source, source_history_at from cards where id = $1`, cardID).Scan(&source, &at); err != nil {
		return nil, err
	}
	if source == nil || *source == importer.SourceTable {
		return nil, nil
	}
	out := &SourceHistory{Source: *source, Entries: []SourceHistoryEntry{}, Pending: at == nil}
	rows, err := tx.Query(ctx, `
		select at, author, text from card_source_history
		 where card_id = $1 order by at, id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e SourceHistoryEntry
		if err := rows.Scan(&e.At, &e.Author, &e.Text); err != nil {
			return nil, err
		}
		out.Entries = append(out.Entries, e)
	}
	return out, rows.Err()
}

// writeSourceHistory пишет записи истории карточке. Автор — именем:
// история — рассказ о том, что было там, и человек мог не переехать.
func writeSourceHistory(ctx context.Context, tx pgx.Tx, orgID, boardID, cardID string, entries []importer.Comment) error {
	if len(entries) == 0 {
		return nil
	}
	var ats []time.Time
	var authors, texts []string
	for _, e := range entries {
		at := e.At
		if at.IsZero() {
			at = time.Now()
		}
		ats, authors, texts = append(ats, at), append(authors, e.AuthorName), append(texts, e.Text)
	}
	_, err := tx.Exec(ctx, `
		insert into card_source_history (org_id, board_id, card_id, at, author, text)
		select $1, $2, $3, a, w, t from unnest($4::timestamptz[], $5::text[], $6::text[]) as x(a, w, t)`,
		orgID, boardID, cardID, ats, authors, texts)
	return err
}

// SourceHistoryTarget — карточка, которой историю ещё надо дотянуть.
type SourceHistoryTarget struct {
	CardID     string
	ExternalID string
}

// SourceHistoryTargets — карточки доски из source без дотянутой истории.
func (s *Service) SourceHistoryTargets(ctx context.Context, orgID, actorID, boardID, source string) ([]SourceHistoryTarget, error) {
	var out []SourceHistoryTarget
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, external_id from cards
			 where board_id = $1 and external_source = $2
			   and source_history_at is null and archived_at is null
			 order by created_at`, boardID, source)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t SourceHistoryTarget
			if err := rows.Scan(&t.CardID, &t.ExternalID); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// AddSourceHistory дописывает карточке то, что источник отдал потом:
// реплики обсуждения и историю. Автор реплики ищется так же, как при
// переносе: по почте среди участников и по выбору в предпросмотре;
// не найден — реплика от переносящего с именем автора в начале.
// Карточка помечается дотянутой, и повтор её не тронет.
func (s *Service) AddSourceHistory(ctx context.Context, orgID, actorID, boardID, cardID, source, sourceName string,
	comments, history []importer.Comment) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Уже дотянута — другим прогоном, пока этот ждал YouGile.
		var done bool
		if err := tx.QueryRow(ctx,
			`select source_history_at is not null from cards where id = $1 and board_id = $2`,
			cardID, boardID).Scan(&done); err != nil {
			return err
		}
		if done {
			return nil
		}
		if len(comments) > 0 {
			people, err := authorsOf(ctx, tx, orgID, source, comments)
			if err != nil {
				return err
			}
			fromWord := i18n.Name(ctx, "из")
			var authors, texts []string
			var ats []time.Time
			for _, cm := range comments {
				author, text := people[cmp.Or(cm.AuthorKey, cm.AuthorEmail)], cm.Text
				if author == "" {
					author = actorID
					if cm.AuthorName != "" {
						text = fromWord + " " + sourceName + ": " + cm.AuthorName + "\n\n" + text
					}
				}
				at := cm.At
				if at.IsZero() {
					at = time.Now()
				}
				authors, texts, ats = append(authors, author), append(texts, text), append(ats, at)
			}
			if _, err := tx.Exec(ctx, `
				insert into card_comments (org_id, board_id, card_id, author_id, body, created_at)
				select $1, $2, $3, a, b, t
				  from unnest($4::uuid[], $5::text[], $6::timestamptz[]) as x(a, b, t)`,
				orgID, boardID, cardID, authors, texts, ats); err != nil {
				return err
			}
		}
		if err := writeSourceHistory(ctx, tx, orgID, boardID, cardID, history); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update cards set source_history_at = now() where id = $1`, cardID)
		return err
	})
}

// authorsOf — участник по ключу автора реплики: почта среди участников
// или выбор, сделанный в предпросмотре переноса этого источника.
func authorsOf(ctx context.Context, tx pgx.Tx, orgID, source string, comments []importer.Comment) (map[string]string, error) {
	out := map[string]string{}
	var keys []string
	for _, cm := range comments {
		if k := cmp.Or(cm.AuthorKey, cm.AuthorEmail); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		select lower(u.email), u.id from users u
		  join memberships m on m.user_id = u.id and m.org_id = $1
		 where lower(u.email) = any($2)`, orgID, keys)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k, id string
		if err := rows.Scan(&k, &id); err != nil {
			rows.Close()
			return nil, err
		}
		out[k] = id
	}
	rows.Close()
	rows, err = tx.Query(ctx, `
		select p.person_key, p.user_id::text from import_people p
		  join memberships m on m.user_id = p.user_id and m.org_id = $1
		 where p.org_id = $1 and p.source = $2 and p.action = 'match' and p.person_key = any($3)`,
		orgID, source, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, id string
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		out[k] = id
	}
	return out, rows.Err()
}
