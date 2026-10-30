-- Уведомление о решении по идее (ROADMAP 43, срез 3): автору и
-- поддержавшим приходит «по идее решили» — тот ответ, без которого
-- банки идей и умирают. Уведомление прежде было только о карточке:
-- доска и карточка обязательны. Теперь оно бывает об идее: доска
-- и карточка пустые, идея заполнена.
--
-- Снятие not null безопасно для прошлой версии: она пишет оба поля
-- всегда, а читает уведомления через join с cards — строку об идее
-- просто не увидит.

alter table notifications alter column board_id drop not null;
alter table notifications alter column card_id drop not null;
alter table notifications add column idea_id uuid references ideas (id) on delete cascade;
alter table notifications add constraint notifications_about_something
    check (card_id is not null or idea_id is not null);

alter table notifications drop constraint notifications_reason_known;
alter table notifications add constraint notifications_reason_known
    check (reason in ('mentioned', 'assigned', 'blocked', 'block_expired',
                      'block_ending', 'over_promise', 'idea_decided'));

create index notifications_idea_idx on notifications (idea_id) where idea_id is not null;

-- Политики: уведомление о карточке видно, пока видна доска; об идее —
-- всегда, идеи открыты всей организации. Пишет о решении тот, кто
-- вправе решать (0084): владелец организации или владелец подразделения,
-- которому идея адресована.
drop policy own on notifications;
create policy own on notifications for select
    using (
        recipient_id = (select app_current_user())
        and (board_id = any (array(select unnest(app_visible_boards())))
             or (board_id is null and idea_id is not null))
    );

drop policy mark_own on notifications;
create policy mark_own on notifications for update
    using (
        recipient_id = (select app_current_user())
        and (board_id = any (array(select unnest(app_visible_boards())))
             or (board_id is null and idea_id is not null))
    )
    with check (recipient_id = (select app_current_user()));

drop policy notifies on notifications;
create policy notifies on notifications for insert
    with check (
        actor_id = (select app_current_user())
        and (board_id = any (array(select unnest(app_writable_boards())))
             or (board_id is null
                 and reason = 'idea_decided'
                 and exists (
                     select 1 from ideas i
                      where i.id = idea_id
                        and ((select app_is_owner())
                             or (i.team_id is not null
                                 and i.team_id = any (array(select unnest(app_admin_teams()))))))))
    );
