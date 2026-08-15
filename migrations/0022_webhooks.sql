-- Вебхуки: рассказать наружу о том, что произошло.
--
-- Подписка — это обещание доставить, и обещание надо чем-то обеспечить.
-- Отправлять запрос прямо из операции нельзя: чужой сервер отвечает
-- медленно или не отвечает вовсе, и перемещение карточки начало бы
-- зависеть от того, жива ли соседняя система.
--
-- Поэтому исходящий ящик: доставка кладётся в ту же транзакцию, что и
-- само событие. Либо произошло и то, и другое, либо ничего — событие
-- не может оказаться записанным, а доставка потерянной. Отправляет её
-- потом отдельный работник, и никакого брокера для этого не нужно:
-- очередь на таблице с `for update skip locked` — ровно то, что здесь
-- требуется, а лишняя движущаяся часть в эксплуатации дороже.
--
-- Доставка гарантируется «не менее одного раза». Ровно одного раза
-- по сети не бывает: ответ теряется и после успешной обработки, и честный
-- ответ на это — повторить. Поэтому у каждой доставки есть постоянный
-- идентификатор, и принимающая сторона обязана уметь его различать.
--
-- Подпись — HMAC от тела ключом подписки, вместе с меткой времени: без
-- метки перехваченный запрос можно повторить когда угодно, а с ней окно
-- ограничено. Ключ показывается один раз, как и все секреты здесь.

create table webhooks (
    id       uuid primary key default gen_random_uuid(),
    org_id   uuid not null references orgs (id) on delete cascade,
    name     text not null,
    url      text not null,
    -- Секрет подписи хранится как есть, а не хешем: им надо подписывать,
    -- а не сверять предъявленное. Это первое место в схеме, где лежит
    -- обратимый секрет, и другого выхода нет.
    secret   text not null,
    -- На что подписаны. Пустой массив означал бы подписку на всё —
    -- такого не бывает: подписка без выбора это способ узнать больше,
    -- чем собирался.
    events   text[] not null,

    created_by  uuid references users (id),
    created_at  timestamptz not null default now(),
    -- Отключается сама после долгой череды отказов: молча копить
    -- недоставленное годами хуже, чем перестать и сказать об этом.
    disabled_at timestamptz,
    last_error  text,

    constraint webhooks_events_not_empty check (cardinality(events) > 0),
    constraint webhooks_url_is_http check (url ~ '^https?://')
);

create index webhooks_org_idx on webhooks (org_id) where disabled_at is null;

-- Исходящий ящик.
create table webhook_deliveries (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid not null references orgs (id) on delete cascade,
    webhook_id uuid not null references webhooks (id) on delete cascade,
    event      text not null,
    payload    jsonb not null,

    attempts        int not null default 0,
    -- Когда пробовать в следующий раз. Прошедшее время означает «пора».
    next_attempt_at timestamptz not null default now(),
    delivered_at    timestamptz,
    -- Отказались от дальнейших попыток. Строка остаётся: по ней видно,
    -- что именно не доехало, и её можно отправить руками.
    failed_at   timestamptz,
    last_status int,
    last_error  text,
    created_at  timestamptz not null default now()
);

-- Работник берёт по этому индексу: неотправленные, у которых срок настал.
create index webhook_deliveries_due_idx
    on webhook_deliveries (next_attempt_at)
    where delivered_at is null and failed_at is null;
create index webhook_deliveries_hook_idx
    on webhook_deliveries (org_id, webhook_id, created_at desc);

alter table webhooks           enable row level security;
alter table webhooks           force  row level security;
alter table webhook_deliveries enable row level security;
alter table webhook_deliveries force  row level security;

create policy tenant_isolation on webhooks as restrictive
    using (org_id = coalesce((select app_current_org()), org_id))
    with check (org_id = coalesce((select app_current_org()), org_id));
create policy tenant_isolation on webhook_deliveries as restrictive
    using (org_id = coalesce((select app_current_org()), org_id))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Подписки заводит и видит владелец: подписка выносит данные наружу,
-- и список подписок — это список того, куда они утекают.
create policy manage on webhooks for all
    using ((select app_is_owner())) with check ((select app_is_owner()));
create policy visible on webhook_deliveries for select
    using ((select app_is_owner()));

-- Доставка кладётся в ящик тем, кто вызвал операцию, — то есть любым,
-- кто может писать. Разбирать ящик будет работник.
create policy appendable on webhook_deliveries for insert
    with check ((select app_can_write()));

-- Ручной повтор — единственное, что владелец меняет в доставке: вернуть
-- сдавшуюся в очередь, когда получателя починили. Всё остальное в ней
-- пишет работник.
create policy retry on webhook_deliveries for update
    using ((select app_is_owner())) with check ((select app_is_owner()));

-- Работник ходит без арендатора: очередь общая, и выбирать из неё
-- по одной организации значило бы обходить их по кругу и знать заранее,
-- какие вообще есть. Ограничение выше это допускает — при невыставленном
-- арендаторе оно пропускает строку, — а других политик на изменение нет,
-- поэтому доступ к чужой очереди из запроса пользователя всё равно
-- закрыт: у него арендатор выставлен всегда.
create policy worker on webhook_deliveries for update
    using ((select app_current_org()) is null)
    with check ((select app_current_org()) is null);
create policy worker_reads on webhook_deliveries for select
    using ((select app_current_org()) is null);
create policy worker_reads_hooks on webhooks for select
    using ((select app_current_org()) is null);
