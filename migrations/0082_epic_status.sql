-- Статус эпика словами (ROADMAP 42.3, срез 10): «идёт», «под угрозой»,
-- «сорван» — с автором, датой и пояснением. Полоса и прогноз говорят,
-- сколько сделано и когда будет готово остальное; статус — то, что
-- об этом думает человек, ведущий эпик, и чего из чисел не вывести:
-- смежники не отвечают, заказчик передумал, срок сдвинут договором.
--
-- Строки только добавляются: статус — это запись о том, что было
-- сказано тогда-то, и история таких записей руководству нужна не
-- меньше последней. Ошибку исправляют новой записью, не правкой.
--
-- Кто пишет: тот, кому доска-портфель открыта на запись, — политика
-- writable, как у вложений. Отдельного права «статус чужого эпика» нет:
-- кто ведёт портфель, тот и говорит о его эпиках; у наблюдателя доска
-- только для чтения, и статус он тоже не пишет.

create table epic_statuses (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid not null references orgs (id) on delete cascade,
    board_id   uuid not null references boards (id) on delete cascade,
    card_id    uuid not null references cards (id) on delete cascade,
    author_id  uuid references users (id),
    status     text not null
        constraint epic_statuses_status_known check (status in ('on_track', 'at_risk', 'off_track')),
    -- Пояснение не обязательно: «идёт» без слов — нормальный статус.
    -- Предел — чтобы статус оставался статусом, а не отчётом.
    note       text not null default ''
        constraint epic_statuses_note_length check (length(note) <= 2000),
    created_at timestamptz not null default now()
);

create index epic_statuses_card_idx on epic_statuses (card_id, created_at desc);

alter table epic_statuses enable row level security;
alter table epic_statuses force  row level security;

create policy tenant_isolation on epic_statuses as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Статус наследует видимость доски, как всё, что на ней висит.
create policy visible on epic_statuses for select
    using (board_id = any (array(select unnest(app_visible_boards()))));
create policy writable on epic_statuses for all
    using (board_id = any (array(select unnest(app_writable_boards()))))
    with check (board_id = any (array(select unnest(app_writable_boards()))));
