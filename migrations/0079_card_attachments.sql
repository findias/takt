-- Вложения к карточке (ROADMAP 36.1).
--
-- Файл лежит в базе, а не на диске. Решение про хранение задним
-- числом не меняется: тысяча файлов, разложенных по каталогу, в базу
-- потом не переедут одной миграцией, а бэкап у установки один — базы.
-- Диска у пода может не быть вовсе (квота томов ноль — реальный стенд
-- заказчика), а размер каждого файла ограничен настройкой, так что
-- таблица растёт медленнее обсуждений. Вынос в объектное хранилище,
-- если понадобится, — отдельная миграция: строка остаётся, тело уезжает.
--
-- Удаление настоящее, не мягкое: на вложение не ссылается ничто,
-- кроме события в ленте, а оно хранит имя файла само.

create table card_attachments (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid not null references orgs (id) on delete cascade,
    board_id   uuid not null references boards (id) on delete cascade,
    card_id    uuid not null references cards (id) on delete cascade,
    author_id  uuid references users (id),
    -- Имя — для показа и для имени скачанного файла. Что внутри,
    -- решает media_type, выведенный сервером из расширения, а не
    -- присланный браузером.
    name       text not null constraint card_attachments_name_not_empty check (btrim(name) <> ''),
    media_type text not null,
    size       integer not null constraint card_attachments_size_positive check (size > 0),
    body       bytea not null,
    created_at timestamptz not null default now()
);

create index card_attachments_card_idx on card_attachments (card_id, created_at);

-- Тело читают по одному и только при скачивании; список карточки его
-- не трогает. Хранить отдельно (toast и так уносит крупное), поэтому
-- без второй таблицы.

alter table card_attachments enable row level security;
alter table card_attachments force  row level security;

create policy tenant_isolation on card_attachments as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Вложение наследует видимость доски, как всё, что на ней висит.
create policy visible on card_attachments for select
    using (board_id = any (array(select unnest(app_visible_boards()))));
create policy writable on card_attachments for all
    using (board_id = any (array(select unnest(app_writable_boards()))))
    with check (board_id = any (array(select unnest(app_writable_boards()))));
