-- История задачи в системе, откуда её перенесли (ROADMAP 23.7).
--
-- YouGile хранит историю задачи системными сообщениями её чата:
-- «переместил в колонку…», «назначил…». Переносить их в журнал событий
-- доски нельзя: журнал листается по номеру записи, и события трёхлетней
-- давности, дописанные сегодня, встали бы в «Изменениях» первыми —
-- как будто всё это случилось только что. Поэтому своя таблица: карточка
-- показывает её отдельным блоком «до переноса», по времени событий.
--
-- Текстом, как пришло: разбирать чужие формулировки в наши переходы
-- без образца значит выдумывать метрику. Переходы из неё — потом.
create table card_source_history (
    id       bigserial   primary key,
    org_id   uuid        not null references orgs (id) on delete cascade,
    board_id uuid        not null references boards (id) on delete cascade,
    card_id  uuid        not null references cards (id) on delete cascade,
    at       timestamptz not null,
    -- Кто это сделал там — именем: человек мог не переехать.
    author   text        not null default '',
    text     text        not null,
    constraint card_source_history_text_not_empty check (length(trim(text)) > 0)
);
create index card_source_history_card_idx on card_source_history (card_id, at);

alter table card_source_history enable row level security;
alter table card_source_history force  row level security;

create policy tenant_isolation on card_source_history as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));
-- Видимость — доски, как у обсуждения.
create policy visible on card_source_history for select
    using (board_id = any (array(select unnest(app_visible_boards()))));
create policy writable on card_source_history for all
    using (board_id = any (array(select unnest(app_writable_boards()))))
    with check (board_id = any (array(select unnest(app_writable_boards()))));

-- Когда история источника дотянута. Пусто — ещё нет (перенос по API
-- дотягивает её фоном), и повторный перенос знает, за какими карточками
-- ещё идти. Старая версия приложения колонку не знает и не трогает.
alter table cards add column source_history_at timestamptz;
