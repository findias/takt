-- Уведомления внутри приложения (ROADMAP, этап 29).
--
-- До этой миграции продукт никому ничего не сообщал: человека звали
-- в обсуждение, назначали на карточку, блокировали его работу — и узнавал
-- он об этом, только открыв доску. Первый срез — колокольчик в самом
-- приложении; почта — потом, поверх этой же таблицы.
--
-- Уведомление пишется в той же транзакции, что и действие, которое его
-- вызвало, — как доставка подписки на события: не теряется при
-- записанном действии и не появляется без него. Источник — само
-- действие: комментарий, событие журнала, назначение. Уникальность
-- «получатель + повод + источник» не даёт двойника, даже если действие
-- записано дважды: у каждого действия источник свой, а повтор операции
-- сервер не выполняет заново. Ключ здесь — страховка, которая сделает
-- ошибку видной.
--
-- Старая версия приложения таблицы не знает и в неё не пишет; в окне
-- выкладки уведомления просто не появляются.

create table notifications (
    id           uuid primary key default gen_random_uuid(),
    org_id       uuid not null references orgs (id) on delete cascade,
    -- Кому. Уведомление — личное, и видит его только получатель.
    recipient_id uuid not null references users (id) on delete cascade,
    board_id     uuid not null references boards (id) on delete cascade,
    card_id      uuid not null references cards (id) on delete cascade,
    -- Кто это сделал; пусто — служебная задача (снятие по сроку).
    actor_id     uuid references users (id) on delete set null,
    reason       text not null
        constraint notifications_reason_known
        check (reason in ('mentioned', 'assigned', 'blocked', 'block_expired')),
    -- Что именно вызвало: комментарий, событие журнала, назначение.
    source       text not null,
    created_at   timestamptz not null default now(),
    read_at      timestamptz,
    constraint notifications_once unique (recipient_id, reason, source)
);

-- Список и счётчик — всегда своих и свежих сверху; непрочитанное
-- считается при каждой загрузке экрана.
create index notifications_recipient on notifications (recipient_id, created_at desc);
create index notifications_unread on notifications (recipient_id) where read_at is null;

alter table notifications enable row level security;
alter table notifications force row level security;

create policy tenant_isolation on notifications as restrictive
    using (org_id = coalesce((select app_current_org()), org_id))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Политика по получателю, а не по доске, — новый для проекта вид.
-- И по доске тоже: видимость перепроверяется при каждом чтении. Отняли
-- доступ к закрытой доске — прежнее уведомление перестаёт быть видным,
-- и название карточки через него не утекает.
create policy own on notifications for select
    using (
        recipient_id = (select app_current_user())
        and board_id = any (array(select unnest(app_visible_boards())))
    );

-- Прочитать своё. Меняется только отметка о прочтении — это держит
-- код; политика держит главное: чужое не тронуть.
create policy mark_own on notifications for update
    using (
        recipient_id = (select app_current_user())
        and board_id = any (array(select unnest(app_visible_boards())))
    )
    with check (recipient_id = (select app_current_user()));

-- Пишет тот, кто действует, и только о себе и о доске, которую он
-- вправе менять: подписаться чужим именем или уведомить о доске,
-- которой не видишь, нельзя.
create policy notifies on notifications for insert
    with check (
        actor_id = (select app_current_user())
        and board_id = any (array(select unnest(app_writable_boards())))
    );

-- Снятие блокировки по сроку идёт без человека — как в 0053: служебная
-- задача пишет ровно свой повод и без автора.
create policy expiry_notifies on notifications for insert
    with check (
        (select app_task()) = 'expire_blocks'
        and reason = 'block_expired'
        and actor_id is null
    );

-- Журнал теперь отдаёт номер записанного события (`returning id`):
-- по нему уведомление ссылается на то, что его вызвало. Под политиками
-- `returning` требует права видеть вставленную строку, а у задачи
-- снятия по сроку было только право вставить. Открывается ровно то,
-- что она сама и пишет.
create policy expiry_reads_log on card_events for select
    using ((select app_task()) = 'expire_blocks' and type = 'block_expired');

-- Уборщик убирает прочитанное старше девяноста дней. Тот же срок стоит
-- в internal/retention; разойтись им нельзя, иначе уборка молча
-- перестанет что-либо находить.
create policy cleanup on notifications for delete
    using ((select app_no_actor()));
create policy cleanup_reads on notifications for select
    using (
        (select app_no_actor())
        and read_at < now() - interval '90 days'
    );
