-- Решение по идее словами (ROADMAP 43, срез 2): «берём», «не берём»,
-- «уже есть», «позже» — с автором, датой и пояснением. Записи только
-- добавляются, как у статуса эпика (0082): последняя — нынешнее
-- решение, остальные — история; ошибку исправляют новой записью.
--
-- Кто решает (решение владельца 30.10.2026): владелец организации —
-- любую идею; владелец подразделения — идеи, адресованные его
-- поддереву. Идея без адресата — только владельцу организации.
-- Политика смотрит на это сама: вставка от того, кому нельзя, падает
-- на 42501, и отказ переводится в слова.

create table idea_decisions (
    id           uuid primary key default gen_random_uuid(),
    org_id       uuid not null references orgs (id) on delete cascade,
    idea_id      uuid not null references ideas (id) on delete cascade,
    author_id    uuid references users (id) on delete set null,
    decision     text not null
        constraint idea_decisions_known check (decision in ('taken', 'declined', 'duplicate', 'later')),
    -- Пояснение: у «не берём» обязательно — автору нужен ответ, а не
    -- молчаливый отказ; у остальных по желанию.
    note         text not null default ''
        constraint idea_decisions_note_length check (length(note) <= 2000)
        constraint idea_decisions_declined_has_reason check (decision <> 'declined' or btrim(note) <> ''),
    -- «Берём» — карточка, которая появилась из идеи. Карточку удалили
    -- насовсем — решение остаётся, ссылка пустеет.
    card_id      uuid references cards (id) on delete set null,
    -- «Уже есть» — та идея, в которую влились голоса этой.
    into_idea_id uuid references ideas (id) on delete set null,
    constraint idea_decisions_not_self check (into_idea_id is null or into_idea_id <> idea_id),
    created_at   timestamptz not null default now()
);

create index idea_decisions_idea_idx on idea_decisions (idea_id, created_at desc);

alter table idea_decisions enable row level security;
alter table idea_decisions force  row level security;

create policy tenant_isolation on idea_decisions as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Решение видно всем, кому видна идея: ради ответа автору и голосовавшим
-- оно и пишется.
create policy visible on idea_decisions for select using (true);

create policy decide on idea_decisions for insert
    with check (
        author_id = (select app_current_user())
        and exists (
            select 1 from ideas i
             where i.id = idea_id
               and ((select app_is_owner())
                    or (i.team_id is not null
                        and i.team_id = any (array(select unnest(app_admin_teams())))))));
