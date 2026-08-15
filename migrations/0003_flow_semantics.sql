-- Семантика колонок и отметки времени потока.
--
-- Журнал переходов у нас есть с первой миграции, но по нему нельзя посчитать
-- ни время цикла, ни старение: неизвестно, какой переход означает «работа
-- началась», а какой — «работа закончена». Kanban Guide определяет все четыре
-- базовые метрики (WIP, пропускная способность, возраст карточки, время цикла)
-- именно через точки старта и финиша, а не через «в какой колонке лежит».
--
-- Это решение необратимо задним числом ровно в том же смысле, что и сам
-- журнал: разметить колонки можно когда угодно, но события, записанные до
-- разметки, останутся без ответа на вопрос «шла ли тогда работа».
--
-- Вторая половина миграции — денормализованные отметки на карточке. Их можно
-- было бы каждый раз вычислять из журнала, но старение карточки нужно на
-- каждой отрисовке доски, а это запрос по всей истории на каждую карточку.

-- --- 1. Колонка перестаёт быть просто заголовком ---

alter table board_columns
    add column kind text not null default 'in_progress'
        constraint board_columns_kind_valid
        check (kind in ('queue', 'in_progress', 'done')),
    -- Точки старта и финиша — не то же самое, что вид колонки: очередей
    -- и стадий работы бывает много, а границей потока объявляются конкретные.
    add column is_started_point  boolean not null default false,
    add column is_finished_point boolean not null default false,
    -- Политика входа в колонку простым текстом — как Definition of Done
    -- на колонке в Azure DevOps. Смысл в том, чтобы правило было явным.
    add column policy text not null default '',
    -- Мягкий лимит по умолчанию: превышение подсвечивается, но не запрещается.
    -- Так сделали все, кто мог сделать иначе, — Jira, Azure DevOps, GitHub.
    -- Жёсткий лимит остаётся осознанным выбором конкретной колонки.
    add column wip_limit_hard boolean not null default false;

alter table board_columns
    add constraint board_columns_wip_limit_positive
    check (wip_limit is null or wip_limit > 0);

-- Разметка уже существующих досок. Все они заведены по одному шаблону
-- «очередь → работа → готово», поэтому крайние колонки читаются по позиции.
--
-- Политики арендатора на время правки снимаются: миграция идёт под ролью
-- приложения, а той видно только строки своей организации — и «своей»
-- у неё сейчас нет ни одной. Без этого запросы ниже прошли бы, не тронув
-- ни строки, и разметка молча не случилась бы. Владельцу таблицы хватает
-- «no force», суперправа не нужны.
alter table board_columns no force row level security;
alter table cards         no force row level security;
alter table card_events   no force row level security;
with ordered as (
    select id,
           row_number() over (partition by board_id order by position) as n,
           count(*)     over (partition by board_id)                   as total
      from board_columns
     where archived_at is null
)
update board_columns c
   set kind = case
                  when o.n = 1       then 'queue'
                  when o.n = o.total then 'done'
                  else 'in_progress'
              end
  from ordered o
 where c.id = o.id and o.total >= 2;

-- Точка старта — первая стадия работы, точка финиша — первая колонка «готово».
update board_columns c
   set is_started_point = true
  from (select distinct on (board_id) id
          from board_columns
         where archived_at is null and kind = 'in_progress'
         order by board_id, position) f
 where c.id = f.id;

update board_columns c
   set is_finished_point = true
  from (select distinct on (board_id) id
          from board_columns
         where archived_at is null and kind = 'done'
         order by board_id, position) f
 where c.id = f.id;

-- --- 2. Отметки времени на карточке ---

alter table cards
    -- Когда карточка вошла в текущую колонку. Из этого считается старение,
    -- и это не created_at: карточка могла пролежать в очереди месяц.
    add column column_entered_at timestamptz not null default now(),
    -- Момент пересечения точки старта. Ставится один раз: возврат карточки
    -- в очередь не обнуляет начало работы, иначе время цикла станет ложью.
    add column started_at  timestamptz,
    -- Момент пересечения точки финиша. Снимается, если карточку забрали
    -- обратно в работу, — тогда она снова считается незавершённой.
    add column finished_at timestamptz;

-- Восстанавливаем вход в колонку из журнала: последний переход, а если
-- карточку ни разу не двигали — момент создания.
update cards c
   set column_entered_at = coalesce(
           (select max(e.at)
              from card_events e
             where e.card_id = c.id and e.type = 'moved'),
           c.created_at);

-- Политики возвращаются на место: снимались они только на время правки
-- данных, и оставить их снятыми значит отключить изоляцию арендаторов.
alter table board_columns force row level security;
alter table cards         force row level security;
alter table card_events   force row level security;
