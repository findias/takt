-- Сохранённые виды доски.
--
-- Вид — это «фильтры плюс группировка», которые человек настроил один
-- раз и хочет открывать одним нажатием: «моё на этой неделе», «всё
-- заблокированное», «чужое без исполнителя».
--
-- Хранится на сервере, а не в браузере. Настройка, живущая
-- в localStorage, исчезает при смене машины и при чистке — а вид,
-- в отличие от свёрнутой колонки, человек настраивает осмысленно
-- и ждёт увидеть завтра. Свёрнутая колонка осталась в браузере именно
-- по обратной причине: она про то, как сейчас уложен экран.
--
-- Принадлежит человеку и доске одновременно. Не организации: «моё
-- на неделе» у каждого своё. И не только человеку: фильтр по колонкам
-- одной доски на другой доске бессмыслен.
--
-- Условия хранятся строкой запроса, а не разобранными колонками.
-- Разложить их по полям значило бы менять схему при каждом новом
-- фильтре, а список фильтров будет расти. Строка же ровно та, что
-- стоит в адресе: вид — это сохранённая ссылка, и никакого второго
-- представления для него заводить не нужно.

create table board_views (
    id       uuid primary key default gen_random_uuid(),
    org_id   uuid not null references orgs (id) on delete cascade,
    board_id uuid not null references boards (id) on delete cascade,
    user_id  uuid not null references users (id) on delete cascade,
    name     text not null,
    query    text not null default '',
    created_at timestamptz not null default now(),

    constraint board_views_name_not_empty check (length(trim(name)) > 0)
);

-- Два вида с одним названием у одного человека на одной доске — это
-- опечатка, а не замысел.
create unique index board_views_name_idx
    on board_views (board_id, user_id, lower(name));

create index board_views_mine_idx on board_views (org_id, user_id, board_id);

alter table board_views enable row level security;
alter table board_views force  row level security;

create policy tenant_isolation on board_views as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Свои виды видит и правит только их владелец. Чужие не показываются
-- вовсе: это не секрет, но и не общее знание — показывать список чужих
-- сохранённых фильтров значит рассказывать, кто чем занят.
create policy own on board_views for all
    using (user_id = (select app_current_user()))
    with check (user_id = (select app_current_user()));
