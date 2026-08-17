package board

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Одна карточка целиком — для того, кто пришёл снаружи.
//
// Внутри приложения такого чтения нет и не нужно: клиент держит снимок
// доски и знает про карточку всё. А интеграция приходит от вебхука,
// у неё на руках два идентификатора — доски и карточки, — и до сих пор
// узнать по ним хоть что-нибудь можно было только чтением снимка доски
// целиком. Пятьсот карточек ради одной — плохой ответ на вопрос
// «что там случилось».
//
// Отдаётся не то же, что лежит в снимке. Метки и исполнители названы
// целиком, а не идентификаторами: в снимке они разложены по словарям
// потому, что название метки иначе уехало бы столько раз, на скольких
// карточках она висит, — а здесь карточка одна, и идентификатор без
// имени отправил бы читателя за тем же снимком.

// CardDetail — карточка вместе с тем, что о ней спрашивают сразу же.
type CardDetail struct {
	Card
	BoardID string `json:"boardId"`
	// Метки и исполнители — целиком, с именами.
	Labels    []Label  `json:"labels"`
	Assignees []Person `json:"assignees"`
	// Связи обеими сторонами: и подзадачи этой карточки, и то, чьей
	// подзадачей она сама является. Названия связанных карточек сюда
	// не едут — за ними идут этим же вызовом, по одной.
	Links []Link `json:"links"`
}

// Card читает карточку доски. Недоступная доска и несуществующая
// карточка отвечают одинаково: существование недоступного мы
// не подтверждаем.
func (s *Service) Card(ctx context.Context, orgID, userID, boardID, cardID string) (CardDetail, error) {
	out := CardDetail{BoardID: boardID, Labels: []Label{}, Assignees: []Person{}, Links: []Link{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		// Прогресс и блокировка считаются тем же кодом, что и в снимке
		// доски: два ответа на «сколько сделано» разошлись бы в первый
		// же день.
		card, err := readCard(ctx, tx, boardID, cardID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Card = card

		if err := tx.QueryRow(ctx, `
			select count(*) from card_comments where card_id = $1`, cardID).
			Scan(&out.Comments); err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			select l.id, l.name, l.tone
			  from card_labels cl join labels l on l.id = cl.label_id
			 where cl.card_id = $1
			 order by l.name`, cardID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var l Label
			if err := rows.Scan(&l.ID, &l.Name, &l.Tone); err != nil {
				rows.Close()
				return err
			}
			out.Labels = append(out.Labels, l)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Порядок назначения сохраняется: первым остаётся тот, кто
		// взялся первым, и это несёт смысл.
		rows, err = tx.Query(ctx, `
			select u.id, u.name, u.email
			  from card_assignees a join users u on u.id = a.user_id
			 where a.card_id = $1
			 order by a.added_at, a.user_id`, cardID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p Person
			if err := rows.Scan(&p.UserID, &p.Name, &p.Email); err != nil {
				rows.Close()
				return err
			}
			out.Assignees = append(out.Assignees, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			select from_card, to_card, kind from card_links
			 where from_card = $1 or to_card = $1`, cardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l Link
			if err := rows.Scan(&l.FromCard, &l.ToCard, &l.Kind); err != nil {
				return err
			}
			out.Links = append(out.Links, l)
		}
		return rows.Err()
	})
	if err != nil {
		return CardDetail{}, err
	}
	return out, nil
}
