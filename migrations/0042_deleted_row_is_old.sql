-- Удалённая строка в журнале называлась «новой».
--
-- `audit_write` кладёт снимок под ключ `new` всегда, а при удалении
-- новой строки нет — есть только исчезнувшая. Снаружи это читается
-- прямо наоборот: интеграция, разбирающая журнал, видит у события
-- `delete` поле `new` и вправе решить, что строка появилась.
-- Тот же ключ ставил и триггер удаления карточки из 0033.
--
-- Внутри это до сих пор не мешало: клиент читает обе стороны и берёт
-- ту, что есть. Но журнал — часть контракта, и «new» у удаления
-- в нём было ложью. Ставим `old`, как у изменения: там исчезнувшее
-- значение лежит именно под ним.

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
    -- под ней — это утопленная лента, а не след.
    if exists (select 1 from boards where id = old.board_id) then
        insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
        values (old.org_id, (select app_current_user()), 'delete', 'cards', old.id,
                jsonb_build_object('old', to_jsonb(old)));
    end if;
    return null;
end $$;

-- --- Прежние записи ---
--
-- Записи, сделанные до этой миграции, переносятся: иначе разбирающему
-- журнал пришлось бы читать обе формы и различать их по дате миграции —
-- то есть знать про эту миграцию. Ключ переставляется, значение
-- не трогается.
--
-- Политика арендатора снимается на время переноса. Миграция идёт под
-- ролью приложения, арендатор не выставлен, и `update` под force RLS
-- обновил бы ноль строк молча — этим уже дважды ломались правки данных
-- внутри миграций.
alter table audit_events no force row level security;

-- Проверяется предусловие, а не результат: счёт строк «сколько было»
-- идёт под теми же политиками и вернёт тот же ноль, что и сам перенос.
do $$
begin
    if (select relforcerowsecurity from pg_class
         where relname = 'audit_events' and relnamespace = 'public'::regnamespace) then
        raise exception 'перенос пошёл бы вхолостую: политики на audit_events ещё действуют';
    end if;
end $$;

update audit_events
   set payload = jsonb_build_object('old', payload -> 'new')
 where action = 'delete'
   and payload ? 'new';

alter table audit_events force row level security;
