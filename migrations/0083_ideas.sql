-- Идеи от людей команды (ROADMAP 43, срез 1): отдельный список
-- организации, а не доска. Идея — не работа: у неё нет исполнителя,
-- оценки и колонки, она не портит метрики потока и «создано против
-- сделано». Поэтому таблица своя, а не вид карточки.
--
-- Подаёт каждый в организации, в том числе наблюдатель (решение
-- владельца 30.10.2026): это первое и единственное, что наблюдатель
-- пишет. Отсюда политики ниже смотрят не на роль, а на авторство:
-- свою идею и свой голос пишет любой, чужие — нет.

create table ideas (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid not null references orgs (id) on delete cascade,
    -- Автор остаётся пустым, если человека стёрли: идея — собственность
    -- организации, а не личности, и с уходом автора не исчезает.
    author_id  uuid references users (id) on delete set null,
    -- «Кому» — подразделение, к которому идея обращена; пусто — всей
    -- организации. Подразделение убрали — идея остаётся общей.
    team_id    uuid references teams (id) on delete set null,
    title      text not null
        constraint ideas_title_not_empty check (btrim(title) <> '')
        constraint ideas_title_length check (length(title) <= 200),
    -- Пределы — чтобы идея оставалась идеей, а не документом: длинное
    -- обсуждение пойдёт в карточку, когда идею возьмут.
    body       text not null default ''
        constraint ideas_body_length check (length(body) <= 5000),
    created_at timestamptz not null default now()
);

create index ideas_org_idx on ideas (org_id, created_at desc);

-- Голос — одна строка на человека и идею: ключ таблицы и есть правило
-- «один голос», и повтор упирается в него, а не в проверку кодом.
create table idea_votes (
    org_id     uuid not null references orgs (id) on delete cascade,
    idea_id    uuid not null references ideas (id) on delete cascade,
    user_id    uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    primary key (idea_id, user_id)
);

create index idea_votes_user_idx on idea_votes (user_id);

alter table ideas enable row level security;
alter table ideas force  row level security;
alter table idea_votes enable row level security;
alter table idea_votes force  row level security;

create policy tenant_isolation on ideas as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));
create policy tenant_isolation on idea_votes as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Идеи и голоса видны всей организации: смысл списка — чтобы идею
-- увидели и поддержали, прятать здесь нечего.
create policy visible on ideas      for select using (true);
create policy visible on idea_votes for select using (true);

-- Подаёт идею любой — от своего имени.
create policy submit on ideas for insert
    with check (author_id = (select app_current_user()));
-- Правит и убирает — автор; владелец организации тоже, как везде,
-- где нужно поправить чужое после ухода человека.
create policy own on ideas for update
    using (author_id = (select app_current_user()) or (select app_is_owner()))
    with check (author_id = (select app_current_user()) or (select app_is_owner()));
create policy remove on ideas for delete
    using (author_id = (select app_current_user()) or (select app_is_owner()));

-- Голос — только свой, и ставится, и снимается.
create policy cast_vote on idea_votes for insert
    with check (user_id = (select app_current_user()));
create policy retract_vote on idea_votes for delete
    using (user_id = (select app_current_user()));
