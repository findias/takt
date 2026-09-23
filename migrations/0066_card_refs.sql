-- Ссылки карточки на заявки внешних систем: RDS, ЗНО, ЗНИ, проблемы.
--
-- Работа на доске почти всегда вызвана чем-то из сервис-деска: запросом
-- на обслуживание, запросом на изменение, проблемой. До сих пор номер
-- заявки писали в описание или в своё поле, и оба пути плохи. В описании
-- номер не найти и не отличить ЗНО от ЗНИ. Своё поле держит одно значение,
-- а у одной работы бывает три ЗНО и изменение, которым её выкатят.
-- Отсюда своя таблица: много ссылок на карточку, у каждой вид.
--
-- Виды закрыты перечислением, а не словарём организации: их четыре,
-- они названы заказчиком, и отчёт «работа по ЗНИ» должен сравнивать
-- один и тот же вид, а не одинаковые подписи. Новый вид — новая миграция,
-- осознанно.
--
-- Ссылка — строкой, как её дали: номер («ЗНО-10492») или адрес. Разбирать
-- адреса чужих систем не наше дело, а номер без адреса — тоже ссылка.
create table card_refs (
    id         uuid        primary key default gen_random_uuid(),
    org_id     uuid        not null references orgs (id) on delete cascade,
    card_id    uuid        not null references cards (id) on delete cascade,
    kind       text        not null
               constraint card_refs_kind_valid
               check (kind in ('rds', 'zno', 'zni', 'problem')),
    ref        text        not null
               constraint card_refs_ref_valid
               check (length(btrim(ref)) between 1 and 500),
    created_at timestamptz not null default now(),
    created_by uuid        references users (id)
);

-- Одна и та же заявка дважды на карточке — не две ссылки, а опечатка.
create unique index card_refs_unique_idx on card_refs (card_id, kind, ref);
-- Под обратный вопрос «какая работа идёт по этой заявке».
create index card_refs_lookup_idx on card_refs (org_id, kind, ref);

alter table card_refs enable row level security;
alter table card_refs force  row level security;

create policy tenant_isolation on card_refs as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));
-- Видимость — карточки, как у значений своих полей: карточка может
-- переехать на другую доску, и ссылки обязаны ехать с ней.
create policy visible on card_refs for select
    using (card_id in (select id from cards));
create policy writable on card_refs for all
    using (card_id in (select id from cards) and (select app_can_write()))
    with check (card_id in (select id from cards) and (select app_can_write()));
