import { useMemo } from 'react'
import { Menu } from '../../shared/ui/Menu.tsx'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { MoreIcon } from '../../shared/ui/icons.tsx'
import { agingLabel } from '../../entities/board/model.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card, Column, EstimateUnit, Label } from '../../shared/api/index.ts'

/**
 * Доска плоским списком.
 *
 * Второй вид на те же данные, а не второй экран: колонки, фильтры,
 * группировка и права остаются теми же, меняется только раскладка.
 * Доска отвечает на вопрос «как идёт работа», таблица — на вопросы
 * «где самое старое» и «сколько на ком висит», а на них колонками
 * не отвечают: чтобы сравнить возраст двух карточек в разных колонках,
 * на доске их приходится искать глазами.
 *
 * Сортировка живёт в адресе, как фильтры и группировка: отсортированный
 * вид присылают ссылкой.
 */

export type Sort = 'age' | 'column' | 'estimate'

export const SORT_NAMES: Record<Sort, string> = {
  age: 'по возрасту',
  column: 'по колонке',
  estimate: 'по оценке',
}

export function parseSort(query: URLSearchParams): Sort {
  const raw = query.get('sort')
  return raw === 'column' || raw === 'estimate' ? raw : 'age'
}

export function sortToQuery(sort: Sort, base?: URLSearchParams): URLSearchParams {
  const query = new URLSearchParams(base)
  if (sort === 'age') query.delete('sort')
  else query.set('sort', sort)
  return query
}

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

  const rows = useMemo(() => {
    const cards = columns.flatMap((c) => (order[c.id] ?? []).map((id) => base.cards[id])).filter(Boolean)
    // Сортируется ровно то число, которое стоит в колонке «Возраст».
    // Сортировать по дате старта было бы почти то же самое и всё-таки
    // не то: у завершённых возраст перестал расти, и они уезжали бы
    // наверх по одному только тому, что начаты давно.
    const byAge = (card: Card) => -(ageDays(card) ?? -1)
    return [...cards].sort((a, b) => {
      switch (sort) {
        case 'column':
          // Внутри колонки — по возрасту: иначе строки одной колонки
          // выстраиваются в случайном порядке, и глазу не за что взяться.
          return (position[a.columnId] ?? 0) - (position[b.columnId] ?? 0) || byAge(a) - byAge(b)
        case 'estimate':
          // Неоценённые вниз: «пусто» — не самое маленькое значение,
          // а отсутствие ответа, и наверху ему делать нечего.
          return (b.estimate ?? -1) - (a.estimate ?? -1)
        default:
          // Самое старое сверху: ради этого вопроса таблицу и открывают.
          return byAge(a) - byAge(b)
      }
    })
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
            <th scope="col">Оценка</th>
            <th scope="col">Возраст</th>
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
                <td className="muted small">
                  {card.estimate === null ? '—' : `${card.estimate} ${UNIT_SHORT[unit]}`}
                </td>
                {/* Возраст показывается всем строкам, а не только
                    перешагнувшим обещание: на доске он подсказка,
                    а здесь — то, по чему сравнивают. */}
                <td className={overdue ? 'table-overdue' : 'muted small'}>{ageText(card)}</td>
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
        {rows.length} {plural(rows.length, 'карточка', 'карточки', 'карточек')}, {SORT_NAMES[sort]}.
      </p>
    </div>
  )
}

const UNIT_SHORT: Record<EstimateUnit, string> = {
  points: 'очк.',
  hours: 'ч',
  days: 'дн.',
}

/** Возраст считается от начала работы. Неначатая карточка возраста
 *  не имеет: она не стареет, она ждёт, и это разные вещи. */
function ageDays(card: Card): number | null {
  if (!card.startedAt) return null
  const end = card.finishedAt ? Date.parse(card.finishedAt) : Date.now()
  return (end - Date.parse(card.startedAt)) / 86_400_000
}

function ageText(card: Card): string {
  const days = ageDays(card)
  if (days === null) return '—'
  return `${days < 10 ? days.toFixed(1) : Math.round(days)} дн.`
}

function plural(n: number, one: string, few: string, many: string): string {
  const mod100 = n % 100
  const mod10 = n % 10
  if (mod100 >= 11 && mod100 <= 14) return many
  if (mod10 === 1) return one
  if (mod10 >= 2 && mod10 <= 4) return few
  return many
}
