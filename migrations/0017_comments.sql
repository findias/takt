-- Обсуждение карточки.
--
-- Сама переписка — вещь простая. Необратимо в ней другое: то, чего
-- не записали в момент написания, потом не восстановить. Отсюда три
-- решения, каждое из которых задним числом не принимается.
--
-- ПЕРВОЕ: ветки с самого начала, глубиной ровно в один уровень. Плоский
-- список превращается в ветки миграцией всех обсуждений: у ответа нет
-- признака, на что он отвечал, и вывести это из текста нельзя. Обратное
-- дёшево — ветки показываются плоско одним запросом. Глубина ограничена
-- одним уровнем сознательно: ответ на ответ на ответ читать невозможно,
-- и все, кто пробовал, к этому и пришли.
--
-- ВТОРОЕ: правка сохраняет прежний текст. «Изменено» без прежнего текста
-- бесполезно: в разборе спрашивают не «правил ли он», а «что там было
-- написано до». Прежние версии складываются отдельной таблицей триггером,
-- то есть их сохранность не зависит от того, вспомнил ли о ней код.
--
-- ТРЕТЬЕ: упоминание хранится ссылкой на человека, а не разбирается
-- из текста при чтении. Человек может смениться именем, текст —
-- переписаться, а факт «его позвали в это обсуждение» должен пережить
-- и то, и другое.
--
-- Удаление мягкое. Строка остаётся: на неё ссылаются ответы, и вырезав
-- её, мы разорвали бы ветку, в которой отвечали живым людям.

create table card_comments (
    id        uuid primary key default gen_random_uuid(),
    org_id    uuid not null references orgs (id) on delete cascade,
    board_id  uuid not null references boards (id) on delete cascade,
    card_id   uuid not null references cards (id) on delete cascade,
    -- Ответ на комментарий. Ровно один уровень: у родителя ответа быть
    -- не может, за этим следит триггер ниже.
    parent_id uuid references card_comments (id) on delete restrict,
    author_id uuid references users (id),
    body      text not null constraint card_comments_body_not_empty check (btrim(body) <> ''),

    created_at timestamptz not null default now(),
    edited_at  timestamptz,
    deleted_at timestamptz,
    deleted_by uuid references users (id)
);

create index card_comments_card_idx on card_comments (card_id, created_at);
create index card_comments_parent_idx on card_comments (parent_id) where parent_id is not null;
create index card_comments_board_idx on card_comments (org_id, board_id, created_at desc);

-- Прежние версии текста. Пишутся триггером, а не кодом: сохранность
-- не должна зависеть от того, вспомнило ли о ней приложение.
create table card_comment_revisions (
    id         bigserial primary key,
    org_id     uuid not null references orgs (id) on delete cascade,
    comment_id uuid not null references card_comments (id) on delete cascade,
    body       text not null,
    -- Момент, в который этот текст перестал быть текущим.
    replaced_at timestamptz not null default now()
);

create index card_comment_revisions_idx on card_comment_revisions (comment_id, replaced_at desc);

-- Кого позвали. Ссылкой, а не разбором текста при чтении: имя сменится,
-- текст перепишется, а факт приглашения обязан пережить и то, и другое.
create table card_comment_mentions (
    org_id     uuid not null references orgs (id) on delete cascade,
    comment_id uuid not null references card_comments (id) on delete cascade,
    user_id    uuid not null references users (id) on delete cascade,
    primary key (comment_id, user_id)
);

create index card_comment_mentions_user_idx on card_comment_mentions (user_id, org_id);

-- --- Правила ---

create function card_comment_depth() returns trigger
language plpgsql as $$
declare
    grandparent uuid;
    parent_card uuid;
begin
    if new.parent_id is null then
        return new;
    end if;

    select parent_id, card_id into grandparent, parent_card
      from card_comments where id = new.parent_id;
    if parent_card is null then
        raise exception 'комментарий, на который отвечают, не найден'
            using errcode = 'foreign_key_violation';
    end if;
    if grandparent is not null then
        raise exception 'ответ на ответ: глубина обсуждения ограничена одним уровнем'
            using errcode = 'check_violation';
    end if;
    if parent_card <> new.card_id then
        raise exception 'ответ должен быть на комментарий той же карточки'
            using errcode = 'check_violation';
    end if;
    return new;
end $$;

create trigger card_comment_depth_check
    before insert or update of parent_id on card_comments
    for each row execute function card_comment_depth();

-- Правка сохраняет прежний текст. Условие на изменение тела: пометка
-- об удалении и прочие обновления версий не плодят.
create function card_comment_keep_previous() returns trigger
language plpgsql as $$
begin
    if new.body is distinct from old.body then
        insert into card_comment_revisions (org_id, comment_id, body)
        values (old.org_id, old.id, old.body);
        new.edited_at := now();
    end if;
    return new;
end $$;

create trigger card_comment_revision
    before update on card_comments
    for each row execute function card_comment_keep_previous();

-- --- Доступ ---

alter table card_comments          enable row level security;
alter table card_comments          force  row level security;
alter table card_comment_revisions enable row level security;
alter table card_comment_revisions force  row level security;
alter table card_comment_mentions  enable row level security;
alter table card_comment_mentions  force  row level security;

do $$
declare t text;
begin
    foreach t in array array['card_comments', 'card_comment_revisions', 'card_comment_mentions'] loop
        execute format(
            'create policy tenant_isolation on %I as restrictive
               using (org_id = (select app_current_org()))
               with check (org_id = coalesce((select app_current_org()), org_id))', t);
    end loop;
end $$;

-- Обсуждение наследует видимость доски, как всё, что на ней висит.
create policy visible on card_comments for select
    using (board_id = any (array(select unnest(app_visible_boards()))));
create policy writable on card_comments for all
    using (board_id = any (array(select unnest(app_writable_boards()))))
    with check (board_id = any (array(select unnest(app_writable_boards()))));

create policy visible on card_comment_revisions for select
    using (comment_id in (select id from card_comments));
-- Политики update и delete нет намеренно: прежние версии только
-- дописываются, переписать историю правок нельзя.
create policy appendable on card_comment_revisions for insert
    with check (comment_id in (select id from card_comments));

create policy visible on card_comment_mentions for select
    using (comment_id in (select id from card_comments));
create policy writable on card_comment_mentions for all
    using (comment_id in (select id from card_comments) and (select app_can_write()))
    with check (comment_id in (select id from card_comments) and (select app_can_write()));
