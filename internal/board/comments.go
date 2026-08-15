package board

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Обсуждение карточки.
//
// Ветки глубиной в один уровень: ответ на ответ читать невозможно, и все,
// кто пробовал, к этому и пришли. Правка сохраняет прежний текст, удаление
// мягкое — на комментарий ссылаются ответы, и вырезав его, мы разорвали бы
// ветку, в которой отвечали живым людям.

var (
	ErrNotAuthor = errors.New("править и удалять можно только свои комментарии")
	// ErrTooDeep — ответ на ответ.
	ErrTooDeep = errors.New("глубина обсуждения ограничена одним уровнем")
)

type Comment struct {
	ID       string  `json:"id"`
	CardID   string  `json:"cardId"`
	ParentID *string `json:"parentId"`
	Author   *string `json:"author"`
	AuthorID *string `json:"authorId"`
	// Body пуст у удалённого комментария: строка остаётся ради ветки,
	// но читать её больше нечего.
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"createdAt"`
	EditedAt  *time.Time `json:"editedAt"`
	Deleted   bool       `json:"deleted"`
	Mentions  []string   `json:"mentions"`
}

// Comments отдаёт обсуждение карточки целиком, от старых к новым.
// Постранично оно не читается намеренно: обсуждение в сотни реплик —
// это разговор, который давно пора было увести в другое место.
func (s *Service) Comments(ctx context.Context, orgID, userID, cardID string) ([]Comment, error) {
	out := []Comment{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select c.id, c.card_id, c.parent_id, c.author_id, u.name,
			       case when c.deleted_at is null then c.body else '' end,
			       c.created_at, c.edited_at, c.deleted_at is not null,
			       coalesce(array_agg(m.user_id::text) filter (where m.user_id is not null), '{}')
			  from card_comments c
			  left join users u on u.id = c.author_id
			  left join card_comment_mentions m on m.comment_id = c.id
			 where c.card_id = $1
			 group by c.id, u.name
			 order by c.created_at`, cardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Comment
			if err := rows.Scan(&c.ID, &c.CardID, &c.ParentID, &c.AuthorID, &c.Author,
				&c.Body, &c.CreatedAt, &c.EditedAt, &c.Deleted, &c.Mentions); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) AddComment(ctx context.Context, orgID, actorID, boardID, cardID, body string, parentID *string, mentions []string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, badRequestf("пустой комментарий")
	}

	var c Comment
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Карточка должна быть на этой доске: иначе обсуждение можно было
		// бы приписать к чужой карточке по прямому идентификатору.
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from cards
			                where id = $1 and board_id = $2 and archived_at is null)`,
			cardID, boardID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}

		err := tx.QueryRow(ctx, `
			insert into card_comments (org_id, board_id, card_id, parent_id, author_id, body)
			values ($1, $2, $3, $4, $5, $6)
			returning id, card_id, parent_id, author_id, body, created_at`,
			orgID, boardID, cardID, parentID, actorID, body).
			Scan(&c.ID, &c.CardID, &c.ParentID, &c.AuthorID, &c.Body, &c.CreatedAt)
		if err != nil {
			return err
		}

		// Упоминания хранятся ссылками и только на участников организации:
		// позвать в обсуждение того, кого нет в организации, нельзя.
		c.Mentions = []string{}
		for _, userID := range mentions {
			tag, err := tx.Exec(ctx, `
				insert into card_comment_mentions (org_id, comment_id, user_id)
				select $1, $2, $3
				 where exists (select 1 from memberships
				                where org_id = $1 and user_id = $3)
				on conflict do nothing`, orgID, c.ID, userID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				c.Mentions = append(c.Mentions, userID)
			}
		}

		return logEvent(ctx, tx, orgID, boardID, cardID, actorID, "commented",
			nil, nil, map[string]any{"commentId": c.ID})
	})
	return c, translateComment(err)
}

// EditComment меняет текст, сохраняя прежний. Править можно только своё:
// чужой текст под чужим именем — это подлог, а не редактирование.
func (s *Service) EditComment(ctx context.Context, orgID, actorID, commentID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return badRequestf("пустой комментарий")
	}
	return s.changeOwn(ctx, orgID, actorID, commentID, func(ctx context.Context, tx pgx.Tx) (int64, error) {
		tag, err := tx.Exec(ctx, `
			update card_comments set body = $2
			 where id = $1 and author_id = $3 and deleted_at is null`,
			commentID, body, actorID)
		return tag.RowsAffected(), err
	})
}

// DeleteComment помечает комментарий удалённым. Строка остаётся: на неё
// ссылаются ответы, и вырезав её, мы разорвали бы ветку.
func (s *Service) DeleteComment(ctx context.Context, orgID, actorID, commentID string) error {
	return s.changeOwn(ctx, orgID, actorID, commentID, func(ctx context.Context, tx pgx.Tx) (int64, error) {
		tag, err := tx.Exec(ctx, `
			update card_comments set deleted_at = now(), deleted_by = $2
			 where id = $1 and author_id = $2 and deleted_at is null`,
			commentID, actorID)
		return tag.RowsAffected(), err
	})
}

// changeOwn выполняет изменение и, если оно ничего не задело, объясняет
// почему. «Не найдено» и «не ваше» — разные ответы: во втором случае
// человеку понятно, что делать.
func (s *Service) changeOwn(ctx context.Context, orgID, actorID, commentID string,
	change func(context.Context, pgx.Tx) (int64, error),
) error {
	return translateComment(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		affected, err := change(ctx, tx)
		if err != nil {
			return err
		}
		if affected > 0 {
			return nil
		}

		var author *string
		err = tx.QueryRow(ctx,
			`select author_id::text from card_comments where id = $1 and deleted_at is null`,
			commentID).Scan(&author)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if author != nil && *author != actorID {
			return ErrNotAuthor
		}
		return ErrNotFound
	}))
}

// Revisions отдаёт прежние версии текста, от свежих к старым. «Изменено»
// без прежнего текста бесполезно: спрашивают не «правил ли он», а «что
// там было написано до».
func (s *Service) Revisions(ctx context.Context, orgID, userID, commentID string) ([]string, error) {
	out := []string{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select body from card_comment_revisions
			 where comment_id = $1
			 order by replaced_at desc`, commentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var body string
			if err := rows.Scan(&body); err != nil {
				return err
			}
			out = append(out, body)
		}
		return rows.Err()
	})
	return out, err
}

func translateComment(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23514" && strings.Contains(pgErr.Message, "ответ на ответ"):
			return ErrTooDeep
		case pgErr.Code == "23514":
			return badRequestf("%s", pgErr.Message)
		case pgErr.Code == "23503":
			return ErrNotFound
		}
	}
	return err
}
