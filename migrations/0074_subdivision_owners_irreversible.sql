-- Необратимое в своём поддереве: удалить доску и карточку насовсем,
-- стереть данные человека.
--
-- Этап 31, решение владельца 26.09.2026. В 0033 необратимое держал один
-- владелец организации («полномочие администратора — про то, как идёт
-- работа, а не про то, чтобы стереть её следы»). Теперь администратор
-- области — владелец подразделения, и владелец решил дать ему все три
-- действия, но только внутри его поддерева. Защита от ошибки остаётся
-- прежней и не зависит от того, кто действует: доска удаляется только
-- из архива и с набранным именем, карточка — после вопроса, всё
-- записывается в журнал.
--
-- Совместимость со старой версией: она пускает к удалению только
-- владельца организации (проверка в обработчике), а ему новые политики
-- разрешают ровно то же, что прежние.

-- --- Кто распоряжается доской ---
--
-- Владелец организации — любой, владелец подразделения — доской своего
-- поддерева, кроме закрытой: закрытую он и не видит, она открывается
-- поимённо (0018), и стирать невидимое было бы странно.
-- plpgsql, а не sql: функция читает таблицу и зовётся из политик,
-- план её тела должен кешироваться (0054, TestPolicyHelpersCachePlans).
create function app_runs_board(b uuid) returns boolean
language plpgsql stable parallel safe
as $fn$ begin return (
    select (select app_is_owner())
        or exists (select 1 from boards
                    where id = b
                      and visibility <> 'private'
                      and team_id = any (array(select unnest(app_admin_teams()))))); end $fn$;

-- Условие пишется по своим колонкам, а не через app_runs_board: функция
-- читала бы boards из политики boards.
alter policy removable on boards
    using (archived_at is not null
           and ((select app_is_owner())
                or (visibility <> 'private'
                    and team_id = any (array(select unnest(app_admin_teams()))))));

alter policy removable_by_owner on cards using ((select app_runs_board(board_id)));
alter policy removable_by_owner on cards rename to removable_by_runner;

alter policy prunable on card_events using ((select app_runs_board(board_id)));

-- --- Кого можно стереть ---
--
-- Личность и участие в организации политик не имеют (users, memberships,
-- sessions — таблицы без RLS), поэтому право здесь — функция, которую
-- спрашивает код, и она же стоит в политике приглашений: стирание
-- обещает не оставить почту нигде, в том числе в приглашениях вне
-- области стирающего.
--
-- Владелец подразделения стирает человека, который весь внутри его
-- поддерева: состоит хотя бы в одном узле и ни в одном вне его. Человек
-- без узлов не «внутри» — иначе любой владелец подразделения стёр бы
-- любого новичка организации. Не владельца организации, не себя и не
-- владельца узла, который ему не подчинён (свой уровень и выше).
create function app_can_erase(u uuid) returns boolean
language plpgsql stable parallel safe
as $fn$ begin return (
    select (select app_is_owner())
        or (u <> (select app_current_user())
            and exists (select 1 from memberships m
                         where m.org_id = (select app_current_org())
                           and m.user_id = u
                           and m.role in ('member', 'viewer'))
            and exists (select 1 from team_members tm where tm.user_id = u)
            and not exists (select 1 from team_members tm
                             where tm.user_id = u
                               and not (tm.team_id = any (array(select unnest(app_admin_teams())))))
            -- coalesce не для красоты: у корня parent_id пуст, сравнение
            -- с пустым даёт не «ложь», а «неизвестно», и владелец корня
            -- молча выпадал из запрета — поймано проверкой.
            and not exists (select 1 from team_admins a join teams t on t.id = a.team_id
                             where a.user_id = u
                               and not coalesce(t.parent_id = any (array(select unnest(app_admin_teams()))), false)))); end $fn$;

alter policy managed on invites
    using (
           token_hash = (select nullif(current_setting('app.invite_token', true), ''))
        or (select app_no_actor())
        or (select app_is_owner())
        or team_id = any (array(select unnest(app_admin_teams())))
        -- Приглашения того, кого стирающий вправе стереть: иначе почта
        -- осталась бы в приглашении вне его области.
        or exists (select 1 from users u
                    where (u.id = invites.accepted_by or lower(u.email) = lower(invites.email))
                      and app_can_erase(u.id))
    );
