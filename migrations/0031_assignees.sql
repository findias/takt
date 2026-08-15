-- Исполнителей у карточки может быть несколько.
--
-- В миграции 0028 было записано обратное: «один, а не список», с доводом
-- «делают трое — значит не делает никто конкретно». Довод верен для
-- ответственности и неверен для работы. Пара, севшая за одну задачу,
-- смежник, которого позвали на день, проверяющий, который её же и
-- доводит, — всё это существует, и до сих пор про это приходилось врать:
-- назначать одного и дописывать остальных в описание, где их не найдёт
-- ни фильтр, ни отчёт.
--
-- Ответственность за это не размывается: первым в списке остаётся тот,
-- кого назначили первым, и порядок мы сохраняем — по времени добавления.
-- Кто отвечает — вопрос к порядку, а не к схеме.
--
-- Устройство то же, что у меток: связь карточки с человеком, а не поле.
-- Причина та же — вопросов «кто и когда взялся» будет больше, чем одно
-- поле может ответить, а место для ответа есть только у связи.
--
-- Ссылка на users, а не на memberships, — как и прежде: человек может
-- уйти из организации, а карточки, которые он делал, останутся вместе
-- с подписью, кто их делал. Проверку «состоит в организации» делает
-- операция назначения.

create table card_assignees (
    org_id  uuid not null references orgs (id) on delete cascade,
    card_id uuid not null references cards (id) on delete cascade,
    user_id uuid not null references users (id),
    -- Когда взялся и кто назначил. Первый по времени — тот, кого
    -- назначили первым: порядок в списке несёт смысл и потому хранится,
    -- а не восстанавливается сортировкой по имени.
    added_at timestamptz not null default now(),
    added_by uuid references users (id),

    primary key (card_id, user_id)
);

-- «Что на мне» — главный вопрос к доске после «что происходит».
create index card_assignees_user_idx on card_assignees (org_id, user_id);

-- Прежние назначения переезжают: исполнитель, проставленный до этой
-- миграции, остаётся исполнителем и становится первым в списке.
insert into card_assignees (org_id, card_id, user_id, added_at)
select org_id, id, assignee_id, coalesce(updated_at, created_at)
  from cards
 where assignee_id is not null;

drop index if exists cards_assignee_idx;
alter table cards drop column assignee_id;

-- --- Доступ ---

alter table card_assignees enable row level security;
alter table card_assignees force  row level security;

create policy tenant_isolation on card_assignees as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Кто над чем работает — наследует видимость карточки, как метки
-- и значения полей: видно тем, кому видна сама карточка.
create policy visible on card_assignees for select
    using (card_id in (select id from cards));
create policy writable on card_assignees for all
    using (card_id in (select id from cards) and (select app_can_write()))
    with check (card_id in (select id from cards) and (select app_can_write()));
