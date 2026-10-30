-- Исходящий ящик событий (ROADMAP 43, срез 4 — найдено на событиях идей).
--
-- Доставка клалась в очередь в транзакции действия запросом
-- «insert … select from webhooks»: подписки выбирались под политиками
-- того, кто действует, а подписки видит только владелец организации
-- (0022, manage). Действие участника, ключа или наблюдателя не находило
-- ни одной подписки — и событие тихо не отправлялось. Проверки этого
-- не ловили: все заводили подписку и действовали одним владельцем.
--
-- Теперь действие кладёт событие в ящик, не глядя на подписки, а
-- работник — он ходит без арендатора и видит подписки все — раскладывает
-- ящик по доставкам. Секрет подписки по-прежнему читают только владелец
-- и работник: открывать webhooks всем пишущим ради одного select было бы
-- дырой шире починки.

create table webhook_outbox (
    id         bigserial primary key,
    org_id     uuid not null references orgs (id) on delete cascade,
    event      text not null,
    payload    jsonb not null,
    created_at timestamptz not null default now()
);

alter table webhook_outbox enable row level security;
alter table webhook_outbox force  row level security;

create policy tenant_isolation on webhook_outbox as restrictive
    using (org_id = coalesce((select app_current_org()), org_id))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Кладёт всякий, кто действует в организации: человек любой роли, ключ,
-- служебная задача с выставленной организацией (снятие блокировок).
create policy emit on webhook_outbox for insert
    with check ((select app_current_org()) is not null);

-- Разбирает работник — без действующего лица, как и очередь доставок.
create policy worker_takes on webhook_outbox for select
    using ((select app_no_actor()));
create policy worker_clears on webhook_outbox for delete
    using ((select app_no_actor()));
create policy worker_enqueues on webhook_deliveries for insert
    with check ((select app_no_actor()));

-- Пауза обещает: случившееся во время неё не копится и после
-- возобновления не прилетает лавиной. Раньше это держалось тем, что
-- доставка заводилась в момент события; теперь момент раскладки —
-- другой, и работнику нужно знать, когда подписку возобновили: событие
-- старше возобновления к ней не относится. Подписке, которую не
-- возобновляли, годится всё с её заведения.
alter table webhooks add column resumed_at timestamptz;
