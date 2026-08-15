-- Администратор подразделения.
--
-- Главный пробел, обнаруженный ещё когда структура организации только
-- вышла наружу: все политики раздачи доступа требуют владельца
-- организации. В дереве из пяти уровней это неработоспособно —
-- руководитель направления должен заводить команды под собой, вписывать
-- в них людей и назначать доски, и не должен трогать соседнее
-- направление.
--
-- Модель та же, что у наблюдения, и это не совпадение: и то, и другое —
-- полномочие над поддеревом, а не свойство человека. Ролью его выразить
-- нельзя, потому что человек бывает администратором одного направления
-- и рядовым участником другого, а ролей у него одна.
--
-- Администрирование включает наблюдение: администратор, который не видит
-- того, чем управляет, — недоразумение. Обратное неверно: наблюдатель
-- по-прежнему ничего не меняет.
--
-- Корневые подразделения по-прежнему заводит только владелец: у нового
-- корня нет предка, а значит нет и того, кто мог бы за него отвечать.

create table team_admins (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid not null references orgs (id) on delete cascade,
    user_id    uuid not null references users (id) on delete cascade,
    team_id    uuid not null references teams (id) on delete cascade,
    granted_by uuid references users (id),
    created_at timestamptz not null default now()
);

create unique index team_admins_idx on team_admins (org_id, user_id, team_id);
create index team_admins_user_idx on team_admins (user_id, org_id);

alter table team_admins enable row level security;
alter table team_admins force  row level security;

create policy tenant_isolation on team_admins as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Кто чем управляет — не тайна внутри организации: скрытая власть хуже,
-- чем отсутствие власти.
create policy visible on team_admins for select using (true);
-- Раздавать администрирование может только владелец организации.
-- Иначе администратор направления назначил бы администратора над собой
-- же или расширил свою область — полномочие, размножающее само себя,
-- перестаёт быть ограниченным.
create policy manage on team_admins for all
    using ((select app_is_owner())) with check ((select app_is_owner()));

-- --- Резолверы ---

-- Узлы, за которые человек отвечает напрямую.
create function app_admin_roots() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(team_id), '{}')
      from team_admins
     where user_id = (select app_current_user())
$$;

-- Они же вместе со всеми поддеревьями.
create function app_admin_teams() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(t.id), '{}')
      from teams t
     where t.archived_at is null
       and t.ancestor_ids && (select app_admin_roots())
$$;

-- --- Права администратора ---
--
-- Условие «узел внутри моей области» пишется через пересечение пути
-- с корнями: путь у узла уже есть, и это тот же приём, которым считается
-- наследование роли вниз.

alter policy manage on teams
    using ((select app_is_owner())
           or ancestor_ids && (select app_admin_roots()))
    with check ((select app_is_owner())
                or ancestor_ids && (select app_admin_roots()));

alter policy manage on team_members
    using ((select app_is_owner())
           or team_id = any (array(select unnest(app_admin_teams()))))
    with check ((select app_is_owner())
                or team_id = any (array(select unnest(app_admin_teams()))));

-- Состав закрытой доски по-прежнему раздаёт только владелец организации,
-- и это не забывчивость.
--
-- Условие «доска моего подразделения» пришлось бы писать через обращение
-- к boards, а политика boards обращается к board_members — Postgres видит
-- повторный вход в отношение и отвечает «infinite recursion detected in
-- policy». Разорвать петлю можно было бы денормализацией: держать команду
-- доски копией в board_members и синхронизировать триггером. Цена —
-- вторая копия связи, которая обязана не разъезжаться с первой; выгода —
-- одна операция над самым редким видом досок.
--
-- Пока обмен не в пользу денормализации. Записано в план: если закрытые
-- доски станут ходовыми, возвращаться сюда.

-- Наблюдение за поддеревом выдаёт тот, кто этим поддеревом управляет.
-- Наблюдение за всей организацией остаётся за владельцем: оно шире любой
-- области, и выдать его из области нельзя.
alter policy manage on observers
    using ((select app_is_owner())
           or (team_id is not null
               and team_id = any (array(select unnest(app_admin_teams())))))
    with check ((select app_is_owner())
                or (team_id is not null
                    and team_id = any (array(select unnest(app_admin_teams())))));

-- --- Доски области ---

create or replace function app_writable_boards() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(b.id), '{}')
      from boards b
     where (select app_can_write())
       and (b.visibility = 'org'
            or b.team_id = any (array(select unnest(app_member_teams())))
            or b.id = any (array(select bm.board_id from board_members bm
                                  where bm.user_id = (select app_current_user())))
            or (b.visibility <> 'private' and (select app_is_owner()))
            -- Доска подразделения, за которое человек отвечает.
            or (b.visibility <> 'private'
                and b.team_id = any (array(select unnest(app_admin_teams())))))
$$;

-- Видимость: администратор видит доски своей области, как и наблюдатель
-- за ней. Закрытая доска по-прежнему открывается только поимённо —
-- это единственное исключение, и оно намеренное.
alter policy visible on boards using (
       visibility = 'org'
    or team_id = any (array(select unnest(app_member_teams())))
    or id = any (array(select bm.board_id from board_members bm
                        where bm.user_id = (select app_current_user())))
    or (visibility <> 'private' and (select app_view_all()))
    or (visibility <> 'private'
        and team_id = any (array(select unnest(app_observed_teams()))))
    or (visibility <> 'private'
        and team_id = any (array(select unnest(app_admin_teams())))));
