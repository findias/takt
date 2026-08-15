-- Команды, видимость досок и наблюдатель всех команд.
--
-- До этой миграции видимость плоская: кто в организации — видит все доски.
-- Для одной команды это верно, для десяти — нет: доска найма и доска
-- инцидентов не должны быть видны всем поголовно.
--
-- Появляются две независимые вещи, и путать их нельзя:
--
--   команда   — кто с кем работает; доска принадлежит команде;
--   view_all  — способность видеть все командные доски, не участвуя в них.
--
-- view_all сделан признаком, а не ролью, по той же причине, по которой
-- GitLab сделал Auditor колонкой у пользователя, а не шестым уровнем
-- доступа: «видит всё» ортогонально «что меняет». Руководитель направления
-- обычно одновременно наблюдатель всех команд и обычный участник своей.
-- Ролью такое не выражается — пришлось бы заводить owner-observer,
-- member-observer, viewer-observer.

-- --- 1. Изоляция арендатора становится неотменяемой ---
--
-- Ловушка, из-за которой этот раздел идёт первым: политики из 0002 созданы
-- как PERMISSIVE, а несколько permissive-политик складываются по ИЛИ.
-- Добавить рядом политику видимости по доскам нельзя — она не сузила бы
-- доступ, а вернула бы его обратно ко всей организации. RESTRICTIVE
-- складываются по И, и никакой будущей политикой их не отменить.
--
-- Тип политики ALTER POLICY менять не умеет, поэтому drop/create. Внутри
-- транзакции миграции окна без политики нет.

do $$
declare t text;
begin
    foreach t in array array[
        'projects', 'boards', 'board_columns', 'cards',
        'card_events', 'operations', 'card_links', 'card_blocks'
    ] loop
        execute format('drop policy tenant_isolation on %I', t);
        execute format(
            'create policy tenant_isolation on %I as restrictive
               using (org_id = app_current_org())
               with check (org_id = app_current_org())', t);
    end loop;
end $$;

-- invites не трогаем: там две permissive-политики — по арендатору и по хешу
-- токена, — и складываться по ИЛИ для них правильно: приглашение открывают
-- до того, как известна организация.
--
-- Важное следствие: у таблиц выше не осталось ни одной permissive-политики,
-- а таблица только с restrictive-политиками не отдаёт ни строки. Поэтому
-- permissive-политики ниже обязательны, а не желательны.

-- --- 2. Личность в области видимости транзакции ---

create function app_current_user() returns uuid
language sql stable parallel safe
as $$ select nullif(current_setting('app.current_user', true), '')::uuid $$;

-- --- 3. Команды ---

create table teams (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    name        text        not null,
    created_at  timestamptz not null default now(),
    archived_at timestamptz
);
create index teams_org_idx on teams (org_id) where archived_at is null;

create table team_members (
    org_id   uuid        not null references orgs (id) on delete cascade,
    team_id  uuid        not null references teams (id) on delete cascade,
    user_id  uuid        not null references users (id) on delete cascade,
    role     text        not null default 'member'
             constraint team_members_role_valid check (role in ('lead', 'member')),
    added_at timestamptz not null default now(),
    primary key (team_id, user_id)
);
create index team_members_user_idx on team_members (user_id, org_id);

-- --- 4. У доски появляются хозяин и видимость ---
--
--   org     — видна всем в организации. Значение по умолчанию, поэтому
--             миграция ничего не прячет задним числом;
--   team    — видна своей команде и наблюдателям;
--   private — видна только поимённо. Наблюдатель её не видит: это
--             единственное исключение из «видит всё», и оно явное.

alter table boards
    add column team_id uuid references teams (id) on delete restrict,
    add column visibility text not null default 'org'
        constraint boards_visibility_valid
        check (visibility in ('org', 'team', 'private')),
    add constraint boards_team_required
        check (visibility <> 'team' or team_id is not null);

create index boards_team_idx on boards (org_id, team_id) where archived_at is null;

create table board_members (
    org_id   uuid        not null references orgs (id) on delete cascade,
    board_id uuid        not null references boards (id) on delete cascade,
    user_id  uuid        not null references users (id) on delete cascade,
    added_at timestamptz not null default now(),
    primary key (board_id, user_id)
);
create index board_members_user_idx on board_members (user_id, org_id);

-- --- 5. Наблюдатель всех команд ---

alter table memberships add column view_all boolean not null default false;

-- --- 6. Резолверы ---
--
-- Все STABLE и без аргументов — это не стиль, а условие пригодности:
-- такую функцию планировщик вычисляет один раз на запрос, а не на строку.

create function app_is_owner() returns boolean
language sql stable parallel safe
as $$
    select coalesce((select role = 'owner' from memberships
                      where org_id = app_current_org()
                        and user_id = app_current_user()), false)
$$;

create function app_can_write() returns boolean
language sql stable parallel safe
as $$
    select coalesce((select role in ('owner', 'member') from memberships
                      where org_id = app_current_org()
                        and user_id = app_current_user()), false)
$$;

-- Владелец организации видит всё по должности; остальным нужен признак.
create function app_view_all() returns boolean
language sql stable parallel safe
as $$
    select coalesce((select role = 'owner' or view_all from memberships
                      where org_id = app_current_org()
                        and user_id = app_current_user()), false)
$$;

-- Видимые доски. Функция читает boards, а на boards уже стоит политика
-- видимости — поэтому правило описано ровно один раз и не может разъехаться
-- между таблицами.
--
-- Условие, на котором это держится: политика boards ссылается на teams,
-- team_members, board_members и memberships, и ни одна из них не ссылается
-- обратно на boards или на саму себя. Нарушение стоит дорого и проявляется
-- не там, где сделано: политика board_members, заглянувшая в board_members,
-- роняет «infinite recursion detected in policy» на создании доски. Стоит
-- политике cards попасть сюда же — тем же способом сломается всё. Охраняется
-- тестом TestBoardVisibilityPoliciesDoNotRecurse.
create function app_visible_boards() returns uuid[]
language sql stable parallel safe
as $$ select coalesce(array_agg(id), '{}') from boards $$;

create function app_writable_boards() returns uuid[]
language sql stable parallel safe
as $$
    select coalesce(array_agg(b.id), '{}')
      from boards b
     where app_can_write()
       and b.archived_at is null
       and (b.visibility = 'org'
            or (b.visibility = 'team' and exists (
                    select 1 from team_members tm
                     where tm.team_id = b.team_id and tm.user_id = app_current_user()))
            or exists (select 1 from board_members bm
                        where bm.board_id = b.id and bm.user_id = app_current_user()))
$$;

-- --- 7. Политики ---

alter table teams         enable row level security;
alter table teams         force  row level security;
alter table team_members  enable row level security;
alter table team_members  force  row level security;
alter table board_members enable row level security;
alter table board_members force  row level security;

create policy tenant_isolation on teams as restrictive
    using (org_id = app_current_org()) with check (org_id = app_current_org());
create policy tenant_isolation on team_members as restrictive
    using (org_id = app_current_org()) with check (org_id = app_current_org());
create policy tenant_isolation on board_members as restrictive
    using (org_id = app_current_org()) with check (org_id = app_current_org());

-- Состав команд не прячем: кто с кем работает — не секрет. Секрет — то,
-- что на досках.
create policy visible on teams        for select using (true);
create policy visible on team_members for select using (true);

-- Состав закрытой доски виден владельцу организации и самому участнику —
-- список участников доски найма сам по себе становится утечкой.
--
-- Напрашивалась третья ветка, «и всем, кто состоит в той же доске». Её
-- здесь быть не может: подзапрос к board_members внутри политики самой
-- board_members Postgres встречает повторным применением этой же политики
-- и отвечает «infinite recursion detected in policy» — причём не на
-- запросе к board_members, а на создании любой доски, потому что политика
-- boards заглядывает сюда. Обойти нечем: security definer не спасает,
-- force row level security действует и на владельца таблицы, а роль
-- у приложения одна.
--
-- Ограничение при этом в ту сторону, в которую ошибаться безопасно: строк
-- видно меньше, а не больше. Политике доски этого достаточно — она
-- спрашивает только «состою ли в ней я», то есть смотрит на собственные
-- строки. Состав чужой доски пусть отдаёт отдельная операция, где решение
-- показать участников принимается явно.
create policy visible on board_members for select
    using (app_is_owner() or user_id = app_current_user());

-- Раздачей доступа распоряжается только владелец организации.
--
-- Это важнее, чем кажется: если разрешить это всякому, кто может писать,
-- рядовой участник впишет себя в закрытую доску одной операцией, и вся
-- модель видимости перестанет что-либо значить.
create policy manage on teams         for all using (app_is_owner()) with check (app_is_owner());
create policy manage on team_members  for all using (app_is_owner()) with check (app_is_owner());
create policy manage on board_members for all using (app_is_owner()) with check (app_is_owner());

-- Доска: правило видимости живёт здесь и только здесь.
create policy visible on boards for select using (
       visibility = 'org'
    or (visibility = 'team' and exists (
           select 1 from team_members tm
            where tm.team_id = boards.team_id and tm.user_id = app_current_user()))
    or exists (select 1 from board_members bm
                where bm.board_id = boards.id and bm.user_id = app_current_user())
    or (visibility <> 'private' and app_view_all()));

-- Кто пишет в доску, тот её и переименовывает; версия доски растёт на
-- каждой операции — это тот же update.
--
-- Из политики visible выше следует правило, которого в этих трёх строках
-- не видно, а оно определяет всю работу с видимостью: Postgres проверяет
-- политику select и на новом ряде тоже, поэтому доску нельзя перевести
-- в состояние, в котором сам её не видишь. Практически это значит, что
-- закрыть доску можно только вокруг себя: сперва впиши участников,
-- включая себя, потом ставь private. Иначе доска осталась бы видна одним
-- лишь вписанным, а вернуть её было бы некому — редактировать невидимую
-- доску не может никто, включая владельца организации.
--
-- Отказ приходит как «new row violates row-level security policy», то есть
-- как сбой сервера. Прикладной код, когда до видимости дойдут руки, обязан
-- проверить это сам и ответить внятно.
create policy writable on boards for update
    using (id = any (app_writable_boards()))
    with check (id = any (app_writable_boards()));
create policy creatable on boards for insert with check (app_can_write());

-- Проект — это папка; секреты лежат на досках.
create policy visible on projects for select using (true);
create policy manage  on projects for all using (app_can_write()) with check (app_can_write());

-- Всё, что висит на доске, наследует её видимость.
do $$
declare t text;
begin
    foreach t in array array['board_columns', 'cards', 'card_events', 'operations'] loop
        execute format(
            'create policy visible on %I for select
               using (board_id = any (app_visible_boards()))', t);
    end loop;
    foreach t in array array['board_columns', 'cards'] loop
        execute format(
            'create policy writable on %I for all
               using (board_id = any (app_writable_boards()))
               with check (board_id = any (app_writable_boards()))', t);
    end loop;
    -- Журналы только пополняются: политик update и delete нет, значит они
    -- запрещены по умолчанию. Append-only держится базой, а не обещанием.
    foreach t in array array['card_events', 'operations'] loop
        execute format(
            'create policy appendable on %I for insert
               with check (board_id = any (app_writable_boards()))', t);
    end loop;
end $$;

-- Результат операции дописывается после её выполнения — это единственное
-- изменение строки журнала операций, и оно разрешено только автору.
create policy finalizable on operations for update
    using (actor_id = app_current_user())
    with check (actor_id = app_current_user());

-- Связи и блокировки — по видимости карточки-источника.
create policy visible on card_links for select
    using (from_card in (select id from cards));
create policy writable on card_links for all
    using (from_card in (select id from cards) and app_can_write())
    with check (from_card in (select id from cards) and app_can_write());

create policy visible on card_blocks for select
    using (card_id in (select id from cards));
create policy writable on card_blocks for all
    using (card_id in (select id from cards) and app_can_write())
    with check (card_id in (select id from cards) and app_can_write());

-- --- 8. Индексы под политику ---
--
-- Политика добавляется к каждому запросу, поэтому у неё должен быть свой
-- путь доступа, а не только у прикладного условия.

create index cards_board_plain_idx    on cards (board_id);
create index board_columns_board_idx  on board_columns (board_id);
create index operations_board_idx     on operations (board_id);
