// Package export — выгрузка всех данных организации одним потоком.
//
// Вопрос, который задают при покупке, звучит не «есть ли у вас экспорт»,
// а «сможем ли мы уйти». Ответ на него — файл, из которого видно всё,
// что организация накопила, без нашего участия и без обращения в поддержку.
//
// Три решения, которые стоит объяснить.
//
// ПОД ТЕМИ ЖЕ ПОЛИТИКАМИ. Выгрузка идёт обычной арендаторской
// транзакцией от имени владельца. Значит, чужая организация в файл
// не попадёт не потому, что мы аккуратно написали условия, а потому,
// что база их не отдаст. Условие `org_id = $1` в каждом запросе оставлено
// вторым слоем и как объяснение читателю, а не как защита.
//
// ИМЕНА ПОЛЕЙ — КАК В БАЗЕ. Остальной API отдаёт camelCase, здесь
// snake_case, и это не небрежность: выгрузка — снимок схемы, а не её
// пересказ. Переименовать поля значит завести вторую схему, которую
// придётся поддерживать наравне с первой и чинить при каждой миграции.
//
// ПОТОКОМ, А НЕ ЦЕЛИКОМ В ПАМЯТИ. У организации с историей карточек
// больше, чем всего остального вместе, и собирать документ в памяти
// значит однажды получить отказ ровно у того заказчика, ради которого
// экспорт и делался.
package export

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

// Version — версия формата выгрузки. Тот, кто её читает, должен уметь
// узнать, что формат сменился, не гадая по содержимому.
const Version = 1

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

// section — раздел выгрузки: имя в документе и запрос, отдающий по одной
// строке jsonb на запись.
type section struct {
	name string
	sql  string
}

// Секреты не выгружаются никогда. Подпись вебхука и хеш ключа доступа —
// не данные организации, а средства проверки: восстановить по ним нечего,
// а утечка файла превращается в утечку доступа. Пароли не выгружаются
// по той же причине и вдобавок принадлежат человеку, а не организации.
var sections = []section{
	{"people", `
		select to_jsonb(x) from (
			select u.email, u.name, m.role, m.created_at
			  from memberships m join users u on u.id = m.user_id
			 where m.org_id = $1 order by u.email) x`},
	{"invites", `
		select to_jsonb(i) - 'token_hash' from invites i
		 where org_id = $1 order by created_at`},
	{"teams", `select to_jsonb(t) from teams t where org_id = $1 order by created_at`},
	{"team_members", `
		select to_jsonb(t) from team_members t where org_id = $1 order by added_at`},
	{"team_admins", `
		select to_jsonb(t) from team_admins t where org_id = $1 order by created_at`},
	{"observers", `select to_jsonb(o) from observers o where org_id = $1 order by created_at`},
	{"projects", `select to_jsonb(p) from projects p where org_id = $1 order by created_at`},
	{"boards", `select to_jsonb(b) from boards b where org_id = $1 order by created_at`},
	{"board_columns", `
		select to_jsonb(c) from board_columns c where org_id = $1 order by board_id, position`},
	{"board_members", `
		select to_jsonb(m) from board_members m where org_id = $1 order by board_id, added_at`},
	{"card_fields", `
		select to_jsonb(f) from card_fields f where org_id = $1 order by created_at`},
	{"cards", `select to_jsonb(c) from cards c where org_id = $1 order by board_id, position`},
	{"card_field_values", `
		select to_jsonb(v) from card_field_values v where org_id = $1 order by card_id, field_id`},
	{"card_links", `select to_jsonb(l) from card_links l where org_id = $1 order by created_at`},
	{"card_blocks", `select to_jsonb(b) from card_blocks b where org_id = $1 order by blocked_at`},
	{"card_comments", `
		select to_jsonb(c) from card_comments c where org_id = $1 order by created_at`},
	{"card_comment_revisions", `
		select to_jsonb(r) from card_comment_revisions r where org_id = $1 order by replaced_at`},
	{"card_comment_mentions", `
		select to_jsonb(m) from card_comment_mentions m where org_id = $1 order by comment_id`},
	{"iterations", `select to_jsonb(i) from iterations i where org_id = $1 order by created_at`},
	{"iteration_cards", `
		select to_jsonb(c) from iteration_cards c where org_id = $1 order by added_at`},
	{"card_events", `select to_jsonb(e) from card_events e where org_id = $1 order by id`},
	{"webhooks", `select to_jsonb(w) - 'secret' from webhooks w where org_id = $1 order by created_at`},
	{"api_clients", `
		select to_jsonb(c) - 'token_hash' from api_clients c where org_id = $1 order by created_at`},
}

// auditSection выгружается по отдельной просьбе: журнал обычно больше
// всех остальных разделов вместе взятых, и класть его в каждую выгрузку
// значит сделать выгрузку неподъёмной для тех, кому нужны карточки.
var auditSection = section{"audit_events", `
	select to_jsonb(a) from audit_events a where org_id = $1 order by id`}

// Dump пишет выгрузку в w. Ошибка после первого записанного байта
// неисправима: заголовки уже ушли, и обрыв виден получателю только
// как испорченный JSON. Поэтому документ и заканчивается закрывающей
// скобкой — оборванный файл не разберётся, и это правильно: молча
// принять половину выгрузки хуже, чем не принять никакой.
func (s *Service) Dump(ctx context.Context, w io.Writer, orgID, userID string, withAudit bool) error {
	out := bufio.NewWriterSize(w, 64<<10)

	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		// Выгрузка — то самое действие, о котором служба безопасности
		// спрашивает «кто и когда». Триггеры её не поймают: это чтение,
		// а не изменение, поэтому запись в журнал делается руками.
		// Слово «insert» здесь навязано ограничением на набор действий
		// и означает «появилась запись о выгрузке», а не изменение данных.
		if _, err := tx.Exec(ctx, `
			insert into audit_events (org_id, actor_id, action, subject, payload)
			values ($1, $2, 'insert', 'export', jsonb_build_object('audit', $3::boolean))`,
			orgID, userID, withAudit); err != nil {
			return err
		}

		var org []byte
		if err := tx.QueryRow(ctx,
			`select to_jsonb(o) from orgs o where o.id = $1`, orgID).Scan(&org); err != nil {
			return err
		}

		stamp, err := json.Marshal(time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		fmt.Fprintf(out, `{"version":%d,"exported_at":%s,"org":%s`, Version, stamp, org)

		list := sections
		if withAudit {
			list = append(append([]section{}, sections...), auditSection)
		}
		for _, sec := range list {
			if err := writeSection(ctx, tx, out, sec, orgID); err != nil {
				return fmt.Errorf("раздел %s: %w", sec.name, err)
			}
		}
		_, err = out.WriteString("}")
		return err
	})
	if err != nil {
		return err
	}
	return out.Flush()
}

func writeSection(ctx context.Context, tx pgx.Tx, out *bufio.Writer, sec section, orgID string) error {
	if _, err := fmt.Fprintf(out, `,"%s":[`, sec.name); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, sec.sql, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()

	first := true
	for rows.Next() {
		var row []byte
		if err := rows.Scan(&row); err != nil {
			return err
		}
		if !first {
			if _, err := out.WriteString(","); err != nil {
				return err
			}
		}
		first = false
		if _, err := out.Write(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = out.WriteString("]")
	return err
}
