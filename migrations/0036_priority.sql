-- Приоритет уровнями: от низкого до наивысшего.
--
-- Миграцией раньше (0035) заведён был класс обслуживания — «по каким
-- правилам это тянут», а не «что раньше». Решение принято другое:
-- приоритет нужен уровнями, и срочность — это верхний уровень, а не
-- отдельная наклейка. Двух механизмов про одно и то же в системе быть
-- не должно, поэтому класс не остаётся рядом, а превращается в уровень.
--
-- Уровней четыре: низкий, средний, высокий, наивысший. Средний —
-- умолчание и середина шкалы; чётное число уровней без середины
-- заставляло бы выбирать сторону там, где выбирать нечего, а пять
-- и больше — это шкала, которой никто не пользуется целиком: в любой
-- команде живут «обычное», «важное» и «горит».
--
-- Хранится словом, а не числом. Число пришлось бы читать по легенде
-- («1 — это высокий или низкий?»), а порядок сравнения всё равно нужен
-- отдельной таблицей: сортировать по алфавиту слов нельзя. Порядок
-- задаётся функцией ниже — одним местом, из которого его берут
-- и запросы, и клиент.
--
-- Прежние значения переносятся по смыслу: срочное было тем самым
-- верхним уровнем, фоновое — нижним, обычное — серединой.

alter table cards
    add column priority text not null default 'medium'
        constraint cards_priority_valid
        check (priority in ('low', 'medium', 'high', 'highest'));

-- Перенос значений. Политика арендатора снимается на время правки:
-- миграция идёт под ролью приложения, арендатора у неё нет, и без
-- этого update прошёл бы вхолостую, а колонку мы тут же удаляем —
-- восстанавливать было бы неоткуда.
alter table cards no force row level security;

do $$
begin
    if (select relforcerowsecurity from pg_class
         where relname = 'cards' and relnamespace = 'public'::regnamespace) then
        raise exception 'перенос пошёл бы вхолостую: политики на cards ещё действуют';
    end if;
end $$;

update cards set priority = case service_class
    when 'expedite' then 'highest'
    when 'filler'   then 'low'
    else 'medium'
end;

alter table cards force row level security;

drop index if exists cards_expedite_idx;
alter table cards drop column service_class;

-- Спрашивают «что у нас горит», то есть про верхние уровни. Частичный
-- индекс ровно под этот вопрос: полный был бы индексом по колонке
-- из одного повторяющегося значения.
create index cards_priority_idx on cards (board_id)
    where priority in ('high', 'highest') and archived_at is null;

-- Порядок уровней — одним местом на всю систему: сортировка по слову
-- дала бы алфавит, а числовая колонка рядом со словом разошлась бы
-- с ним в первый же день.
create function card_priority_rank(priority text) returns int
language sql immutable parallel safe
as $$ select case priority
    when 'highest' then 0
    when 'high'    then 1
    when 'medium'  then 2
    else 3
end $$;
