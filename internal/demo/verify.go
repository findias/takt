package demo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

// Verify сверяет наполненную базу с тем, что о демонстрационных данных
// обещано в внутренняя-инструкция.md.
//
// Проверка нужна потому, что стенд — не украшение: на нём смотрят
// интерфейс глазами и снимают все снимки экранов, а «почти всякая ошибка
// вёрстки видна только на настоящей длине текста и настоящем числе
// меток». Данные, растерявшие половину обещанного, дают зелёный экран
// без блокировок, без чужих подзадач и без закрытой итерации — и проход
// глазами не находит того, ради чего затевался.
//
// Второй повод: стенд протухает. Сквозные сценарии и снимки ходят
// по этой же организации — снимают блокировки, заводят и отзывают
// приглашения, отмечают сделанное. 19 августа проход по интерфейсу
// наткнулся на причину блокировки «21e»: след чужого прогона, который
// выглядел как поломка приложения.
//
// Проверяются обещания, а не строки: «есть карточка с блокировкой
// и причиной», а не «у ПОСТ-4 написано то-то». Демонстрационные данные
// правятся часто, и проверка, пересказывающая их дословно, ломается
// на каждой правке, ничего не находя.
func Verify(ctx context.Context, db *store.Store) error {
	// Организация и люди — таблицы личности, они вне политик: их читают
	// до того, как известен арендатор.
	var orgID, ownerID string
	err := db.Pool.QueryRow(ctx, `
		select o.id, u.id from orgs o
		  join memberships m on m.org_id = o.id
		  join users u on u.id = m.user_id
		 where o.name = $1 and lower(u.email) = $2`, OrgName, People[0].Email).
		Scan(&orgID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("организации %q нет: наполните базу (make demo)", OrgName)
	}
	if err != nil {
		return err
	}

	var missing []string
	checks := []struct {
		promise string
		query   string
	}{
		{
			"люди четырёх ролей",
			`select count(distinct role) = 3 and count(*) >= 4 from memberships where org_id = $1`,
		},
		{
			"дерево подразделений в три уровня",
			`select coalesce(max(array_length(ancestor_ids, 1)), 0) >= 3
			   from teams where org_id = $1 and archived_at is null`,
		},
		{
			// Без него раздел «Убранные подразделения» не показывается,
			// и вид не попадает в набор снимков — то есть не смотрится
			// никем и никогда.
			"убранное подразделение",
			`select exists (select 1 from teams
			                where org_id = $1 and archived_at is not null)`,
		},
		{
			"три доски всех трёх видимостей",
			`select count(distinct visibility) = 3 from boards
			  where org_id = $1 and archived_at is null`,
		},
		{
			"карточки с оценкой",
			`select count(*) >= 3 from cards where org_id = $1 and estimate is not null`,
		},
		{
			"карточки с метками",
			`select count(distinct card_id) >= 2 from card_labels where org_id = $1`,
		},
		{
			"карточка с несколькими исполнителями",
			`select exists (select 1 from card_assignees where org_id = $1
			                 group by card_id having count(*) > 1)`,
		},
		{
			"своё поле со значением",
			`select exists (select 1 from card_field_values where org_id = $1)`,
		},
		{
			"карточка, заблокированная с причиной",
			`select exists (select 1 from card_blocks
			                 where org_id = $1 and unblocked_at is null and reason <> '')`,
		},
		{
			"подзадачи",
			`select count(*) >= 3 from card_links where org_id = $1 and kind = 'subtask'`,
		},
		{
			// Ради этого случая и заводились три доски: часть работы
			// живёт у соседей, и родитель обязан знать о ней.
			"подзадача на доске соседей",
			`select exists (
			   select 1 from card_links l
			     join cards parent on parent.id = l.from_card
			     join cards part on part.id = l.to_card
			    where l.org_id = $1 and l.kind = 'subtask'
			      and parent.board_id <> part.board_id)`,
		},
		{
			"обсуждение с веткой",
			`select exists (select 1 from card_comments where org_id = $1 and parent_id is not null)`,
		},
		{
			"закрытая и идущая итерации",
			`select count(*) filter (where closed_at is not null) >= 1
			   and count(*) filter (where closed_at is null) >= 1
			   from iterations where org_id = $1`,
		},
		{
			"архив карточек",
			`select count(*) >= 1 from cards where org_id = $1 and archived_at is not null`,
		},
		{
			// Без прошлого метрики потока считать не из чего: карточка,
			// законченная секунду назад, даёт время цикла в ноль дней.
			// Порог мягче обещанных трёх недель нарочно: проверка ловит
			// не глубину истории, а её отсутствие — журнал, целиком
			// случившийся в одну минуту. Ровно так он и выглядел, пока
			// сдвиг молча не работал.
			"история за прошлые недели, а не за одну минуту",
			`select exists (select 1 from card_events
			                 where org_id = $1 and at < now() - interval '14 days')`,
		},
	}

	// Всё остальное читается от имени владельца: у таблиц включён force
	// RLS, и запрос без арендатора честно ответит «ничего нет» на базе,
	// где всё на месте. Эта проверка на том и споткнулась первой же
	// своей версией — тем самым способом, о котором предупреждает
	// внутренняя-инструкция.md.
	err = db.InTenant(ctx, orgID, ownerID, func(tx pgx.Tx) error {
		for _, c := range checks {
			var ok bool
			if err := tx.QueryRow(ctx, c.query, orgID).Scan(&ok); err != nil {
				return fmt.Errorf("проверка «%s»: %w", c.promise, err)
			}
			if !ok {
				missing = append(missing, c.promise)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"демонстрационные данные разошлись с обещанным — не хватает: %s.\n"+
				"Стенд протухает от сквозных прогонов и снимков: они ходят по этой же "+
				"организации. Пересоберите его целиком: make stand",
			strings.Join(missing, "; "))
	}
	return nil
}
