-- Подразделения: команда получает родителя, роль наследуется вниз,
-- наблюдение ограничивается поддеревом.
--
-- До этой миграции структура плоская: организация → команда → человек.
-- Для десяти команд этого хватает, для организации с направлениями и
-- отделами — нет: руководителю направления нужно видеть все команды под
-- собой и не нужно видеть соседнее направление.
--
-- Три решения, каждое взято у тех, кто это уже прошёл.
--
-- 1. ГЛУБИНА ОГРАНИЧЕНА ПЯТЬЮ УРОВНЯМИ. Linear ограничивает пятью, GitLab
--    допускает двадцать, но в собственной документации рекомендует «пять
--    или меньше» и прямо связывает глубину с деградацией. Предел здесь не
--    из осторожности: он превращает «сколько угодно предков» в массив
--    известной длины, по которому работает индекс.
--
-- 2. ДЕРЕВО ХРАНИТСЯ ДВАЖДЫ: parent_id — источник истины, ancestor_ids —
--    путь от корня до себя включительно, пересчитываемый триггером.
--    Это путь GitLab: они заменили рекурсивные запросы на массив
--    traversal_ids с GIN-индексом. Рекурсивный CTE внутри политики
--    планировщик не сплющивает, а массив ложится в индексное условие —
--    ровно то, за что мы боролись в двух прошлых миграциях.
--
--    Closure table рассматривалась и отвергнута: на запросах выигрыша
--    нет, а перенос поддерева стоит |поддерево| × |предки| строк вместо
--    одного UPDATE.
--
-- 3. НАБЛЮДЕНИЕ — ЗАПИСЬ, А НЕ ПРИЗНАК. Здесь мы расходимся с GitLab
--    сознательно. Их Auditor — тип пользователя, и поэтому он видит весь
--    инстанс: ограничить его подразделением нечем. Ограничить наблюдение
--    поддеревом умеет только Azure DevOps, и именно потому, что у них это
--    запись на узле дерева. Признак у пользователя неизбежно означает
--    «вся организация» — значит, нужна запись.
--
--    Заодно снимается ограничение, которое иначе всплыло бы сразу:
--    руководитель двух направлений выражается двумя строками, а флагом
--    не выражается никак.

-- --- 1. Команда получает родителя и путь ---

alter table teams
    add column parent_id uuid references teams (id) on delete restrict,
    -- Путь от корня до себя включительно. Корневая команда — массив из
    -- одного элемента, собственного идентификатора: так «мои команды и всё
    -- под ними» и «я сам» выражаются одним и тем же оператором пересечения.
    add column ancestor_ids uuid[] not null default '{}';

-- Индекс под оператор пересечения массивов: по нему считается и «все
-- потомки узла», и «команды человека вместе с их поддеревьями».
create index teams_ancestors_idx on teams using gin (ancestor_ids);
create index teams_parent_idx on teams (parent_id) where archived_at is null;

-- Уже существующие команды — корневые.
update teams set ancestor_ids = array[id];

alter table teams
    add constraint teams_depth_limited
    check (cardinality(ancestor_ids) between 1 and 5);

-- --- 2. Поддержание пути ---
--
-- Триггер, а не вычисление на лету: путь читается в каждой политике
-- видимости, а меняется только при переносе команды.

create function teams_path_before() returns trigger
language plpgsql as $$
declare
    parent_path uuid[];
begin
    if new.parent_id is null then
        new.ancestor_ids := array[new.id];
        return new;
    end if;

    select ancestor_ids into parent_path from teams where id = new.parent_id;
    if parent_path is null then
        raise exception 'родительская команда не найдена'
            using errcode = 'foreign_key_violation';
    end if;

    -- Цикл: узел не может оказаться среди собственных предков. База сама
    -- от этого не защищает — рекурсивный обход просто зациклился бы.
    if new.id = any (parent_path) then
        raise exception 'команда не может быть вложена в собственного потомка'
            using errcode = 'check_violation';
    end if;

    new.ancestor_ids := parent_path || new.id;

    -- Ограничение таблицы поймало бы это и само, но сообщением про
    -- нарушение check-констрейнта. Гарантия остаётся за ограничением,
    -- объяснение — здесь: до пользователя дойдёт именно оно.
    if cardinality(new.ancestor_ids) > 5 then
        raise exception 'глубина вложенности команд ограничена пятью уровнями'
            using errcode = 'check_violation';
    end if;
    return new;
end $$;

create trigger teams_path
    before insert or update of parent_id on teams
    for each row execute function teams_path_before();

-- Перенос узла переписывает путь всему поддереву. Одним запросом:
-- у потомка меняется только «голова» пути — до перенесённого узла
-- включительно, — а хвост под ним остаётся прежним.
create function teams_path_after() returns trigger
language plpgsql as $$
begin
    update teams d
       set ancestor_ids = new.ancestor_ids
                          || d.ancestor_ids[array_position(d.ancestor_ids, new.id) + 1 : ]
     where d.ancestor_ids @> array[new.id]
       and d.id <> new.id;

    -- Проверка глубины на самом узле уже сработала (ограничение таблицы),
    -- но перенос мог утопить поддерево целиком.
    if exists (select 1 from teams where cardinality(ancestor_ids) > 5) then
        raise exception 'перенос уводит поддерево глубже пяти уровней'
            using errcode = 'check_violation';
    end if;
    return null;
end $$;

-- `update of parent_id` срабатывает на упоминание колонки в SET, а запрос
-- выше её не упоминает — поэтому каскад не зацикливается.
create trigger teams_path_cascade
    after update of parent_id on teams
    for each row when (old.parent_id is distinct from new.parent_id)
    execute function teams_path_after();

-- --- 3. Наблюдатели ---
--
-- team_id = null означает всю организацию: это прежний view_all, только
-- выраженный записью. Человек может наблюдать несколько поддеревьев.

create table observers (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid        not null references orgs (id) on delete cascade,
    user_id    uuid        not null references users (id) on delete cascade,
    team_id    uuid                 references teams (id) on delete cascade,
    granted_by uuid                 references users (id),
    created_at timestamptz not null default now()
);

-- Два частичных индекса вместо одного уникального: null в уникальном
-- индексе не сравнивается сам с собой, и «наблюдатель всей организации»
-- завёлся бы дважды.
create unique index observers_org_wide_idx on observers (org_id, user_id)
    where team_id is null;
create unique index observers_team_idx on observers (org_id, user_id, team_id)
    where team_id is not null;
create index observers_user_idx on observers (user_id, org_id);

insert into observers (org_id, user_id, team_id)
select org_id, user_id, null from memberships where view_all;

alter table memberships drop column view_all;

alter table observers enable row level security;
alter table observers force  row level security;

create policy tenant_isolation on observers as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = (select app_current_org()));

-- Кто над кем поставлен наблюдателем — не тайна внутри организации:
-- скрытое наблюдение хуже отсутствия наблюдения.
create policy visible on observers for select using (true);
create policy manage  on observers for all
    using ((select app_is_owner())) with check ((select app_is_owner()));

-- --- 4. Резолверы ---

-- Команды человека вместе со всеми их поддеревьями.
--
-- Наследование идёт только вниз и только вверх по роли: состоящий
-- в «Разработке» состоит и во всех отделах под ней. Понизить роль в
-- дочерней команде нельзя — так у всех, кто это реализовал, и по той же
-- причине: правило «максимум из унаследованного» предсказуемо, а
-- «где-то ниже урезали» превращает разбор доступа в раскопки.
create function app_member_teams() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(t.id), '{}')
      from teams t
     where t.archived_at is null
       and t.ancestor_ids && (array(select tm.team_id from team_members tm
                                     where tm.user_id = (select app_current_user())))
$$;

-- Поддеревья, за которыми человек наблюдает.
create function app_observed_teams() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(t.id), '{}')
      from teams t
     where t.archived_at is null
       and t.ancestor_ids && (array(select o.team_id from observers o
                                     where o.user_id = (select app_current_user())
                                       and o.team_id is not null))
$$;

-- Владелец организации видит всё по должности; остальным нужна запись
-- наблюдателя без указания команды.
create or replace function app_view_all() returns boolean
language sql stable parallel safe
as $$
    select (select app_is_owner())
        or exists (select 1 from observers
                    where user_id = (select app_current_user())
                      and team_id is null)
$$;

create or replace function app_writable_boards() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(b.id), '{}')
      from boards b
     where (select app_can_write())
       and b.archived_at is null
       and (b.visibility = 'org'
            or b.team_id = any (array(select unnest(app_member_teams())))
            or b.id = any (array(select bm.board_id from board_members bm
                                  where bm.user_id = (select app_current_user()))))
$$;

-- --- 5. Видимость доски ---
--
-- Ветвей стало пять, и порядок по-прежнему от дешёвого к дорогому.
-- Наблюдение за поддеревом стоит рядом с наблюдением за организацией и
-- подчиняется тому же правилу: закрытая доска не открывается никому,
-- кроме вписанных поимённо.

alter policy visible on boards using (
       visibility = 'org'
    or team_id = any (array(select unnest(app_member_teams())))
    or id = any (array(select bm.board_id from board_members bm
                        where bm.user_id = (select app_current_user())))
    or (visibility <> 'private' and (select app_view_all()))
    or (visibility <> 'private'
        and team_id = any (array(select unnest(app_observed_teams())))));
