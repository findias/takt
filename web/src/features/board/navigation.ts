/**
 * Ходьба по доске стрелками.
 *
 * Сейчас единственный способ добраться до карточки с клавиатуры — Tab,
 * а он идёт по всем кнопкам подряд: до третьей карточки во второй
 * колонке — два десятка нажатий. Стрелки превращают доску в сетку,
 * какой она и выглядит.
 *
 * Чистая функция: соседа считает по спискам, ничего не зная про DOM.
 * Так её проверяют без браузера, и так же её можно позвать из любого
 * другого места — например, из будущей командной палитры.
 *
 * Правило выбора соседа при переходе вбок: та же позиция сверху, а если
 * в соседней колонке столько карточек нет — последняя. Прыгать
 * в начало колонки неверно: человек ведёт взгляд по строке и ожидает
 * оказаться напротив, а не вверху.
 */
export type Direction = 'left' | 'right' | 'up' | 'down'

export function nextCard(
  columnIds: string[],
  order: Record<string, string[]>,
  current: string | null,
  direction: Direction,
): string | null {
  // Ничего не выделено — начинаем с первой карточки первой непустой
  // колонки: любое движение должно куда-то приводить.
  if (!current) {
    for (const columnId of columnIds) {
      const first = (order[columnId] ?? [])[0]
      if (first) return first
    }
    return null
  }

  const columnIndex = columnIds.findIndex((id) => (order[id] ?? []).includes(current))
  if (columnIndex < 0) return null
  const column = order[columnIds[columnIndex]] ?? []
  const rowIndex = column.indexOf(current)

  if (direction === 'up') return column[rowIndex - 1] ?? current
  if (direction === 'down') return column[rowIndex + 1] ?? current

  const step = direction === 'left' ? -1 : 1
  // Пустые колонки пропускаем: остановка в пустоте выглядит как потеря
  // выделения, а не как перемещение.
  for (let i = columnIndex + step; i >= 0 && i < columnIds.length; i += step) {
    const neighbour = order[columnIds[i]] ?? []
    if (neighbour.length === 0) continue
    return neighbour[Math.min(rowIndex, neighbour.length - 1)]
  }
  return current
}
