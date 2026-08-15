-- Обещание доски: за сколько работа проходит доску.
--
-- Kanban Guide называет это SLE и требует шести элементов в определении
-- потока; пять у нас были, этого не было. Дословно: «forecast of how long
-- it should take a work item to flow from started to finished… two parts:
-- a period of elapsed time and a probability… should be based on historical
-- cycle time and, once calculated, should be visualized on the DoW».
--
-- Почему это поле, а не вычисление. Процентиль по истории у нас считается
-- и показывается в отчёте — но он пересчитывается при каждом открытии
-- и едет вместе с работой: команда, у которой дела пошли хуже, увидит
-- выросший «p85» и не заметит ухудшения, потому что мерило съехало вместе
-- с измеряемым. Обещание неподвижно между пересмотрами, и ровно поэтому
-- по нему видно, что стало хуже.
--
-- Отсюда же и то, что оно пустое по умолчанию: доска без истории обещать
-- не может, и подставлять ей выдуманный срок значит начинать с неправды.

alter table boards
    add column sle_days int
        constraint boards_sle_days_positive check (sle_days is null or sle_days > 0),
    add column sle_probability int not null default 85
        constraint boards_sle_probability_sane
        check (sle_probability between 50 and 99);

comment on column boards.sle_days is
    'За сколько дней работа проходит доску с заявленной вероятностью. Пусто — обещания нет.';
comment on column boards.sle_probability is
    'Вероятность в процентах: «85% за 8 дней». Меньше половины — не прогноз, а гадание; больше 99 — не бывает.';
