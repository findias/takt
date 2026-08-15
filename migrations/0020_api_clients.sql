-- Сервисные клиенты: доступ к API не от человека.
--
-- Токен принадлежит организации, а не человеку: интеграция живёт дольше
-- того, кто её завёл, и увольнение сотрудника не должно ронять обмен
-- с соседней системой.
--
-- Главное решение здесь неочевидно и стоит объяснения. У клиента есть
-- собственная личность — строка в users, — и через неё он состоит
-- в организации ровно как человек.
--
-- Соблазн был обратный: научить политики понимать «сейчас действует
-- клиент, а не человек». Это значило бы переписать все резолверы и
-- каждую политику, добавив в них вторую ветку, и с этого момента любое
-- правило доступа надо было бы держать в голове дважды. Личность вместо
-- второй ветки решает и это, и заодно то, о чём мы предупреждали ещё
-- в журнале действий: actor_id ссылается на users, а действие токена
-- человеку не принадлежит. Теперь принадлежит — служебной личности,
-- у которой есть имя.
--
-- Такая личность не может войти по паролю: хеш заведомо непригоден,
-- а сессий у неё не бывает. Единственный способ действовать — предъявить
-- токен.
--
-- В базе лежит только хеш токена, как у приглашений: показать значение
-- повторно невозможно. Отдельно хранится начало токена — по нему человек
-- узнаёт свой ключ в списке, не раскрывая его.

create table api_clients (
    id      uuid primary key default gen_random_uuid(),
    org_id  uuid not null references orgs (id) on delete cascade,
    name    text not null,
    -- Служебная личность клиента. Через неё работают все политики:
    -- вторая ветка в правилах доступа не нужна.
    user_id uuid not null references users (id) on delete cascade,

    token_hash text not null unique,
    -- Первые символы токена: по ним ключ узнают в списке, не раскрывая.
    prefix text not null,
    -- Что клиенту позволено. Проверяется на входе в API, до всяких
    -- политик: политики отвечают на вопрос «чьи данные», а разрешения —
    -- «что этому ключу вообще можно».
    scopes text[] not null,

    created_by   uuid references users (id),
    created_at   timestamptz not null default now(),
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz,

    constraint api_clients_scopes_not_empty check (cardinality(scopes) > 0)
);

create index api_clients_org_idx on api_clients (org_id, created_at desc);
create unique index api_clients_name_idx on api_clients (org_id, lower(name))
    where revoked_at is null;

alter table api_clients enable row level security;
alter table api_clients force  row level security;

-- Изоляция арендатора с одной оговоркой: клиент ищется по токену до того,
-- как известна организация, — предъявленный секрет и есть право. Поэтому
-- при невыставленном арендаторе ограничение пропускает строку дальше,
-- а решают permissive-политики ниже: либо владелец организации, либо
-- совпадение хеша токена. Ровно так же устроено приглашение.
create policy tenant_isolation on api_clients as restrictive
    using (org_id = coalesce((select app_current_org()), org_id))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Ключи организации видит и заводит только владелец: список интеграций —
-- это список того, что имеет доступ к данным, и он же подсказка,
-- что стоит украсть.
create policy manage on api_clients for all
    using ((select app_is_owner())) with check ((select app_is_owner()));

-- Поиск клиента по предъявленному токену идёт до того, как известна
-- организация, — ровно как у приглашения. Политика открывает одну строку
-- тому, кто предъявил её хеш.
create function app_api_token() returns text
language sql stable parallel safe
as $$ select nullif(current_setting('app.api_token', true), '') $$;

create policy by_token on api_clients for select
    using (token_hash = (select app_api_token()));

-- Отметка о последнем использовании — единственное, что клиент меняет
-- в себе сам, и только предъявив свой токен.
create policy touch_self on api_clients for update
    using (token_hash = (select app_api_token()))
    with check (token_hash = (select app_api_token()));
