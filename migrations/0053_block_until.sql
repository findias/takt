-- У блокировки есть срок, и он сам её снимает (ROADMAP 28.1).
--
-- Блокировка была интервалом без конца: «ждём поставку до четверга»
-- заставляло вернуться в карточку в четверг, а забытая блокировка
-- копила время в блоке — и врали метрики потока, ради которых она
-- и сделана интервалом.
--
-- Срок — момент, а не дата. У карточки срок `due_on` — дата: обещание
-- наружу живёт днями. «До пятницы, 18:00» — конкретное время, и снятие
-- в полночь по часам сервера было бы снятием не тогда, когда обещали.
-- Пусто — «пока не снимут», и это остаётся нормой.
--
-- Старая версия приложения на этой схеме работает как прежде: колонку
-- она не пишет и не читает, признака задачи (ниже) не ставит.

alter table card_blocks add column blocked_until timestamptz;

-- Проход ищет истёкшее каждую минуту — по индексу, а не перебором всех
-- открытых блокировок всех организаций.
create index card_blocks_expiring on card_blocks (blocked_until)
    where unblocked_at is null and blocked_until is not null;

-- --- Служебная задача ---
--
-- Снимает проход в самом сервере, без человека. Политики блокировок,
-- журнала и досок требуют человека (`app_can_write()`), и под ними
-- проход отработал бы молча и впустую — ровно та грабля, на которой
-- стояли с правкой данных в миграции (0031) и с работником вебхуков.
-- Обходить политики отдельной ролью не берём: такую дыру потом никто
-- не пересматривает. Вместо этого у задачи своё имя в области
-- транзакции, и политики ниже открывают ей ровно то, что она делает.
--
-- Имя ставит только код приложения — так же, как арендатора и человека:
-- снаружи настроек транзакции не достать.
create function app_task() returns text
language sql stable parallel safe
as $$ select nullif(current_setting('app.task', true), '') $$;

-- Шаг первый — без арендатора: какие организации пора обойти.
--
-- Ограничивающая политика строгая и без арендатора не отдаёт ничего.
-- Ослабляется она приёмом, который уже стоит у подписок (0022): без
-- арендатора ограничение пропускает строку, а решают разрешающие
-- политики. Их для этого случая одна — узкая, ниже: служебной задаче
-- без действующего лица видно только открытые блокировки с вышедшим
-- сроком. Остальным областям без арендатора (приглашение, ключ)
-- разрешающих политик на блокировки нет: `visible` и `writable` смотрят
-- в карточки, а карточки без арендатора не видны никому.
alter policy tenant_isolation on card_blocks
    using (org_id = coalesce((select app_current_org()), org_id))
    with check (org_id = (select app_current_org()));

create policy expiry_finds on card_blocks for select
    using ((select app_no_actor())
           and (select app_task()) = 'expire_blocks'
           and unblocked_at is null
           and blocked_until <= now());

-- Шаг второй — в каждой организации, с арендатором: закрыть истёкшее,
-- записать событие, поднять версию доски, положить доставку подписчикам.
-- Всё — только внутри своей организации: арендатор выставлен, и строгие
-- ограничения остальных таблиц действуют как обычно.
create policy expiry_sees on card_blocks for select
    using ((select app_task()) = 'expire_blocks' and (select app_current_org()) is not null);
create policy expiry_closes on card_blocks for update
    using ((select app_task()) = 'expire_blocks'
           and (select app_current_org()) is not null
           and unblocked_at is null
           and blocked_until <= now())
    -- Закрывается моментом срока и без автора — иначе это не снятие
    -- по сроку, а что-то другое, и задача его делать не вправе.
    with check ((select app_task()) = 'expire_blocks'
                and unblocked_at = blocked_until
                and unblocked_by is null);

create policy expiry_sees on cards for select
    using ((select app_task()) = 'expire_blocks' and (select app_current_org()) is not null);

create policy expiry_sees on boards for select
    using ((select app_task()) = 'expire_blocks' and (select app_current_org()) is not null);
create policy expiry_bumps on boards for update
    using ((select app_task()) = 'expire_blocks' and (select app_current_org()) is not null)
    with check ((select app_task()) = 'expire_blocks');

create policy expiry_logs on card_events for insert
    with check ((select app_task()) = 'expire_blocks' and type = 'block_expired');

create policy expiry_reads_hooks on webhooks for select
    using ((select app_task()) = 'expire_blocks' and (select app_current_org()) is not null);
create policy expiry_enqueues on webhook_deliveries for insert
    with check ((select app_task()) = 'expire_blocks' and event = 'card.block_expired');
