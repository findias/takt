-- Предикат политики должен попадать в индексное условие, а не в фильтр.
--
-- Предыдущая миграция убрала вызов функции с каждой строки, и это дало
-- тысячекратное ускорение. Но она остановилась на полпути: форма
-- `board_id in (select unnest(…))` превращается в hashed SubPlan, а
-- хешированный подплан — это фильтр. Строка сначала читается с диска,
-- потом отбрасывается. Таблица всё равно прочитывается целиком.
--
-- `board_id = any (array(select …))` даёт InitPlan, то есть обычный
-- массив-константу, и планировщику становится можно завести по нему
-- индексное условие. Замер на 200 досках по 100 карточек, где участнику
-- видна ровно одна доска:
--
--   in (select unnest(…))          Seq Scan + Filter        20 000 строк прочитано,
--                                                            19 925 отброшено, 5.3 мс
--   = any (array(select …))        Bitmap Index Scan        100 строк прочитано, 1.9 мс
--                                  Index Cond: board_id = ANY ($1)
--
-- Разница во времени на этом объёме невелика; разница в том, что первая
-- форма линейна по размеру таблицы, а вторая — по размеру видимого. На
-- миллионе карточек это уже не про миллисекунды.
--
-- Важная тонкость, из-за которой первая попытка замера ничего не показала:
-- политики select и all объединяются через OR, и достаточно одной ветви
-- в форме подплана, чтобы весь OR перестал быть индексируемым. Поэтому
-- переводить надо все ветви разом, иначе работа впустую.

-- --- 1. Доска ---
--
-- Подзапросы к team_members и board_members тоже становятся массивами:
-- у boards есть boards_team_idx, и по team_id можно идти индексом.

alter policy visible on boards using (
       visibility = 'org'
    or team_id = any (array(select tm.team_id from team_members tm
                             where tm.user_id = (select app_current_user())))
    or id = any (array(select bm.board_id from board_members bm
                        where bm.user_id = (select app_current_user())))
    or (visibility <> 'private' and (select app_view_all())));

alter policy writable on boards
    using (id = any (array(select unnest(app_writable_boards()))))
    with check (id = any (array(select unnest(app_writable_boards()))));

-- --- 2. Всё, что висит на доске ---

do $$
declare t text;
begin
    foreach t in array array['board_columns', 'cards', 'card_events', 'operations'] loop
        execute format(
            'alter policy visible on %I
               using (board_id = any (array(select unnest(app_visible_boards()))))', t);
    end loop;
    foreach t in array array['board_columns', 'cards'] loop
        execute format(
            'alter policy writable on %I
               using (board_id = any (array(select unnest(app_writable_boards()))))
               with check (board_id = any (array(select unnest(app_writable_boards()))))', t);
    end loop;
    foreach t in array array['card_events', 'operations'] loop
        execute format(
            'alter policy appendable on %I
               with check (board_id = any (array(select unnest(app_writable_boards()))))', t);
    end loop;
end $$;

-- --- 3. Связи и блокировки остаются как есть ---
--
-- У них предикат `from_card in (select id from cards)` — подзапрос к
-- таблице, а не к массиву. Разворачивать его в массив нельзя: это значило
-- бы вытащить в память идентификаторы всех видимых карточек организации
-- ради проверки одной связи. Здесь хешированный подплан — правильный
-- выбор, а не недоделка.
