-- Мультиарендность.
--
-- До этой миграции организация была свойством пользователя: одна почта —
-- одна организация навсегда. Это тупик — подрядчик в двух командах, продажник
-- в демо-стенде и в своей компании, любой перенос сотрудника между
-- юрлицами. Разделяем два разных понятия:
--
--   users       — личность: почта и пароль, глобально уникальные;
--   memberships — участие: кто в какой организации и с какой ролью.
--
-- Роль переезжает из users в memberships: она свойство участия, а не
-- человека. Один и тот же человек может быть владельцем своей команды
-- и наблюдателем в чужой.
--
-- Вторая половина миграции — изоляция на уровне PostgreSQL (RLS).
-- Проверок в коде мало: достаточно одного забытого условия в одном запросе,
-- чтобы данные утекли между арендаторами. RLS делает такую ошибку
-- невозможной, а не маловероятной.

-- --- 1. Организация становится адресуемой ---

alter table orgs add column slug text;

update orgs set slug = left(replace(id::text, '-', ''), 12) where slug is null;

alter table orgs alter column slug set not null;
create unique index orgs_slug_key on orgs (lower(slug));

-- --- 2. Личность отделяется от участия ---

create table memberships (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid        not null references orgs (id) on delete cascade,
    user_id    uuid        not null references users (id) on delete cascade,
    role       text        not null default 'member',
    created_at timestamptz not null default now(),
    constraint memberships_role_valid check (role in ('owner', 'member', 'viewer')),
    constraint memberships_unique unique (org_id, user_id)
);
create index memberships_user_idx on memberships (user_id);

insert into memberships (org_id, user_id, role)
select org_id, id, role from users;

-- Почта становится глобально уникальной: она теперь идентификатор личности,
-- а не пары «личность в организации». Если до этой миграции один адрес был
-- заведён в двух организациях, создание индекса упадёт — это осознанно:
-- такие дубли надо разобрать руками, а не склеивать вслепую.
drop index users_org_email_key;
create unique index users_email_key on users (lower(email));

alter table users drop column org_id;
alter table users drop column role;

-- --- 3. Сессия помнит, в какой организации человек сейчас работает ---

alter table sessions add column active_org_id uuid references orgs (id) on delete cascade;

update sessions s
   set active_org_id = m.org_id
  from memberships m
 where m.user_id = s.user_id;

-- --- 4. Приглашения ---

create table invites (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    email       text        not null,
    role        text        not null default 'member',
    -- в базе лежит только хеш: утечка таблицы не даёт войти по ссылкам
    token_hash  text        not null,
    invited_by  uuid        references users (id) on delete set null,
    expires_at  timestamptz not null,
    accepted_at timestamptz,
    accepted_by uuid        references users (id) on delete set null,
    revoked_at  timestamptz,
    created_at  timestamptz not null default now(),
    constraint invites_role_valid check (role in ('owner', 'member', 'viewer'))
);
create unique index invites_token_key on invites (token_hash);
create index invites_org_idx on invites (org_id) where accepted_at is null and revoked_at is null;

-- --- 5. Изоляция арендаторов средствами базы ---

-- Возвращает организацию, в области которой выполняется транзакция.
-- Не задана — NULL, и все политики ниже перестают пропускать строки:
-- забыть выставить арендатора безопаснее, чем увидеть чужие данные.
create function app_current_org() returns uuid
language sql stable
as $$ select nullif(current_setting('app.current_org', true), '')::uuid $$;

do $$
declare
    t text;
begin
    foreach t in array array[
        'projects', 'boards', 'board_columns', 'cards', 'card_events', 'operations', 'invites'
    ] loop
        execute format('alter table %I enable row level security', t);
        -- FORCE обязателен: без него политики не действуют на владельца
        -- таблиц, а приложение подключается именно под ним
        execute format('alter table %I force row level security', t);
        execute format(
            'create policy tenant_isolation on %I
               using (org_id = app_current_org())
               with check (org_id = app_current_org())', t);
    end loop;
end $$;

-- Приглашение — исключение: его открывают по секретной ссылке, когда
-- организация ещё неизвестна, а человек может вообще не иметь аккаунта.
-- Вторая политика разрешает доступ к строке тому, кто предъявил токен.
-- Это ровно та модель, которой приглашение и является: знание секрета
-- и есть право, а не принадлежность к организации.
create policy invite_by_token on invites
    using (token_hash = nullif(current_setting('app.invite_token', true), ''))
    with check (token_hash = nullif(current_setting('app.invite_token', true), ''));

-- Таблицы личности (orgs, users, sessions, memberships) под RLS не попадают
-- намеренно: к ним обращаются до того, как известна организация — на входе,
-- при проверке сессии и при выборе активной организации. Их запросы всегда
-- ограничены конкретным user_id, и эта граница проверяется в коде.
