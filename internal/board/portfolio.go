package board

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Карточка эпика на портфеле и дерево через доски (этап 33.5).
//
// Полоса эпика и так считает листья на всех досках (Subtree), но не
// говорит, где эта работа лежит и где она стоит. Этого не видно
// и из снимка портфеля: он знает только прямые части. Отсюда два
// ответа сервера: разбивка поддерева эпика по доскам — в снимке
// портфеля, и само дерево через доски — отдельным запросом, для вида
// «Дерево».

// TeamShare — часть поддерева эпика на одной доске: сколько там листьев,
// сколько сделано и сколько карточек стоит. Считаются листья, как
// у полосы: промежуточная фича — это её задачи, и доска с одними фичами
// без задач в них посчитана по фичам.
type TeamShare struct {
	BoardID   string `json:"boardId"`
	BoardName string `json:"boardName"`
	BoardKey  string `json:"boardKey"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	// Blocked — стоящих карточек этой доски на любой глубине: остановленная
	// фича останавливает и свои задачи, и узнать об этом надо на эпике.
	Blocked int `json:"blocked"`
}

// teams — разбивка поддеревьев карточек портфеля по доскам. Одним
// запросом на доску или на карточку, как subtrees: снимку и патчу негде
// разойтись. Доска самого эпика не считается — её работа видна
// на портфеле и так. Потомки на невидимых досках отсекаются политиками:
// значка закрытой доски нет, как нет и её листьев в полосе.
func teams(ctx context.Context, tx pgx.Tx, boardID, cardID *string) (map[string][]TeamShare, error) {
	out := map[string][]TeamShare{}
	rows, err := tx.Query(ctx, `
		with recursive tree(root, root_board, card, depth) as (
			select l.from_card, r.board_id, l.to_card, 1
			  from card_links l
			  join cards r on r.id = l.from_card and r.archived_at is null
			  join boards rb on rb.id = r.board_id and rb.level = 'portfolio'
			 where l.kind = 'subtask'
			   and ($1::uuid is null or r.board_id = $1)
			   and ($3::uuid is null or r.id = $3)
			union all
			select t.root, t.root_board, l.to_card, t.depth + 1
			  from tree t
			  join card_links l on l.from_card = t.card and l.kind = 'subtask'
			 where t.depth < $2
		),
		nodes as (
			select distinct on (t.root, c.id) t.root, t.root_board, c.board_id,
			       `+cardDone+` as done,
			       not exists (select 1 from card_links k
			                     join cards kc on kc.id = k.to_card and kc.archived_at is null
			                    where k.from_card = c.id and k.kind = 'subtask') as leaf,
			       exists (select 1 from card_blocks b
			                where b.card_id = c.id and b.unblocked_at is null) as blocked
			  from tree t
			  join cards c on c.id = t.card and c.archived_at is null
			 order by t.root, c.id, t.depth
		)
		select n.root, b.id, b.name, b.key,
		       count(*) filter (where n.leaf),
		       count(*) filter (where n.leaf and n.done),
		       count(*) filter (where n.blocked)
		  from nodes n
		  join boards b on b.id = n.board_id
		 where n.board_id <> n.root_board
		 group by n.root, b.id, b.name, b.key
		having count(*) filter (where n.leaf) > 0 or count(*) filter (where n.blocked) > 0
		 order by n.root, b.name`, boardID, MaxSubtaskDepth, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		var s TeamShare
		if err := rows.Scan(&root, &s.BoardID, &s.BoardName, &s.BoardKey, &s.Total, &s.Done, &s.Blocked); err != nil {
			return nil, err
		}
		out[root] = append(out[root], s)
	}
	return out, rows.Err()
}

// TreeNode — узел ветки через доски. Недоступный узел не пропадает,
// как и в пути до корня: у него только идентификатор и родитель. Ниже
// него видны лишь части, которые видны сами (связь подзадачи видна
// и со стороны части, 0068), — открытая работа под закрытой фичей
// не теряется.
type TreeNode struct {
	ID         string `json:"id"`
	ParentID   string `json:"parentId,omitempty"`
	Visible    bool   `json:"visible"`
	Number     string `json:"number,omitempty"`
	Title      string `json:"title,omitempty"`
	BoardID    string `json:"boardId,omitempty"`
	BoardName  string `json:"boardName,omitempty"`
	ColumnName string `json:"columnName,omitempty"`
	Done       bool   `json:"done"`
	Blocked    bool   `json:"blocked"`
}

// CardTree — ветка карточки вниз через все доски, корнем первым.
// Предел глубины тот же, что у дерева подзадач: цикл в связях, если он
// возник, обход не зациклит. Убранное в архив в ветку не входит, как
// и в полосу: иначе его не отличить от недоступного. Невидимый корень —
// ErrNotFound, как у пути: существование недоступного не подтверждается.
func (s *Service) CardTree(ctx context.Context, orgID, userID, cardID string) ([]TreeNode, error) {
	out := []TreeNode{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			with recursive down(card, parent, depth) as (
				select id, null::uuid, 0 from cards where id = $1 and archived_at is null
				union all
				select l.to_card, d.card, d.depth + 1
				  from down d join card_links l on l.from_card = d.card and l.kind = 'subtask'
				 where d.depth < $2
				   and not exists (select 1 from cards a where a.id = l.to_card and a.archived_at is not null)
			),
			first as (
				select distinct on (card) card, parent, depth from down order by card, depth
			)
			select f.card, coalesce(f.parent::text, ''), c.id is not null,
			       coalesce(c.number, ''), coalesce(c.title, ''),
			       coalesce(b.id::text, ''), coalesce(b.name, ''), coalesce(col.name, ''),
			       coalesce(`+cardDone+`, false),
			       exists (select 1 from card_blocks k where k.card_id = f.card and k.unblocked_at is null)
			  from first f
			  left join cards c on c.id = f.card
			  left join boards b on b.id = c.board_id
			  left join board_columns col on col.id = c.column_id
			 order by f.depth, c.number, f.card`, cardID, MaxSubtaskDepth)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TreeNode])
		if err != nil {
			return err
		}
		if len(out) == 0 || !out[0].Visible || out[0].ID != cardID {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}
