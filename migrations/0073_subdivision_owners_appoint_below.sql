-- Владелец подразделения назначает владельцев ниже себя.
--
-- Этап 31, решение владельца 26.09.2026. В 0018 раздавать полномочие
-- мог только владелец организации: «полномочие, размножающее само себя,
-- перестаёт быть ограниченным». Размножение и теперь ограничено —
-- назначать можно только в узел строго ниже своего: родитель узла
-- лежит в области назначающего. Свой узел и всё выше — нельзя, поэтому
-- область назначающего не растёт, а власть сверху не урезается:
-- владелец вложенного узла не снимет старшего, чей узел выше его.
--
-- Политика `manage` была `for all` и поэтому действовала и на чтение.
-- Новое условие зовёт app_admin_teams(), которая сама читает
-- team_admins: при `for all` чтение таблицы вызывало бы политику,
-- политика — функцию, функция — чтение, и так без конца. Чтение
-- и так открыто всем политикой `visible`, поэтому `manage` делится
-- на запись, удаление и правку, а чтения не касается вовсе.
--
-- Совместимость со старой версией: она назначает только владельцем
-- организации, а ему новые политики разрешают то же, что и прежде.

drop policy manage on team_admins;

create policy appoints on team_admins for insert
    with check (
           (select app_is_owner())
        or (select t.parent_id from teams t where t.id = team_admins.team_id)
               = any (array(select unnest(app_admin_teams())))
    );

create policy dismisses on team_admins for delete
    using (
           (select app_is_owner())
        or (select t.parent_id from teams t where t.id = team_admins.team_id)
               = any (array(select unnest(app_admin_teams())))
    );

-- Правки записи приложение не делает; оставлена владельцу, как было.
create policy amends on team_admins for update
    using ((select app_is_owner())) with check ((select app_is_owner()));
