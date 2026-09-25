package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/rank"
)

// Упорядочить колонку по итерации (ROADMAP 34.11, решение владельца
// 25.09.2026: разовая перестановка, а не режим показа).
//
// Очередь, в которую карточки кладут кто когда, превращается в список,
// где сверху то, что надо делать раньше: итерации по порядку начала —
// незакрытый хвост прошлых впереди, потом идущая, потом будущие, —
// а без итерации в конце. Внутри одной итерации порядок остаётся
// прежним: его кто-то уже расставил руками, и сортировка его не знает.
//
// Одна операция, а не перенос по карточке: двадцать переносов — это
// двадцать строк «перенесена» в истории и двадцать окон для чужой
// правки посередине. Порядок внутри колонки в метрики потока не входит,
// поэтому события на карточки не пишутся: колонка у карточек та же.
// Дальше порядок снова ручной — это перестановка, а не режим.

type sortColumnPayload struct {
	ColumnID string `json:"columnId"`
	By       string `json:"by"`
}

func sortColumn(ctx context.Context, tx pgx.Tx, _, boardID string, raw json.RawMessage) (Patch, error) {
	var p sortColumnPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Patch{}, badRequestf("разбор SORT_COLUMN: %v", err)
	}
	// Способ упорядочить назван явно, хотя он пока один: иначе второй
	// способ пришлось бы вводить, меняя смысл запроса без поля.
	if p.By != "iteration" {
		return Patch{}, badRequestf("упорядочить можно только по итерации (by: iteration)")
	}
	if _, err := loadColumn(ctx, tx, boardID, p.ColumnID); err != nil {
		return Patch{}, err
	}

	rows, err := tx.Query(ctx, `
		select c.id
		  from cards c
		  left join iteration_cards ic on ic.card_id = c.id and ic.removed_at is null
		  left join iterations i on i.id = ic.iteration_id
		 where c.column_id = $1 and c.archived_at is null
		 order by i.id is null, i.starts_on, i.ends_on, i.created_at, c.position
		 for update of c`, p.ColumnID)
	if err != nil {
		return Patch{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Patch{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Patch{}, err
	}

	keys, err := rank.NBetween("", "", len(ids))
	if err != nil {
		return Patch{}, fmt.Errorf("позиции колонки: %w", err)
	}
	// Уникальность позиции в колонке проверяется построчно, и новый ключ
	// одной карточки может совпасть со старым ключом соседки. Поэтому
	// сначала все уходят на временные ключи, заведомо разные, а потом
	// встают на свои. Временные живут только внутри транзакции.
	if _, err := tx.Exec(ctx, `
		update cards set position = '~' || id::text
		 where column_id = $1 and archived_at is null`, p.ColumnID); err != nil {
		return Patch{}, err
	}

	var patch Patch
	for i, id := range ids {
		c, err := scanCard(tx.QueryRow(ctx, `
			update cards set position = $2, version = version + 1
			 where id = $1
			 returning `+cardFields, id, keys[i]))
		if err != nil {
			return Patch{}, err
		}
		if err := completeCard(ctx, tx, &c); err != nil {
			return Patch{}, err
		}
		patch.Cards = append(patch.Cards, c)
	}
	return patch, nil
}
