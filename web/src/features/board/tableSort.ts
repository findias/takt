// Порядок строк в таблице и его запись в адресе.
//
// Отдельно от `TableView.tsx` по той же причине, что `filters.ts`
// и `grouping.ts` лежат отдельно от доски: это правило, а не разметка,
// и проверять его надо без DOM и React. Запускатель моделей их
// и не поднимает.

import { ageDays } from '../../entities/board/model.ts'
import { priorityRank } from '../../entities/card/model.ts'
import type { Card } from '../../shared/api/index.ts'

export type Sort = 'age' | 'column' | 'due' | 'estimate' | 'priority'

export const SORT_NAMES: Record<Sort, string> = {
  age: 'по возрасту',
  column: 'по колонке',
  due: 'по сроку',
  estimate: 'по оценке',
  priority: 'по приоритету',
}

/**
 * Куда растёт столбец, по которому выстроены строки.
 *
 * Живёт здесь, а не в разметке, потому что это свойство порядка,
 * а не заголовка: три сортировки из пяти идут по убыванию — самое
 * старое, самое крупное и самое срочное сверху, — и заголовок обязан
 * говорить об этом то же самое, что делает `comparator`. Пока
 * направление было записано в разметке одним словом на все случаи,
 * диктор в трёх случаях из пяти читал обратное тому, что видел
 * зрячий: «по возрастанию» над столбцом, который убывает.
 */
export const SORT_DIRECTION: Record<Sort, 'ascending' | 'descending'> = {
  age: 'descending',
  column: 'ascending',
  due: 'ascending',
  estimate: 'descending',
  priority: 'descending',
}

/**
 * Порядок строк.
 *
 * Вынесен из отрисовки и проверяется отдельно: у трёх сортировок из
 * пяти правило про пустое значение, и оно не выводится из типа —
 * «нет оценки» и «нет срока» уезжают вниз, а не считаются нулём
 * и не встают первыми.
 *
 * `position` — порядковый номер колонки на доске: сортировать колонки
 * по имени значило бы поставить «Готово» перед «В работе».
 */
export function comparator(
  sort: Sort,
  position: Record<string, number>,
): (a: Card, b: Card) => number {
  // Сортируется ровно то число, которое стоит в колонке «Возраст».
  // Сортировать по дате старта было бы почти то же самое и всё-таки
  // не то: у завершённых возраст перестал расти, и они уезжали бы
  // наверх по одному только тому, что начаты давно.
  const byAge = (card: Card) => -(ageDays(card) ?? -1)
  return (a, b) => {
    switch (sort) {
      case 'column':
        // Внутри колонки — по возрасту: иначе строки одной колонки
        // выстраиваются в случайном порядке, и глазу не за что взяться.
        return (position[a.columnId] ?? 0) - (position[b.columnId] ?? 0) || byAge(a) - byAge(b)
      case 'priority':
        // Внутри уровня — по возрасту: иначе строки одного уровня
        // выстраиваются в случайном порядке, и глазу не за что взяться.
        return priorityRank(a.priority) - priorityRank(b.priority) || byAge(a) - byAge(b)
      case 'due':
        // Без даты — вниз, и тем же доводом, что у неоценённых:
        // «пусто» здесь не самый ранний срок, а отсутствие
        // обязательства. Сравниваются ISO-строки: у них порядок
        // совпадает с порядком дат, и разбирать их незачем.
        if (a.dueOn === null) return b.dueOn === null ? byAge(a) - byAge(b) : 1
        if (b.dueOn === null) return -1
        return a.dueOn.localeCompare(b.dueOn) || byAge(a) - byAge(b)
      case 'estimate':
        // Неоценённые вниз: «пусто» — не самое маленькое значение,
        // а отсутствие ответа, и наверху ему делать нечего.
        return (b.estimate ?? -1) - (a.estimate ?? -1)
      default:
        // Самое старое сверху: ради этого вопроса таблицу и открывают.
        return byAge(a) - byAge(b)
    }
  }
}

export function parseSort(query: URLSearchParams): Sort {
  const raw = query.get('sort')
  return raw === 'column' || raw === 'due' || raw === 'estimate' || raw === 'priority'
    ? raw
    : 'age'
}

export function sortToQuery(sort: Sort, base?: URLSearchParams): URLSearchParams {
  const query = new URLSearchParams(base)
  if (sort === 'age') query.delete('sort')
  else query.set('sort', sort)
  return query
}
