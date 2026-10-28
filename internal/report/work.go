package report

// Время в работе: сколько карточка на самом деле простояла в колонках
// работы.
//
// Время цикла меряет от первого начала до готовности, и в него входит
// всё, что было между: карточку вернули в очередь на две недели, она
// ждала ревью в колонке-очереди. Руководитель, спрашивающий «сколько
// на самом деле делали», получал число, в котором ожидания больше, чем
// работы, — и на перенесённых данных медиану в пятьсот дней.
//
// Считается по журналу перемещений: каждое событие, поставившее
// карточку в колонку, открывает отрезок, следующее закрывает. Колонка
// работы — та, что была `in_progress` в момент перехода: вид берётся
// снимком из события, переразметка доски задним числом историю
// не переписывает. Незакрытый последний отрезок идёт до «сейчас».
// Отрезки обрезаются началом и завершением карточки: закрытая, не выходя
// из колонки работы, она иначе «работалась» бы и дальше, а время
// в работе выходило бы больше времени цикла рядом с ним.
//
// Пусто, а не ноль, когда история неполна: ноль читался бы как «работы
// не было». Неполна она, если первое видимое событие — не заведение
// карточки (её перенесли с доски, которой спрашивающий не видит) или
// если работа начата раньше заведения у нас (перенесена из другой
// системы: там она шла, а журнала оттуда нет). Не начатая карточка —
// тоже пусто: в работе она не была.
//
// Подзапрос для `left join lateral (…) w on true` при карточке `c`;
// даёт w.days — дни до сотых.
const workDaysLateral = `
		select case when c.started_at is not null
		             and s.first_type = 'created'
		             and c.started_at >= s.first_at
		            then coalesce(s.secs, 0) / 86400.0 end as days
		  from (select (array_agg(m.type order by m.at, m.id))[1] as first_type,
		               min(m.at) as first_at,
		               sum(greatest(0, extract(epoch from
		                     (least(coalesce(m.till, now()), coalesce(c.finished_at, now()))
		                      - greatest(m.at, c.started_at)))))
		                 filter (where m.kind = 'in_progress') as secs
		          from (select e.id, e.type, e.at,
		                       case when e.type in ('moved_out', 'archived') then null
		                            else coalesce(e.payload -> 'to' ->> 'kind', col.kind) end as kind,
		                       lead(e.at) over (order by e.at, e.id) as till
		                  from card_events e
		                  left join board_columns col on col.id = e.to_column
		                 where e.card_id = c.id
		                   and e.type in ('created', 'moved', 'moved_in', 'moved_out',
		                                  'archived', 'restored')) m) s`
