-- Журнал действий своего поддерева для владельца подразделения.
--
-- Этап 31, срез 4. Журнал читали владелец организации и наблюдатель
-- всей организации (app_view_all). Владелец подразделения теперь
-- приглашает, назначает, удаляет насовсем и стирает — и не видел
-- следов ни своих действий, ни действий соседей по своей ветке.
--
-- Отдельной колонки «подразделение» у записи нет, но каждая запись
-- хранит строку целиком (payload.new и payload.old), и по ней узел
-- находится для всего, что относится к ветке:
--   teams                     — сам узел;
--   team_members, observers,
--   invites, team_admins      — team_id;
--   boards                    — team_id, кроме закрытых: их владелец
--                               подразделения не видит и в журнале;
--   cards (удаление)          — доска карточки.
-- Узел ищется по пути до корня в teams, а не по app_admin_teams():
-- та отбрасывает убранные в архив узлы, и история ветки пропадала бы
-- вместе с архивацией узла.
--
-- Записи уровня организации — участие, личность, ссылки для входа,
-- выгрузка, состав закрытых досок — к ветке не относятся и остаются
-- за владельцем. Исключение одно: своё. Владелец подразделения видит
-- записи, которые сделал сам, — стирание человека пишется записью
-- о личности, и без этого он не нашёл бы в журнале собственного
-- необратимого действия.
--
-- Совместимость со старой версией: политика только расширяет чтение,
-- триггер только добавляет записи с новым subject — старый клиент
-- покажет его названием таблицы.

-- --- Назначения тоже в журнал ---
--
-- 0018 завела team_admins без триггера: назначать мог один владелец,
-- и пропуск не был заметен. Теперь назначает и снимает владелец
-- подразделения (0073) — такое полномочие обязано оставлять след.
create trigger audit after insert or update or delete on team_admins
    for each row execute function audit_write('id');

-- Корни области передаются аргументом: политика вычисляет их один раз
-- на запрос, а не на каждую строку журнала.
create function app_audit_in_area(subject text, payload jsonb, actor uuid, roots uuid[])
returns boolean
language plpgsql stable parallel safe
as $fn$
declare
    doc  jsonb;
    team uuid;
begin
    if cardinality(roots) = 0 then
        return false;
    end if;
    if actor = (select app_current_user()) then
        return true;
    end if;
    foreach doc in array array[payload -> 'new', payload -> 'old'] loop
        continue when doc is null or jsonb_typeof(doc) <> 'object';
        team := null;
        if subject = 'teams' then
            team := (doc ->> 'id')::uuid;
        elsif subject in ('team_members', 'observers', 'invites', 'team_admins') then
            team := (doc ->> 'team_id')::uuid;
        elsif subject = 'boards' then
            continue when doc ->> 'visibility' = 'private';
            team := (doc ->> 'team_id')::uuid;
        elsif subject = 'cards' then
            select b.team_id into team
              from boards b
             where b.id = (doc ->> 'board_id')::uuid and b.visibility <> 'private';
        end if;
        if team is not null
           and exists (select 1 from teams t where t.id = team and t.ancestor_ids && roots) then
            return true;
        end if;
    end loop;
    return false;
end $fn$;

alter policy visible on audit_events
    using ((select app_view_all())
           or app_audit_in_area(subject, payload, actor_id, (select app_admin_roots())));
