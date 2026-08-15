-- Свои поля у карточки.
--
-- Собственное исследование проекта требовало завести их с первого дня,
-- и причина не в удобстве: фильтры, представления и отчёты строятся
-- поверх модели полей. Введённые позже, они переписывают всё это целиком,
-- потому что до них фильтр умеет спрашивать только про то, что вшито
-- в схему, а после — про что угодно.
--
-- Два решения, которые дальше не переиграть.
--
-- ПЕРВОЕ: определения принадлежат организации, а не доске. Поле «Заказчик»
-- на доске найма и такое же поле на доске поддержки — это одно поле,
-- иначе сводный отчёт по организации собрать не из чего: он будет
-- складывать разные сущности с одинаковым названием. Сузить область
-- определения позже можно добавлением колонки, расширить — нет.
--
-- ВТОРОЕ: значения лежат в типизированных колонках, а не в одном jsonb.
-- Разница проявится не сегодня, а когда по этим значениям начнут
-- фильтровать и считать: по числу в jsonb нельзя сравнивать без приведения
-- на каждой строке, дата в нём — строка, а сортировка строк по дате даёт
-- всем известный результат. Разреженность таблицы — цена, и она мелкая
-- рядом с этим.

create table card_fields (
    id     uuid primary key default gen_random_uuid(),
    org_id uuid not null references orgs (id) on delete cascade,
    name   text not null,
    kind   text not null
           constraint card_fields_kind_valid
           check (kind in ('text', 'number', 'date', 'select', 'checkbox')),
    -- Варианты для kind = select: массив строк. Идентификатором варианта
    -- служит он сам: переименование варианта — это и есть смена значения,
    -- и делать вид, что нет, значит завести молчаливую подмену данных.
    options    jsonb       not null default '[]'::jsonb,
    created_at timestamptz not null default now(),
    archived_at timestamptz,

    constraint card_fields_options_are_array check (jsonb_typeof(options) = 'array'),
    constraint card_fields_select_has_options
        check (kind <> 'select' or jsonb_array_length(options) > 0)
);

-- Два поля с одним названием в одной организации — гарантированная
-- путаница в отчётах. Регистр не считается различием по той же причине.
create unique index card_fields_name_idx
    on card_fields (org_id, lower(name)) where archived_at is null;

create table card_field_values (
    org_id   uuid not null references orgs (id) on delete cascade,
    card_id  uuid not null references cards (id) on delete cascade,
    field_id uuid not null references card_fields (id) on delete cascade,

    value_text   text,
    value_number numeric(18, 4),
    value_date   date,
    value_bool   boolean,
    value_option text,

    updated_at timestamptz not null default now(),
    updated_by uuid references users (id),

    primary key (card_id, field_id),
    -- Ровно одно значение заполнено. Пустое значение хранить незачем:
    -- «поля нет» и «поле пустое» — одно и то же, и различать их значило бы
    -- заводить третье состояние, которого никто не ждёт.
    constraint card_field_values_exactly_one check (
        (value_text is not null)::int + (value_number is not null)::int +
        (value_date is not null)::int + (value_bool is not null)::int +
        (value_option is not null)::int = 1
    )
);

create index card_field_values_field_idx on card_field_values (org_id, field_id);
-- Под будущие фильтры: «карточки, у которых поле равно такому-то».
create index card_field_values_text_idx   on card_field_values (field_id, value_text)   where value_text is not null;
create index card_field_values_number_idx on card_field_values (field_id, value_number) where value_number is not null;
create index card_field_values_date_idx   on card_field_values (field_id, value_date)   where value_date is not null;
create index card_field_values_option_idx on card_field_values (field_id, value_option) where value_option is not null;

-- --- Значение обязано соответствовать виду поля ---
--
-- Проверить это ограничением таблицы нельзя: вид лежит в другой таблице.
-- Значит триггер — и значит правило держит база, а не договорённость
-- между тремя местами в коде.

create function card_field_value_matches_kind() returns trigger
language plpgsql as $$
declare
    field_kind text;
    field_options jsonb;
begin
    select kind, options into field_kind, field_options
      from card_fields where id = new.field_id;
    if field_kind is null then
        raise exception 'поле не найдено' using errcode = 'foreign_key_violation';
    end if;

    if (field_kind = 'text'     and new.value_text   is null)
    or (field_kind = 'number'   and new.value_number is null)
    or (field_kind = 'date'     and new.value_date   is null)
    or (field_kind = 'checkbox' and new.value_bool   is null)
    or (field_kind = 'select'   and new.value_option is null) then
        raise exception 'значение не соответствует виду поля «%»', field_kind
            using errcode = 'check_violation';
    end if;

    -- Вариант обязан быть из списка. Иначе список превращается
    -- в рекомендацию, а отчёт по вариантам — в перечисление опечаток.
    if field_kind = 'select'
       and not (field_options ? new.value_option) then
        raise exception 'вариант «%» не из списка поля', new.value_option
            using errcode = 'check_violation';
    end if;
    return new;
end $$;

create trigger card_field_value_kind
    before insert or update on card_field_values
    for each row execute function card_field_value_matches_kind();

-- --- Доступ ---

alter table card_fields       enable row level security;
alter table card_fields       force  row level security;
alter table card_field_values enable row level security;
alter table card_field_values force  row level security;

create policy tenant_isolation on card_fields as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));
create policy tenant_isolation on card_field_values as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Какие поля заведены в организации — не тайна: это словарь, а не данные.
create policy visible on card_fields for select using (true);
create policy manage  on card_fields for all
    using ((select app_can_write())) with check ((select app_can_write()));

-- Значения наследуют видимость карточки, как связи и блокировки.
create policy visible on card_field_values for select
    using (card_id in (select id from cards));
create policy writable on card_field_values for all
    using (card_id in (select id from cards) and (select app_can_write()))
    with check (card_id in (select id from cards) and (select app_can_write()));
