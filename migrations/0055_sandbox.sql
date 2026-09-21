-- Песочница публичного демо (ROADMAP 30.1).
--
-- Посетитель демо получает свою организацию с демонстрационными
-- данными и через сутки её теряет. Песочница — обычная организация
-- со сроком: отдельная схема или отдельная база означали бы второй
-- продукт, который однажды начнёт показывать не то, что получит
-- заказчик.
--
-- Пусто — настоящая организация; такие заводит и старая версия
-- приложения, так что в окне выкладки ничего не меняется.
alter table orgs add column sandbox_expires_at timestamptz;

create index orgs_sandbox_expiring on orgs (sandbox_expires_at)
    where sandbox_expires_at is not null;

-- Люди песочницы — её собственные: почта выдумана, пароля никто
-- не знает, и после организации им жить незачем. Ссылка на неё,
-- а не признак: тогда удаление организации уносит их тем же
-- каскадом, и отдельной уборки людей, которая однажды не доедет,
-- нет.
alter table users add column sandbox_org_id uuid references orgs (id) on delete cascade;

create index users_sandbox_idx on users (sandbox_org_id) where sandbox_org_id is not null;

-- --- Журнал не пишет об исчезающей организации ---
--
-- До этой миграции организацию не удалял никто, и триггер журнала
-- этого не предусматривал: каскад удалял состав, триггер записывал
-- «удалён участник» в журнал той самой организации, которая уже
-- удалена, — и вставка падала на внешнем ключе, а с ней и всё
-- удаление (проверено на стенде 21.09.2026). Запись о событии
-- организации, которой больше нет, некому и незачем читать: журнал
-- уходит тем же каскадом.
--
-- Проверка — существование строки организации, а не признак
-- песочницы: правило «о несуществующей организации не пишем» верно
-- для любой.
create or replace function audit_write() returns trigger
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

    if tg_op = 'DELETE'
       and not exists (select 1 from orgs where id = (doc ->> 'org_id')::uuid) then
        return null;
    end if;

    -- Секреты в журнал не попадают. Хеш токена приглашения — не «почти
    -- безопасное» значение: политика открывает строку приглашения именно
    -- по хешу, поэтому знание хеша равносильно знанию ссылки.
    doc  := doc  - 'token_hash' - 'password_hash';
    prev := prev - 'token_hash' - 'password_hash';

    insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
    values (
        (doc ->> 'org_id')::uuid,
        (select app_current_user()),
        lower(tg_op),
        tg_table_name,
        (doc ->> tg_argv[0])::uuid,
        case
            -- Исчезнувшее лежит под `old` и у удаления, и у изменения:
            -- разбирающему журнал не приходится помнить, что у одного
            -- действия ключи означают не то же, что у остальных.
            when tg_op = 'DELETE' then jsonb_build_object('old', doc)
            when prev is null      then jsonb_build_object('new', doc)
            else jsonb_build_object('new', doc, 'old', prev)
        end);
    return null;
end $$;

create or replace function audit_card_delete() returns trigger
language plpgsql as $$
begin
    -- Карточка, уехавшая вместе с доской, отдельной записи не получает:
    -- доска уже записана целиком, а пятьсот строк «удалена карточка»
    -- под ней — это утопленная лента, а не след. Уехавшая вместе
    -- с организацией — тем более.
    if exists (select 1 from boards where id = old.board_id)
       and exists (select 1 from orgs where id = old.org_id) then
        insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
        values (old.org_id, (select app_current_user()), 'delete', 'cards', old.id,
                jsonb_build_object('old', to_jsonb(old)));
    end if;
    return null;
end $$;
