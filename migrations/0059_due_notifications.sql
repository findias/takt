-- Уведомления по времени (ROADMAP 29.1): «срок блокировки твоей
-- карточки — меньше суток» и «твоя карточка перешагнула обещание доски».
--
-- Ни то ни другое не событие: никто ничего не сделал, просто прошло
-- время. Поэтому их замечает служебная задача `notify_due`, как снятие
-- по сроку замечает `expire_blocks`, — без человека, внутри одной
-- организации за раз.
--
-- Старая версия приложения новых поводов не пишет и не читает: ограничение
-- только расширяется, строки прежних поводов остаются годными.

alter table notifications drop constraint notifications_reason_known;
alter table notifications add constraint notifications_reason_known
    check (reason in ('mentioned', 'assigned', 'blocked', 'block_expired',
                      'block_ending', 'over_promise'));

-- Задача видит только то, из чего складываются её поводы, и только
-- в выставленной организации: открытые блокировки, карточки и доски.
create policy due_sees on card_blocks for select
    using ((select app_task()) = 'notify_due' and (select app_current_org()) is not null
           and unblocked_at is null);
create policy due_sees on cards for select
    using ((select app_task()) = 'notify_due' and (select app_current_org()) is not null);
create policy due_sees on boards for select
    using ((select app_task()) = 'notify_due' and (select app_current_org()) is not null);

-- Пишет свои два повода и без автора — время не автор.
create policy due_notifies on notifications for insert
    with check ((select app_task()) = 'notify_due'
                and reason in ('block_ending', 'over_promise')
                and actor_id is null);

-- Задача проходит раз в минуту, а сообщать о каждом сроке надо один
-- раз. Значит, ей нужно видеть, что уже сказано, — но только своё:
-- два своих повода, без чтения чужих уведомлений о прочем. Этого же
-- требует `on conflict do nothing`: под политиками он проверяет, что
-- новую строку можно прочитать.
create policy due_remembers on notifications for select
    using ((select app_task()) = 'notify_due'
           and reason in ('block_ending', 'over_promise'));
