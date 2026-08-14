-- Этап 0. Базовая схема.
--
-- Три решения здесь необратимы задним числом и заложены сразу:
--   1. cards.position — строковый ключ дробной индексации (LexoRank-подобный).
--      Перемещение карточки — ровно один UPDATE, и два одновременных
--      перетаскивания не ломают порядок.
--   2. card_events — append-only журнал. Из него считается вся аналитика
--      потока (cycle time, CFD, старение) и лента активности. Восстановить
--      историю переходов позже неоткуда.
--   3. org_id в каждой таблице. Сейчас бесплатно, потом — миграция всего.

create table orgs (
    id         uuid primary key default gen_random_uuid(),
    name       text        not null,
    created_at timestamptz not null default now()
);

create table users (
    id            uuid primary key default gen_random_uuid(),
    org_id        uuid        not null references orgs (id) on delete cascade,
    email         text        not null,
    name          text        not null,
    password_hash text        not null,
    role          text        not null default 'member',  -- owner | member | viewer
    created_at    timestamptz not null default now(),
    constraint users_role_valid check (role in ('owner', 'member', 'viewer'))
);
create unique index users_org_email_key on users (org_id, lower(email));

create table sessions (
    id         uuid primary key default gen_random_uuid(),
    user_id    uuid        not null references users (id) on delete cascade,
    expires_at timestamptz not null,
    created_at timestamptz not null default now()
);
create index sessions_user_idx on sessions (user_id);

create table projects (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    name        text        not null,
    created_at  timestamptz not null default now(),
    archived_at timestamptz
);
create index projects_org_idx on projects (org_id) where archived_at is null;

create table boards (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    project_id  uuid        not null references projects (id) on delete cascade,
    name        text        not null,
    -- монотонная версия доски: растёт на каждой операции, возвращается
    -- в снимке и в ответе 409, чтобы клиент понял, насколько он отстал
    version     bigint      not null default 1,
    created_at  timestamptz not null default now(),
    archived_at timestamptz
);
create index boards_project_idx on boards (project_id) where archived_at is null;

create table board_columns (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    board_id    uuid        not null references boards (id) on delete cascade,
    name        text        not null,
    position    text        not null,
    wip_limit   int,
    created_at  timestamptz not null default now(),
    archived_at timestamptz
);
create unique index board_columns_order_key on board_columns (board_id, position);

create table cards (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    board_id    uuid        not null references boards (id) on delete cascade,
    column_id   uuid        not null references board_columns (id) on delete cascade,
    position    text        not null,
    title       text        not null,
    description text        not null default '',
    version     bigint      not null default 1,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    archived_at timestamptz
);
-- уникальность позиции внутри колонки: страховка от коллизии ключей,
-- она же — индекс, по которому колонка читается уже отсортированной
create unique index cards_order_key on cards (column_id, position);
create index cards_board_idx on cards (board_id) where archived_at is null;

-- append-only. Ничего, кроме insert.
create table card_events (
    id          bigserial primary key,
    org_id      uuid        not null,
    board_id    uuid        not null,
    card_id     uuid        not null,
    actor_id    uuid,
    type        text        not null,  -- created | moved | renamed | archived | ...
    from_column uuid,
    to_column   uuid,
    payload     jsonb       not null default '{}'::jsonb,
    at          timestamptz not null default now()
);
create index card_events_board_idx on card_events (board_id, at);
create index card_events_card_idx on card_events (card_id, id);

-- идемпотентность: повтор операции с тем же operation_id возвращает
-- сохранённый результат вместо повторного применения
create table operations (
    operation_id uuid primary key,
    org_id       uuid        not null,
    actor_id     uuid,
    board_id     uuid        not null,
    kind         text        not null,
    result       jsonb       not null,
    at           timestamptz not null default now()
);
create index operations_at_idx on operations (at);
