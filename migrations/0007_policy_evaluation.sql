-- Политика считается один раз на запрос, а не один раз на строку.
--
-- Политика — это предикат, который планировщик подставляет в каждый запрос
-- к таблице. Записанный неудачно, он превращается в вызов функции на каждую
-- строку: чтение доски из ста карточек сто раз спрашивает базу «кто я и что
-- мне видно». Supabase измерил на этом разброс от 178 000 мс до 12 мс на
-- миллионе строк — цена одной пары скобок.
--
-- Лечится двумя приёмами, и оба про форму записи, а не про смысл:
--
--   1. Вызов функции внутри подзапроса — `(select app_current_org())` —
--      планировщик выносит в InitPlan: считает один раз и подставляет как
--      константу. Заодно предикат становится пригоден для индексного
--      условия: обычный вызов stable-функции в него не превращается,
--      потому что планировщик обязан считать его зависящим от строки.
--   2. Подзапрос не должен ссылаться на защищаемую строку. Коррелированный
--      `exists (… where tm.team_id = boards.team_id)` — это поиск на каждую
--      строку, причём с применением политик уже к team_members. Несвязанный
--      `team_id in (select … where user_id = я)` строит множество моих
--      команд один раз и дальше только проверяет вхождение.
--
-- Смысл не меняется ни в одной политике — меняется только форма. Ровно
-- поэтому миграция не трогает ни одного теста: весь набор проверок
-- видимости обязан пройти без правок, и это её единственное доказательство.
--
-- Что здесь НЕ лечится и лечиться не может: поиск по тексту. Оператор `~~`
-- (`like`, `ilike`) не помечен leakproof, поэтому под RLS планировщику
-- запрещено пускать его в индексное условие вперёд политики — GIN и GiST
-- окажутся бесполезны. Пометить оператор leakproof может только
-- суперпользователь, и это осознанное открытие ковровой дорожки: смысл
-- пометки в том, что функция ничего не сообщает об аргументах даже текстом
-- ошибки. Когда дойдёт до поиска по карточкам, его придётся строить
-- отдельным путём, а не надеяться на политики.

-- --- 1. Резолверы: вызовы внутри тоже считаются на каждую строку ---
--
-- Тело `app_writable_boards` — запрос по всем доскам, и `app_current_user()`
-- в нём вызывался на каждую доску. Здесь та же пара скобок и тот же переход
-- от коррелированного exists к несвязанному in.

create or replace function app_is_owner() returns boolean
language sql stable parallel safe
as $$
    select coalesce((select role = 'owner' from memberships
                      where org_id = (select app_current_org())
                        and user_id = (select app_current_user())), false)
$$;

create or replace function app_can_write() returns boolean
language sql stable parallel safe
as $$
    select coalesce((select role in ('owner', 'member') from memberships
                      where org_id = (select app_current_org())
                        and user_id = (select app_current_user())), false)
$$;

create or replace function app_view_all() returns boolean
language sql stable parallel safe
as $$
    select coalesce((select role = 'owner' or view_all from memberships
                      where org_id = (select app_current_org())
                        and user_id = (select app_current_user())), false)
$$;

create or replace function app_writable_boards() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(b.id), '{}')
      from boards b
     where (select app_can_write())
       and b.archived_at is null
       and (b.visibility = 'org'
            or b.team_id in (select tm.team_id from team_members tm
                              where tm.user_id = (select app_current_user()))
            or b.id in (select bm.board_id from board_members bm
                         where bm.user_id = (select app_current_user())))
$$;

-- --- 2. Изоляция арендатора ---
--
-- Самый горячий предикат в базе: он добавляется к каждому запросу к каждой
-- таблице с данными. `org_id = (select app_current_org())` сравнивает
-- колонку с константой, а значит ложится на индексы, у которых org_id идёт
-- первым столбцом.

do $$
declare t text;
begin
    foreach t in array array[
        'projects', 'boards', 'board_columns', 'cards',
        'card_events', 'operations', 'card_links', 'card_blocks',
        'teams', 'team_members', 'board_members', 'invites'
    ] loop
        execute format(
            'alter policy tenant_isolation on %I
               using (org_id = (select app_current_org()))
               with check (org_id = (select app_current_org()))', t);
    end loop;
end $$;

alter policy invite_by_token on invites
    using (token_hash = (select nullif(current_setting('app.invite_token', true), '')))
    with check (token_hash = (select nullif(current_setting('app.invite_token', true), '')));

-- --- 3. Видимость доски ---
--
-- Здесь коррелированных подзапросов было два, и оба разворачивались в поиск
-- на каждую доску. Порядок ветвей тоже не случаен: самая частая и самая
-- дешёвая — `visibility = 'org'` — стоит первой, до всякого обращения
-- к другим таблицам.

alter policy visible on boards using (
       visibility = 'org'
    or team_id in (select tm.team_id from team_members tm
                    where tm.user_id = (select app_current_user()))
    or id in (select bm.board_id from board_members bm
               where bm.user_id = (select app_current_user()))
    or (visibility <> 'private' and (select app_view_all())));

alter policy writable on boards
    using (id in (select unnest(app_writable_boards())))
    with check (id in (select unnest(app_writable_boards())));

alter policy creatable on boards with check ((select app_can_write()));

alter policy manage on projects
    using ((select app_can_write())) with check ((select app_can_write()));

-- --- 4. Всё, что висит на доске ---
--
-- `= any (массив)` заменён на `in (select unnest(массив))` не ради красоты:
-- так подзапрос становится несвязанным, планировщик считает его один раз
-- и складывает в хеш, а не разворачивает массив заново на каждой строке.

do $$
declare t text;
begin
    foreach t in array array['board_columns', 'cards', 'card_events', 'operations'] loop
        execute format(
            'alter policy visible on %I
               using (board_id in (select unnest(app_visible_boards())))', t);
    end loop;
    foreach t in array array['board_columns', 'cards'] loop
        execute format(
            'alter policy writable on %I
               using (board_id in (select unnest(app_writable_boards())))
               with check (board_id in (select unnest(app_writable_boards())))', t);
    end loop;
    foreach t in array array['card_events', 'operations'] loop
        execute format(
            'alter policy appendable on %I
               with check (board_id in (select unnest(app_writable_boards())))', t);
    end loop;
end $$;

alter policy finalizable on operations
    using (actor_id = (select app_current_user()))
    with check (actor_id = (select app_current_user()));

alter policy writable on card_links
    using (from_card in (select id from cards) and (select app_can_write()))
    with check (from_card in (select id from cards) and (select app_can_write()));

alter policy writable on card_blocks
    using (card_id in (select id from cards) and (select app_can_write()))
    with check (card_id in (select id from cards) and (select app_can_write()));

-- --- 5. Команды и состав досок ---

alter policy manage on teams
    using ((select app_is_owner())) with check ((select app_is_owner()));
alter policy manage on team_members
    using ((select app_is_owner())) with check ((select app_is_owner()));
alter policy manage on board_members
    using ((select app_is_owner())) with check ((select app_is_owner()));

alter policy visible on board_members
    using ((select app_is_owner()) or user_id = (select app_current_user()));
