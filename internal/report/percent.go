package report

// Процент выполнения в выгрузке — тот же, что на карточке доски
// (ROADMAP 38, entities/card/model.ts → completion): у сделанной — 100,
// что бы ни стояло руками (решение владельца 29.10: сделанная выполнена
// на сто); иначе поставленный руками — он и есть; иначе по частям —
// по листьям всего поддерева, весом, когда оценены все, иначе штуками.
// У неначатой работы без частей процента нет: ноль в каждой второй
// строке был бы шумом, а пустая ячейка честно говорит «не мерили».
//
// Часть сделана, если прошла финиш или отмечена руками; несделанная
// входит своим ручным процентом — так же, как cardShare доски. Части
// в архиве не считаются, как и на доске.
//
// Подзапрос для `left join lateral (…) p on true` при карточке `c`;
// даёт p.percent — целое от 0 до 100. Предел глубины — параметр $1
// запроса (board.MaxSubtaskDepth).
const percentLateral = `
		select case when c.outcome = 'done' or c.done_at is not null then 100
		            when c.percent is not null then c.percent
		            when s.leaves > 0 then round(100 * case
		                 when s.unestimated = 0 and s.weight > 0 then s.weight_done / s.weight
		                 else s.done / s.leaves end)::int end as percent
		  from (with recursive tree(card, depth) as (
		          select l.to_card, 1 from card_links l
		           where l.from_card = c.id and l.kind = 'subtask'
		          union all
		          select l.to_card, t.depth + 1 from tree t
		            join card_links l on l.from_card = t.card and l.kind = 'subtask'
		           where t.depth < $1::int
		        ), parts as (
		          select distinct k.id, k.estimate::float8 as estimate,
		                 case when k.outcome = 'done' or k.done_at is not null then 1.0
		                      else coalesce(k.percent, 0) / 100.0 end as share
		            from tree t
		            join cards k on k.id = t.card and k.archived_at is null
		           where not exists (select 1 from card_links g
		                               join cards gk on gk.id = g.to_card and gk.archived_at is null
		                              where g.from_card = k.id and g.kind = 'subtask')
		        )
		        select count(*)::float8 as leaves,
		               coalesce(sum(share), 0)::float8 as done,
		               count(*) filter (where estimate is null) as unestimated,
		               coalesce(sum(estimate), 0)::float8 as weight,
		               coalesce(sum(estimate * share), 0)::float8 as weight_done
		          from parts) s`
