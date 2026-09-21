-- Функции политик кешируют свой план: plpgsql вместо sql.
--
-- Замер 21.09.2026 на стенде: одна операция над доской — медиана 71 мс,
-- снимок доски из 30 карточек — 71 мс. Выполнение запросов было ни при
-- чём: в каждом плане стояли узлы по 3.4–3.7 мс — вызовы «какие доски
-- видит этот человек» (`app_visible_boards()` и соседей). Это
-- SQL-функции с агрегатом внутри; такие PostgreSQL не встраивает
-- и строит план их тела заново при каждом вызове — вместе
-- с развёрнутыми политиками досок, подразделений и состава. Вызовов
-- на снимок — десятки: каждая таблица с политикой спрашивает своё.
--
-- У plpgsql план тела кешируется на всё соединение. Тела не меняются
-- ни на символ: те же запросы, те же политики, та же видимость, —
-- меняется только то, сколько раз их планируют. После правки: операция
-- 15 мс, снимок 13 мс.
--
-- Почему это безопасно для изоляции. Функции по-прежнему вызываются
-- с правами вызывающего (security invoker), политики внутри них
-- действуют как прежде, а настройки области (`app.current_org`,
-- `app.current_user`) читаются при выполнении, а не зашиты в план.
-- Кешированный план PostgreSQL сбрасывает сам при смене роли и при
-- правке политик.
--
-- Функции, читающие одни настройки (`app_current_org()` и подобные),
-- остаются на sql: их PostgreSQL встраивает в запрос, и это быстрее
-- любого кеша. Правило держит `TestPolicyHelpersCachePlans`: функция
-- политик, которая читает таблицу, обязана быть plpgsql.
--
-- Со старой версией приложения совместимо: имена, аргументы, результаты
-- и смысл те же.

create or replace function app_admin_roots() returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce(array_agg(team_id), '{}')
      from team_admins
     where user_id = (select app_current_user())); end $fn$;

create or replace function app_admin_teams() returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce(array_agg(t.id), '{}')
      from teams t
     where t.archived_at is null
       and t.ancestor_ids && (select app_admin_roots())); end $fn$;

create or replace function app_can_write() returns boolean
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce((select role in ('owner', 'member') from memberships
                      where org_id = (select app_current_org())
                        and user_id = (select app_current_user())), false)); end $fn$;

create or replace function app_is_directory() returns boolean
language plpgsql stable parallel unsafe
as $fn$ begin return (select exists (
        select 1
          from api_clients
         where user_id = (select app_current_user())
           and org_id = (select app_current_org())
           and revoked_at is null
           and (expires_at is null or expires_at > now())
           and 'scim:write' = any (scopes)
    )); end $fn$;

create or replace function app_is_owner() returns boolean
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce((select role = 'owner' from memberships
                      where org_id = (select app_current_org())
                        and user_id = (select app_current_user())), false)); end $fn$;

create or replace function app_member_teams() returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce(array_agg(t.id), '{}')
      from teams t
     where t.archived_at is null
       and t.ancestor_ids && (array(select tm.team_id from team_members tm
                                     where tm.user_id = (select app_current_user())))); end $fn$;

create or replace function app_observed_teams() returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce(array_agg(t.id), '{}')
      from teams t
     where t.archived_at is null
       and t.ancestor_ids && (array(select o.team_id from observers o
                                     where o.user_id = (select app_current_user())
                                       and o.team_id is not null))); end $fn$;

create or replace function app_view_all() returns boolean
language plpgsql stable parallel safe
as $fn$ begin return (select (select app_is_owner())
        or exists (select 1 from observers
                    where user_id = (select app_current_user())
                      and team_id is null)); end $fn$;

create or replace function app_visible_boards() returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce(array_agg(id), '{}') from boards); end $fn$;

create or replace function app_writable_boards() returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select coalesce(array_agg(b.id), '{}')
      from boards b
     where (select app_can_write())
       and (b.visibility = 'org'
            or b.team_id = any (array(select unnest(app_member_teams())))
            or b.id = any (array(select bm.board_id from board_members bm
                                  where bm.user_id = (select app_current_user())))
            or (b.visibility <> 'private' and (select app_is_owner()))
            -- Доска подразделения, за которое человек отвечает.
            or (b.visibility <> 'private'
                and b.team_id = any (array(select unnest(app_admin_teams())))))); end $fn$;

create or replace function label_path(scope_team uuid, scope_board uuid) returns uuid[]
language plpgsql stable parallel safe
as $fn$ begin return (select case
        when scope_board is not null then
            coalesce((select t.ancestor_ids
                        from boards b join teams t on t.id = b.team_id
                       where b.id = scope_board), '{}') || scope_board
        when scope_team is not null then
            (select ancestor_ids from teams where id = scope_team)
        else '{}'::uuid[]
    end); end $fn$;
