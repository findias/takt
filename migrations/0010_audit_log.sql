-- Административные действия оставляют след.
--
-- На доске журналируется всё: card_events пишет каждое перемещение,
-- переименование и блокировку. За пределами доски не журналируется ничего.
-- Кто выдал приглашение, кто поменял роль, кто перенёс отдел в другое
-- направление, кто сделал человека наблюдателем — этих ответов сейчас нет
-- и взять их неоткуда. Ровно эти вопросы задают при разборе инцидента,
-- и задают их всегда постфактум.
--
-- Поэтому журнал — того же класса решение, что и сам card_events:
-- не «удобно бы иметь», а «либо пишется с самого начала, либо не
-- существует». Разница только в том, что card_events отвечает на вопрос
-- «как шла работа», а этот — на вопрос «кто раздал доступ».
--
-- Три свойства, каждое обеспечено базой, а не обещанием:
--
--   1. ПИШЕТ ТРИГГЕР, А НЕ КОД. Запись, сделанная в обход приложения —
--      миграцией, скриптом, руками в psql, — попадёт в журнал так же,
--      как сделанная через API. Журнал, который ведёт приложение, молчит
--      ровно в тех случаях, ради которых он заводился.
--
--   2. ТОЛЬКО ДОПИСЫВАЕТСЯ. Политик update и delete нет вовсе, а значит
--      они запрещены по умолчанию. Переписать историю нельзя даже
--      владельцу организации.
--
--   3. ПОДПИСЬ НЕЛЬЗЯ ПОДДЕЛАТЬ. Политика вставки требует, чтобы автор
--      записи совпадал с личностью в области транзакции. Записать
--      действие от чужого имени невозможно.
--
-- Чего здесь намеренно НЕ сделано: запись не требует установленной
-- личности. Требование выглядело заманчиво — «изменить состав организации,
-- не назвавшись, нельзя», — но означало бы, что любая будущая миграция,
-- дописывающая членство или команды, падает целиком. Журнал, роняющий
-- обслуживание базы, снимут первым же коммитом.
--
-- Поэтому действие без личности записывается с пустым автором и остаётся
-- видимым как таковое. Назваться чужим именем при этом всё равно нельзя.

create table audit_events (
    id         bigserial primary key,
    org_id     uuid        not null references orgs (id) on delete cascade,
    -- null остаётся возможным только для действий, у которых человека
    -- действительно нет: миграции и служебные задачи. Политика вставки
    -- не даёт назваться чужим именем, но не запрещает не назваться вовсе.
    actor_id   uuid                 references users (id),
    action     text        not null
               constraint audit_events_action_valid
               check (action in ('insert', 'update', 'delete')),
    subject    text        not null,
    subject_id uuid,
    payload    jsonb       not null default '{}'::jsonb,
    at         timestamptz not null default now()
);

-- Лента организации читается от свежего к старому — это единственный
-- способ, которым её вообще смотрят.
create index audit_events_org_idx on audit_events (org_id, id desc);
-- «Что происходило вот с этой командой» — второй и последний вопрос.
create index audit_events_subject_idx
    on audit_events (org_id, subject, subject_id, id desc);

-- --- Запись ---

create function audit_write() returns trigger
language plpgsql as $$
declare
    doc  jsonb;
    prev jsonb;
begin
    if tg_op = 'DELETE' then
        doc := to_jsonb(old);
    else
        doc := to_jsonb(new);
        if tg_op = 'UPDATE' then
            prev := to_jsonb(old);
        end if;
    end if;

    -- Секреты в журнал не попадают. Хеш токена приглашения — не «почти
    -- безопасное» значение: политика открывает строку приглашения именно
    -- по хешу, поэтому знание хеша равносильно знанию ссылки. Журнал
    -- читают владелец и наблюдатель, и превращать его в раздачу
    -- действующих приглашений незачем.
    doc  := doc  - 'token_hash' - 'password_hash';
    prev := prev - 'token_hash' - 'password_hash';

    insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
    values (
        (doc ->> 'org_id')::uuid,
        (select app_current_user()),
        lower(tg_op),
        tg_table_name,
        (doc ->> tg_argv[0])::uuid,
        case when prev is null then jsonb_build_object('new', doc)
             else jsonb_build_object('new', doc, 'old', prev) end);
    return null;
end $$;

-- Аргумент триггера — колонка, которой подпись действия адресуется:
-- у членства это человек, у команды — она сама, у состава доски — доска.
create trigger audit after insert or update or delete on memberships
    for each row execute function audit_write('user_id');
create trigger audit after insert or update or delete on invites
    for each row execute function audit_write('id');
create trigger audit after insert or update or delete on teams
    for each row execute function audit_write('id');
create trigger audit after insert or update or delete on team_members
    for each row execute function audit_write('team_id');
create trigger audit after insert or update or delete on board_members
    for each row execute function audit_write('board_id');
create trigger audit after insert or update or delete on observers
    for each row execute function audit_write('user_id');

-- Доска попадает в журнал не вся: версия растёт на каждой операции, и
-- журналировать это значило бы утопить ленту. `update of` срабатывает на
-- упоминание колонки в SET, а обычные операции с доской её не упоминают,
-- поэтому в журнал попадает ровно смена хозяина и видимости.
create trigger audit_insert after insert or delete on boards
    for each row execute function audit_write('id');
create trigger audit_access after update of visibility, team_id on boards
    for each row execute function audit_write('id');

-- --- Доступ ---

alter table audit_events enable row level security;
alter table audit_events force  row level security;

-- Чтение — строго своя организация. Запись — своя организация либо
-- служебный контекст, в котором арендатор не выставлен вовсе: там org_id
-- берётся не из запроса, а из изменяемой строки, подделать его неоткуда.
create policy tenant_isolation on audit_events as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Читают журнал те же, кто видит организацию целиком: владелец и
-- наблюдатель всей организации. Наблюдателю поддерева журнал не открыт —
-- по нему видно всё, что происходит в организации, и сузить его до
-- поддерева нечестно: половина записей всё равно про людей, а не про
-- команды.
create policy visible on audit_events for select using ((select app_view_all()));

-- Подпись обязана совпадать с личностью в области транзакции. `is not
-- distinct from` вместо `=`: без личности запись возможна, но тогда
-- и автора у неё нет — назваться чужим именем нельзя ни при каком
-- значении.
create policy appendable on audit_events for insert
    with check (actor_id is not distinct from (select app_current_user()));

-- Политик update и delete нет намеренно: то, чего нет, запрещено.
