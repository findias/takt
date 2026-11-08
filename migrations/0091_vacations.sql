-- Отпуска не меняют исполнителей и факты потока. Старое приложение
-- новых таблиц не знает; часовой пояс имеет безопасное умолчание.
alter table orgs add column time_zone text not null default 'UTC';

create table vacations (
    id uuid primary key,
    org_id uuid not null references orgs(id) on delete cascade,
    user_id uuid not null references users(id) on delete cascade,
    starts_on date not null,
    ends_on date not null,
    note text not null default '' check (length(note) <= 500),
    cancelled boolean not null default false,
    version bigint not null default 1,
    created_at timestamptz not null default now(),
    check (starts_on >= date '0001-01-01' and ends_on <= date '9999-12-31' and starts_on <= ends_on)
);
create index vacations_person_idx on vacations(org_id, user_id, starts_on);
alter table vacations enable row level security;
alter table vacations force row level security;
create policy tenant_isolation on vacations as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = (select app_current_org()));
create policy visible on vacations for select using (true);
create policy own_insert on vacations for insert
    with check (user_id = (select app_current_user()) and exists (
        select 1 from memberships m join users u on u.id = m.user_id
        where m.org_id = vacations.org_id and m.user_id = app_current_user() and u.kind = 'person'));
create policy own_update on vacations for update
    using (user_id = (select app_current_user()))
    with check (user_id = (select app_current_user()));
create trigger audit after insert or update on vacations
    for each row execute function audit_write('id');

-- Как app_sees_board_as: проверяем того же человека теми же политиками,
-- не воспроизводим правила закрытых досок в сервисе замещения.
create function app_writes_board_as(u uuid, b uuid) returns boolean
language plpgsql volatile as $fn$
declare me text := current_setting('app.current_user', true); allowed boolean;
begin
    if not exists (select 1 from boards where id = b) then return false; end if;
    perform set_config('app.current_user', u::text, true);
    allowed := b = any(app_writable_boards());
    perform set_config('app.current_user', coalesce(me, ''), true);
    return allowed;
end $fn$;

create table card_coverages (
    id uuid primary key,
    org_id uuid not null references orgs(id) on delete cascade,
    vacation_id uuid not null references vacations(id) on delete cascade,
    vacation_version bigint not null,
    card_id uuid not null references cards(id) on delete cascade,
    substitute_id uuid not null references users(id) on delete cascade,
    starts_on date not null,
    ends_on date not null,
    status text not null default 'proposed' check (status in ('proposed', 'accepted', 'declined', 'cancelled')),
    created_by uuid references users(id) on delete set null,
    version bigint not null default 1,
    created_at timestamptz not null default now(),
    check (starts_on >= date '0001-01-01' and ends_on <= date '9999-12-31' and starts_on <= ends_on)
);
create index card_coverages_card_idx on card_coverages(card_id);
create index card_coverages_substitute_idx on card_coverages(org_id, substitute_id, ends_on);
alter table card_coverages enable row level security;
alter table card_coverages force row level security;
create policy tenant_isolation on card_coverages as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = (select app_current_org()));
create policy visible on card_coverages for select using (exists (
    select 1 from cards c where c.id = card_id));
create policy propose on card_coverages for insert with check (
    status = 'proposed' and created_by = (select app_current_user()) and exists (
        select 1 from cards c where c.id = card_id
        and c.board_id = any(array(select unnest(app_writable_boards()))))
    and exists (select 1 from vacations v where v.id=vacation_id and v.org_id=card_coverages.org_id
        and not v.cancelled and v.version=vacation_version and v.user_id<>substitute_id
        and card_coverages.starts_on>=v.starts_on and card_coverages.ends_on<=v.ends_on
        and exists(select 1 from card_assignees a where a.card_id=card_coverages.card_id and a.user_id=v.user_id))
    and exists(select 1 from memberships m join users u on u.id=m.user_id
        where m.org_id=card_coverages.org_id and m.user_id=substitute_id and u.kind='person')
    and exists(select 1 from cards c where c.id=card_id and app_writes_board_as(substitute_id,c.board_id)));
create policy respond on card_coverages for update using (
    substitute_id = (select app_current_user())
    or exists (select 1 from vacations v where v.id = vacation_id and v.user_id = app_current_user())
    or exists (select 1 from cards c where c.id = card_id
        and c.board_id = any(array(select unnest(app_writable_boards())))))
    with check (true);

-- RLS определяет строки, триггер — допустимые изменения полей:
-- заместитель вправе ответить, но не переписать адресованную ему задачу.
create function coverage_transition() returns trigger language plpgsql as $fn$
begin
    if new.id <> old.id or new.org_id <> old.org_id or new.vacation_id <> old.vacation_id
       or new.vacation_version <> old.vacation_version or new.card_id <> old.card_id
       or new.substitute_id <> old.substitute_id or new.starts_on <> old.starts_on
       or new.ends_on <> old.ends_on or new.created_by is distinct from old.created_by then
        raise exception 'изменить можно только ответ на замещение';
    end if;
    if new.status = old.status then return new; end if;
    if new.status in ('accepted', 'declined') then
        if old.status <> 'proposed' or new.substitute_id <> app_current_user() then
            raise exception 'ответить на замещение может только приглашённый человек';
        end if;
    elsif new.status <> 'cancelled' then
        raise exception 'изменить можно только ответ на замещение';
    end if;
    return new;
end $fn$;
create trigger coverage_transition before update on card_coverages
    for each row execute function coverage_transition();
create trigger audit after insert or update on card_coverages
    for each row execute function audit_write('id');

alter table notifications drop constraint notifications_reason_known;
alter table notifications add constraint notifications_reason_known check (reason in (
    'mentioned', 'assigned', 'blocked', 'block_expired', 'block_ending', 'over_promise', 'idea_decided', 'coverage_changed'));
-- Отмену отпуска сообщает его автор даже после потери права записи
-- на доску; содержание уведомления всё равно защищено видимостью доски.
create policy coverage_notice on notifications for insert with check (
    reason = 'coverage_changed' and actor_id = app_current_user() and exists (
        select 1 from card_coverages cc join vacations v on v.id = cc.vacation_id
        join cards c on c.id = cc.card_id
        where cc.card_id = notifications.card_id and c.board_id = notifications.board_id
        and (cc.substitute_id = notifications.recipient_id or v.user_id = notifications.recipient_id or cc.created_by = notifications.recipient_id)
        and (cc.substitute_id = app_current_user() or v.user_id = app_current_user())));
