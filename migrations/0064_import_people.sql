-- Выбор по людям источника переноса (ROADMAP 23.6).
--
-- В предпросмотре переноса у каждого исполнителя источника выбирают:
-- сопоставить с участником, завести или не переносить. Повторный перенос
-- той же доски (а он бывает: люди заводятся потом, доска дописывается)
-- обязан помнить выбор, иначе его делают заново каждый раз, и «Иван
-- из YouGile — это наш Иван Петров» приходится угадывать опять.
--
-- Хранится только сделанное руками: сопоставление по почте находится
-- само и хранить его — значит однажды помешать ему найти человека,
-- которого завели позже. «Завести» хранится как сопоставление с тем,
-- кого завели.
create table import_people (
    org_id     uuid        not null references orgs (id) on delete cascade,
    -- Источник: yougile, table и так далее — ключи людей у каждого свои.
    source     text        not null,
    -- Почта, если источник её дал, иначе source:<идентификатор в источнике>.
    person_key text        not null,
    action     text        not null
               constraint import_people_action_valid check (action in ('match', 'skip')),
    user_id    uuid        references users (id) on delete cascade,
    decided_by uuid        references users (id) on delete set null,
    decided_at timestamptz not null default now(),
    primary key (org_id, source, person_key),
    constraint import_people_match_has_user check ((action = 'match') = (user_id is not null))
);

alter table import_people enable row level security;
alter table import_people force  row level security;

create policy tenant_isolation on import_people as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = (select app_current_org()));

-- Переносит тот, кто пишет в организации, — он же и помнит выбор.
create policy writers on import_people
    using ((select app_can_write()))
    with check ((select app_can_write()));
