-- Именованные срезы выгрузки для руководства (этап 24.2).
--
-- Срез — это отбор «Отчётов», сохранённый под именем: «что закрыли
-- за квартал», «где застревает». Без него каждый отчёт собирается
-- заново, и два отчёта за разные месяцы оказываются посчитаны
-- по-разному — а объяснять расхождение будет тот, кто показывает.
--
-- Хранится так же, как сохранённый вид доски (0030): строкой запроса,
-- ровно той, что стоит в адресе экрана. Период в ней — либо словом
-- (period=lastQuarter), либо датами: «прошлый квартал», открытый
-- в январе, обязан дать октябрь–декабрь, а не тот квартал, когда срез
-- сохранили. Разбирать строку на колонки значило бы менять схему
-- с каждым новым параметром отбора.
--
-- Принадлежит человеку, а не организации — по той же причине, что вид
-- доски: список чужих сохранённых отборов рассказывает, кто чем занят.
-- Поделиться срезом можно ссылкой: отбор целиком лежит в адресе.
-- К доске срез не привязан: отбор в «Отчётах» охватывает многие доски.

create table report_slices (
    id         uuid primary key default gen_random_uuid(),
    org_id     uuid not null references orgs (id) on delete cascade,
    user_id    uuid not null references users (id) on delete cascade,
    name       text not null,
    query      text not null default '',
    created_at timestamptz not null default now(),

    constraint report_slices_name_not_empty check (length(trim(name)) > 0),
    constraint report_slices_name_length check (length(name) <= 200),
    constraint report_slices_query_length check (length(query) <= 8000)
);

-- Два среза с одним названием у одного человека — опечатка.
create unique index report_slices_name_idx
    on report_slices (org_id, user_id, lower(name));

alter table report_slices enable row level security;
alter table report_slices force  row level security;

create policy tenant_isolation on report_slices as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

create policy own on report_slices for all
    using (user_id = (select app_current_user()))
    with check (user_id = (select app_current_user()));
