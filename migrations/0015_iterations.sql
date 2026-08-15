-- Итерации: спринт как интервал, а не как поле на карточке.
--
-- Соблазн — завести `cards.iteration_id` и закрыть вопрос. Так делают,
-- и так теряют ровно то, ради чего итерации заводят. Поле отвечает
-- на вопрос «где карточка сейчас», а спрашивают всегда другое: что было
-- в спринте на момент его закрытия, сколько добавили после начала,
-- сколько выкинули по дороге. Поле этого не знает и знать не может:
-- при переносе карточки в следующий спринт прошлое просто затирается.
--
-- Поэтому вхождение в итерацию — интервал, как и блокировка: когда
-- вошла, когда вышла. Состав на любой момент восстанавливается запросом,
-- и «добавлено после старта» перестаёт быть догадкой.
--
-- Это же делает миграцию необратимой задним числом: интервалы, которых
-- не записывали, не восстановить. Отсюда её место в этапе «необратимое».
--
-- Итерация принадлежит доске, а не команде. У карточки доска есть всегда,
-- а команда — не всегда: доска может быть открыта всей организации
-- и не принадлежать никому. Привязка к команде запретила бы итерации
-- на таких досках, а привязка к доске не запрещает ничего.

create table iterations (
    id        uuid primary key default gen_random_uuid(),
    org_id    uuid not null references orgs (id) on delete cascade,
    board_id  uuid not null references boards (id) on delete cascade,
    name      text not null,
    starts_on date not null,
    ends_on   date not null,
    -- Цель итерации словами. Не украшение: на ретроспективе спрашивают
    -- «чего хотели», и ответ должен быть записан до, а не вспомнен после.
    goal       text        not null default '',
    -- Закрытая итерация не принимает и не отпускает карточки: её состав
    -- застыл, и именно по нему считается сделанное.
    closed_at  timestamptz,
    created_at timestamptz not null default now(),

    constraint iterations_dates_ordered check (ends_on >= starts_on)
);

create index iterations_board_idx on iterations (org_id, board_id, starts_on desc);

-- Вхождение карточки в итерацию. Интервал, а не ссылка.
create table iteration_cards (
    id           bigserial primary key,
    org_id       uuid not null references orgs (id) on delete cascade,
    iteration_id uuid not null references iterations (id) on delete cascade,
    card_id      uuid not null references cards (id) on delete cascade,
    added_at     timestamptz not null default now(),
    added_by     uuid references users (id),
    removed_at   timestamptz,
    removed_by   uuid references users (id),

    constraint iteration_cards_interval_ordered
        check (removed_at is null or removed_at >= added_at)
);

-- Карточка входит в итерацию один раз за интервал…
create unique index iteration_cards_open_idx
    on iteration_cards (iteration_id, card_id) where removed_at is null;
-- …и не может одновременно идти в двух итерациях: иначе сделанная работа
-- посчиталась бы дважды, в обеих.
create unique index iteration_cards_one_open_per_card_idx
    on iteration_cards (card_id) where removed_at is null;

create index iteration_cards_iteration_idx on iteration_cards (iteration_id, added_at);
create index iteration_cards_card_idx on iteration_cards (card_id, added_at);

-- --- Доступ ---
--
-- И итерация, и вхождение принадлежат доске, поэтому наследуют её
-- видимость — так же, как колонки и карточки.

alter table iterations      enable row level security;
alter table iterations      force  row level security;
alter table iteration_cards enable row level security;
alter table iteration_cards force  row level security;

create policy tenant_isolation on iterations as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));
create policy tenant_isolation on iteration_cards as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

create policy visible on iterations for select
    using (board_id = any (array(select unnest(app_visible_boards()))));
create policy writable on iterations for all
    using (board_id = any (array(select unnest(app_writable_boards()))))
    with check (board_id = any (array(select unnest(app_writable_boards()))));

-- Вхождение адресуется итерацией: доска у него та же, что у неё.
create policy visible on iteration_cards for select
    using (iteration_id in (select id from iterations));
create policy writable on iteration_cards for all
    using (iteration_id in (select id from iterations) and (select app_can_write()))
    with check (iteration_id in (select id from iterations) and (select app_can_write()));
