import { useMemo } from 'react'
import { Menu } from '../../shared/ui/Menu.tsx'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { MoreIcon } from '../../shared/ui/icons.tsx'
import { ageLabel, agingLabel } from '../../entities/board/model.ts'
import {
  UNIT_SHORT,
  cardsLabel,
  dateWords,
  dueIsBurning,
  priorityLabel,
} from '../../entities/card/model.ts'
import { SORT_NAMES, comparator } from './tableSort.ts'
import type { Sort } from './tableSort.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card, Column, EstimateUnit, Label } from '../../shared/api/index.ts'

/**
 * Доска плоским списком.
 *
 * Второй вид на те же данные, а не второй экран: колонки, фильтры,
 * группировка и права остаются теми же, меняется только раскладка.
 * Доска отвечает на вопрос «как идёт работа», таблица — на вопросы
 * «где самое старое», «сколько на ком висит» и «что мы обещали
 * и на когда», а на них колонками не отвечают: чтобы сравнить возраст
 * или срок двух карточек в разных колонках, на доске их приходится
 * искать глазами.
 *
 * Отсюда и правило столбцов: в таблице стоит то, что сравнивают между
 * строками, и стоит у каждой строки — включая пустые значения. Пустое
 * место сравнивать не с чем, поэтому «нет срока» пишется прочерком,
 * а не отсутствием ячейки.
 *
 * Сортировка живёт в адресе, как фильтры и группировка: отсортированный
 * вид присылают ссылкой.
 */

export function TableView({
  base,
  order,
  columns,
  unit,
  sort,
  people,
  labels,
  onOpenCard,
  onMoveToColumn,
  onAssign,
}: {
  base: BaseState
  /** Тот же порядок, что на доске: фильтр уже применён. */
  order: Record<string, string[]>
  columns: Column[]
  unit: EstimateUnit
  sort: Sort
  people: Record<string, string>
  labels: Label[]
  onOpenCard: (cardId: string) => void
  onMoveToColumn: (cardId: string, columnId: string) => void
  onAssign: (cardId: string, userId: string, on: boolean) => void
}) {
  const columnName = useMemo(
    () => Object.fromEntries(columns.map((c) => [c.id, c.name])),
    [columns],
  )
  const position = useMemo(
    () => Object.fromEntries(columns.map((c, i) => [c.id, i])),
    [columns],
  )
  const iterationName = useMemo(
    () => Object.fromEntries(base.iterations.map((i) => [i.id, i.name])),
    [base.iterations],
  )

  const rows = useMemo(() => {
    const cards = columns.flatMap((c) => (order[c.id] ?? []).map((id) => base.cards[id])).filter(Boolean)
    return [...cards].sort(comparator(sort, position))
  }, [base.cards, columns, order, position, sort])

  if (rows.length === 0) {
    return <p className="muted small table-empty">Ни одной карточки — показывать нечего.</p>
  }

  return (
    <div className="table-wrap">
      <table className="board-table">
        <caption className="sr-only">
          Карточки доски списком, {SORT_NAMES[sort]}
        </caption>
        <thead>
          <tr>
            <th scope="col">Ключ</th>
            <th scope="col">Задача</th>
            <th scope="col">Колонка</th>
            <th scope="col">Кто делает</th>
            <th scope="col">Метки</th>
            <th scope="col">Приоритет</th>
            <th scope="col">Оценка</th>
            <th scope="col">Возраст</th>
            <th scope="col">Срок</th>
            <th scope="col">Итерация</th>
            <th scope="col">
              <span className="sr-only">Действия</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((card) => {
            const assignees = base.cardAssignees[card.id] ?? []
            const own = base.cardLabels[card.id] ?? []
            const overdue = agingLabel(card, base.info.sleDays)
            return (
              <tr key={card.id}>
                <td className="mono muted small">{card.number}</td>
                <td>
                  <button className="link table-title" onClick={() => onOpenCard(card.id)}>
                    {card.title}
                  </button>
                </td>
                <td className="muted small">{columnName[card.columnId]}</td>
                <td>
                  {assignees.length === 0 ? (
                    <span className="muted small">никто</span>
                  ) : (
                    <span className="avatars">
                      {assignees.map((id) => (
                        <Avatar key={id} name={people[id] ?? 'Кто-то'} />
                      ))}
                    </span>
                  )}
                </td>
                <td>
                  {own.map((id) => {
                    const label = labels.find((l) => l.id === id)
                    return label ? (
                      <span key={id} className={`chip chip--${label.tone}`}>
                        {label.name}
                      </span>
                    ) : null
                  })}
                </td>
                {/* Приоритет назван словом у каждой строки, в отличие
                    от доски: таблицу открывают, чтобы сравнивать,
                    а сравнивать пустое место с пустым местом нельзя. */}
                <td className="muted small">{priorityLabel(card.priority)}</td>
                <td className="muted small">
                  {card.estimate === null ? '—' : `${card.estimate} ${UNIT_SHORT[unit]}`}
                </td>
                {/* Возраст показывается всем строкам, а не только
                    перешагнувшим обещание: на доске он подсказка,
                    а здесь — то, по чему сравнивают. */}
                <td className={overdue ? 'table-overdue' : 'muted small'}>{ageText(card)}</td>
                {/* Срок — датой, а не отсчётом, в отличие от доски.
                    Доску спрашивают «успеваем ли» и отвечают ей
                    «через 4 дн.»; список открывают с вопросом «что мы
                    обещали и на какое число», и на него отвечает
                    только число. Нехватку отсчёта закрывает порядок:
                    сортировка по сроку выстраивает столбец календарём.
                    Горящее — цветом, как перешагнувший возраст: без
                    него «сегодня» и «прошёл» пришлось бы вычитать
                    из даты глазами. */}
                <td className={dueIsBurning(card.dueOn) ? 'table-overdue' : 'muted small'}>
                  {card.dueOn === null ? '—' : dateWords(card.dueOn)}
                </td>
                <td className="muted small">{iterationName[base.cardIterations[card.id]] ?? '—'}</td>
                <td>
                  <Menu
                    label={`Действия карточки «${card.title}»`}
                    items={[
                      { label: 'Открыть', onSelect: () => onOpenCard(card.id) },
                      ...columns
                        .filter((c) => c.id !== card.columnId)
                        .map((c) => ({
                          label: `Перенести в «${c.name}»`,
                          onSelect: () => onMoveToColumn(card.id, c.id),
                        })),
                      ...Object.entries(people).map(([id, name]) => ({
                        label: assignees.includes(id) ? `Снять: ${name}` : `Назначить: ${name}`,
                        onSelect: () => onAssign(card.id, id, !assignees.includes(id)),
                      })),
                    ]}
                  >
                    <MoreIcon />
                  </Menu>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <p className="muted small">
        {cardsLabel(rows.length)}, {SORT_NAMES[sort]}.
      </p>
    </div>
  )
}

function ageText(card: Card): string {
  // Прочерк, а не пустая ячейка: «возраста нет» — это ответ, а пустое
  // место читается как «не посчитали».
  return ageLabel(card) ?? '—'
}

