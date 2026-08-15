-- Иерархия карточек, исход работы и блокировки.
--
-- Всё в этой миграции — данные, которых потом неоткуда взять. Мы уже заложили
-- журнал переходов и точки старта и финиша; здесь закрываются четыре дыры,
-- каждая из которых молча портит метрики потока:
--
--   1. Отмена неотличима от завершения. Пропускная способность считает
--      выброшенные карточки за сделанную работу, и любой прогноз по ней
--      систематически оптимистичен. Little (2011) формулирует это прямо:
--      λ считается по всем покинувшим систему, поэтому «покинул» и
--      «сделан» обязаны быть разными фактами.
--   2. Связи карточек. Подзадача, заведённая как обычная карточка, теряет
--      факт «это разбиение вон той задачи» — а вместе с ним и долю
--      разбиения, без которой прогноз по бэклогу занижает объём.
--   3. Блокировки. Kanban Guide требует «unblocking blocked work» как
--      практику, а булев флаг не даёт ни времени разрешения блокировки,
--      ни эффективности потока. Интервал задним числом не восстановить.
--   4. Смысл перехода. `is_started_point` и `kind` изменяемы: переразметили
--      доску — и весь исторический цикл пересчитался по новым правилам.
--      Журнал есть, а смысл журнала переписывается. Поэтому смысл кладётся
--      снимком в событие.
--
-- Связи вынесены в отдельную таблицу, а не в `cards.parent_id`, по трём
-- причинам: типов связи больше одного, появление связи — это событие, а
-- одна и та же карточка может быть подзадачей в одной цепочке и блокировать
-- другую.

-- --- 1. Исход работы ---

alter table cards
    -- null — работа ещё идёт. Проставляется при пересечении точки финиша
    -- (done) или при отказе от карточки (discarded).
    add column outcome text
        constraint cards_outcome_valid
        check (outcome is null or outcome in ('done', 'discarded'));

-- Уже завершённые карточки считаем сделанными: другого источника истины нет.
update cards set outcome = 'done' where finished_at is not null;

-- Пропускная способность и время цикла читаются по этому индексу.
create index cards_outcome_idx on cards (board_id, outcome, finished_at)
    where outcome is not null;

-- --- 2. Связи карточек ---

create table card_links (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid        not null references orgs (id) on delete cascade,
    -- Направление имеет смысл: для subtask — родитель и ребёнок,
    -- для blocks — кто мешает и кому.
    from_card   uuid        not null references cards (id) on delete cascade,
    to_card     uuid        not null references cards (id) on delete cascade,
    kind        text        not null default 'subtask',
    created_by  uuid,
    created_at  timestamptz not null default now(),
    -- Только для blocks: момент, когда зависимость перестала мешать.
    resolved_at timestamptz,
    constraint card_links_kind_valid check (kind in ('subtask', 'blocks', 'relates')),
    -- Карточка не может ссылаться на себя ни в каком смысле.
    constraint card_links_not_self check (from_card <> to_card)
);

-- Пара карточек связывается каждым видом связи не больше одного раза.
create unique index card_links_unique on card_links (from_card, to_card, kind);
create index card_links_to_idx on card_links (to_card, kind);
-- У подзадачи ровно один родитель: дерево, а не граф. Для blocks и relates
-- такого ограничения нет — зависимостей у задачи может быть сколько угодно.
create unique index card_links_single_parent on card_links (to_card)
    where kind = 'subtask';

-- --- 3. Блокировки как интервалы ---

create table card_blocks (
    id           uuid primary key default gen_random_uuid(),
    org_id       uuid        not null references orgs (id) on delete cascade,
    card_id      uuid        not null references cards (id) on delete cascade,
    reason       text        not null default '',
    blocked_at   timestamptz not null default now(),
    blocked_by   uuid,
    unblocked_at timestamptz,
    unblocked_by uuid
);

-- Одна открытая блокировка на карточку: вторая причина — это дополнение
-- к первой, а не новый интервал, иначе время в блоке считается дважды.
create unique index card_blocks_open on card_blocks (card_id)
    where unblocked_at is null;
create index card_blocks_card_idx on card_blocks (card_id, blocked_at);

-- --- 4. Изоляция арендаторов ---

alter table card_links  enable row level security;
alter table card_links  force  row level security;
alter table card_blocks enable row level security;
alter table card_blocks force  row level security;

create policy tenant_isolation on card_links
    using (org_id = app_current_org())
    with check (org_id = app_current_org());

create policy tenant_isolation on card_blocks
    using (org_id = app_current_org())
    with check (org_id = app_current_org());

-- --- 5. Индексы, ведущие с org_id ---
--
-- Политика RLS добавляется к каждому запросу, поэтому арендатор должен быть
-- первым столбцом составного индекса: иначе планировщик выбирает между
-- индексом под запрос и индексом под политику и нередко берёт seq scan.
create index cards_org_board_idx on cards (org_id, board_id)
    where archived_at is null;
create index card_events_org_board_idx on card_events (org_id, board_id, at);
