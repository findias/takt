-- Отпечатка нынешнего решения мало: потерянный ответ могут повторить
-- после другого решения. Идентификатор команды остаётся в истории,
-- поэтому поздний повтор ничего не пишет. Новое намерение — новый UUID.
-- Старый бинарник не знает колонки и оставляет её пустой.
alter table idea_decisions add column request_id uuid;

-- Область команды — человек и идея внутри организации. Индекс
-- страхует от двойной записи, даже если появится ещё один путь решения.
create unique index idea_decisions_request_id_idx
    on idea_decisions (org_id, author_id, idea_id, request_id)
    where request_id is not null;
