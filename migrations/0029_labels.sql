-- Метки.
--
-- Устройство то же, что у своих полей карточки, и по той же причине:
-- определение принадлежит организации, а не доске. Метка «срочно»
-- на доске найма и такая же на доске поддержки — это одна метка, иначе
-- ни фильтр, ни сводный отчёт по организации собрать не из чего.
--
-- Почему метки, если есть поля вида select. Потому что читаются они
-- по-разному: поле отвечает на вопрос «какой», метка — «да или нет»,
-- и на карточке метка занимает столько же места, сколько слово. Все
-- разобранные доски держат и то, и другое.
--
-- Цвет — не значение, а имя оттенка. Хранить «#e07a5f» значит завести
-- цвет, который в тёмной теме начнёт светиться, и правило «сырых цветов
-- в правилах нет» перестанет действовать ровно там, где данные приходят
-- из базы. Набор закрытый и совпадает с оттенками аватаров.

create table labels (
    id     uuid primary key default gen_random_uuid(),
    org_id uuid not null references orgs (id) on delete cascade,
    name   text not null,
    tone   text not null default 'slate'
           constraint labels_tone_valid
           check (tone in ('slate', 'green', 'blue', 'violet', 'rose', 'amber', 'teal', 'brown')),
    created_at  timestamptz not null default now(),
    archived_at timestamptz,

    constraint labels_name_not_empty check (length(trim(name)) > 0)
);

-- Две метки с одним названием — гарантированная путаница: их начнут
-- вешать вперемешку, а фильтр будет показывать половину.
create unique index labels_name_idx
    on labels (org_id, lower(name)) where archived_at is null;

create table card_labels (
    org_id   uuid not null references orgs (id) on delete cascade,
    card_id  uuid not null references cards (id) on delete cascade,
    label_id uuid not null references labels (id) on delete cascade,
    -- Кто повесил и когда: метка — такое же решение о работе, как
    -- назначение исполнителя, и вопрос «кто это пометил» задают.
    added_at timestamptz not null default now(),
    added_by uuid references users (id),

    primary key (card_id, label_id)
);

-- Главный будущий запрос: «карточки с этой меткой».
create index card_labels_label_idx on card_labels (org_id, label_id);

-- --- Доступ ---

alter table labels      enable row level security;
alter table labels      force  row level security;
alter table card_labels enable row level security;
alter table card_labels force  row level security;

create policy tenant_isolation on labels as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));
create policy tenant_isolation on card_labels as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Какие метки заведены — словарь, а не данные: их видно всем в организации.
create policy visible on labels for select using (true);
create policy manage  on labels for all
    using ((select app_can_write())) with check ((select app_can_write()));

-- А что чем помечено — наследует видимость карточки, как значения полей.
create policy visible on card_labels for select
    using (card_id in (select id from cards));
create policy writable on card_labels for all
    using (card_id in (select id from cards) and (select app_can_write()))
    with check (card_id in (select id from cards) and (select app_can_write()));
