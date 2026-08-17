import { useEffect, useRef, useState } from 'react'
import { combine } from '@atlaskit/pragmatic-drag-and-drop/combine'
import { autoScrollForElements } from '@atlaskit/pragmatic-drag-and-drop-auto-scroll/element'
import { dropTargetForElements } from '@atlaskit/pragmatic-drag-and-drop/element/adapter'
import { flowMarks, limitLabel, parseLimitDraft } from '../../entities/board/model.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type {
  Column,
  ColumnKind,
  EstimateUnit,
  Label,
  Priority,
} from '../../shared/api/index.ts'
import { IconButton } from '../../shared/ui/Button.tsx'
import { EditableText } from '../../shared/ui/EditableText.tsx'
import { ChevronLeftIcon, ChevronRightIcon, PlusIcon } from '../../shared/ui/icons.tsx'
import { NO_SUBTASKS } from '../../entities/card/model.ts'
import type { Related } from '../../entities/card/model.ts'
import { CardView } from './CardView.tsx'
import type { ColumnPatch } from './useBoard.ts'

/** Общий пустой список меток: `?? []` создаёт новый массив на каждую
 *  отрисовку и в одиночку обесценивает мемоизацию карточки. */
const NO_LABELS: string[] = []
/** По той же причине — общий пустой список исполнителей. */
const NO_ASSIGNEES: string[] = []

/**
 * Колонка доски вместе со своими карточками.
 *
 * Здесь же живёт всё, что относится к колонке и ни к чему больше:
 * форма новой карточки, счётчик с лимитом и разметка колонки для
 * потока.
 */
type ColumnProps = {
  columnId: string
  name: string
  column: Column
  /** Свёрнутая колонка занимает полосу шириной с заголовок. */
  collapsed: boolean
  onToggleCollapsed: () => void
  cardIds: string[]
  cards: BaseState['cards']
  unit: EstimateUnit
  sleDays: number | null
  people: Record<string, string>
  /** cardId → исполнители в порядке назначения. */
  cardAssignees: Record<string, string[]>
  onAssign: (cardId: string, userId: string, on: boolean) => void
  /** Карточка, которую только что перенесли: вспыхивает на новом месте. */
  justMoved: string | null
  labels: Label[]
  cardLabels: Record<string, string[]>
  /** cardId → родительская задача, если карточка чья-то подзадача. */
  parents: Record<string, { id: string; title: string; onThisBoard: boolean }>
  /** cardId → название его итерации. Названия, а не идентификаторы:
   *  карточка показывает, и разбирать словарь итераций ей незачем. */
  iterations: Record<string, string>
  /** cardId → её подзадачи, если работа разбита. */
  children: Record<string, Related[]>
  onLabel: (cardId: string, labelId: string, on: boolean) => void
  /** Выделенные карточки — одним набором на доску: массовое действие
   *  спрашивают у доски, а не у колонки. */
  selected: Set<string>
  onSelect: (cardId: string, on: boolean, extend?: boolean) => void
  onPrioritise: (cardId: string, priority: Priority) => void
  onBlock: (cardId: string, reason: string) => void
  onUnblock: (cardId: string) => void
  columns: Column[]
  onMoveToColumn: (cardId: string, columnId: string) => void
  onOpenCard: (cardId: string) => void
  onMoveByKeyboard: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onNavigate: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onCreateCard: (title: string) => void
  onRenameColumn: (name: string) => void
  onSetLimit: (limit: number | null) => void
  onUpdateColumn: (patch: ColumnPatch) => void
  onRenameCard: (cardId: string, title: string) => void
  onArchiveCard: (cardId: string) => void
  /** Пусто — удалять насовсем нельзя: так у всех, кроме владельца.
   *  Название передаётся вместе с идентификатором: диалог один на доску,
   *  и спрашивают из него в том числе про архивные карточки. */
  onDeleteCard?: (cardId: string, title: string) => void
}

/**
 * Сколько карточек рисуется сразу.
 *
 * Замер на доске в пятьсот карточек: полный список — 892 мс до первой
 * отрисовки, сотня — 403. Дальше список дорисовывается по мере
 * прокрутки, и разницы человек не замечает: в окно помещается около
 * двадцати карточек.
 *
 * Настоящей виртуализации (окно, ползающее по списку, с оценкой высот)
 * здесь нет намеренно: она ломает перетаскивание — исчезнувшая из
 * разметки цель перестаёт быть целью, — и требует знать высоту
 * карточки, которая у нас разная. Дорисовка вниз даёт ту же выгоду
 * на первой отрисовке и ничего не ломает.
 */
const CHUNK = 100

export function ColumnView(props: ColumnProps) {
  const dropRef = useRef<HTMLDivElement>(null)
  const tailRef = useRef<HTMLDivElement>(null)
  const [over, setOver] = useState(false)
  const [adding, setAdding] = useState(false)
  const [settings, setSettings] = useState(false)
  const [limit, setLimit] = useState(CHUNK)

  const total = props.cardIds.length
  const shownCards = total > limit ? props.cardIds.slice(0, limit) : props.cardIds

  // Дорисовка по прокрутке. Запас в четыреста пикселей: карточки
  // должны появляться до того, как человек доскроллит до пустоты,
  // иначе список выглядит обрывающимся.
  useEffect(() => {
    const element = tailRef.current
    if (!element || total <= limit) return
    const watcher = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setLimit((l) => l + CHUNK)
      },
      { rootMargin: '400px' },
    )
    watcher.observe(element)
    return () => watcher.disconnect()
  }, [limit, total])

  useEffect(() => {
    const element = dropRef.current
    if (!element) return
    return combine(
      // Автопрокрутка списка: без неё карточку нельзя утащить ниже
      // видимой части колонки — приходится бросать, прокручивать
      // и тащить снова.
      autoScrollForElements({ element }),
      dropTargetForElements({
        element,
        canDrop: ({ source }) => source.data.kind === 'card',
        getData: () => ({ kind: 'column', columnId: props.columnId }),
        onDragEnter: () => setOver(true),
        onDragLeave: () => setOver(false),
        onDrop: () => setOver(false),
      }),
    )
  }, [props.columnId])

  return (
    <section
      className={`column${over ? ' column--over' : ''}${props.collapsed ? ' column--collapsed' : ''}`}
      aria-label={props.name}
    >
      <header className="column-header">
        {/* Название и счётчик — одна мысль «в этой колонке столько
            работы», поэтому стоят рядом. Раньше счётчик висел посередине
            заголовка и читался как случайное число. */}
        <div className="row row--tight">
          <EditableText value={props.name} onSave={props.onRenameColumn} className="column-title" />
          <ColumnCount
            count={props.cardIds.length}
            limit={props.column.wipLimit}
            hard={props.column.wipLimitHard}
            onSetLimit={props.onSetLimit}
          />
        </div>
        <div className="row row--tight">
          {!props.collapsed && (
            <button
              className="link column-settings-toggle"
              aria-expanded={settings}
              onClick={() => setSettings((v) => !v)}
            >
              Разметка
            </button>
          )}
          {/* Сворачивание — личное предпочтение смотрящего, поэтому оно
              не в адресе и не на сервере: «Готово» мешает одному
              и нужна другому. */}
          <IconButton
            label={props.collapsed ? `Развернуть «${props.name}»` : `Свернуть «${props.name}»`}
            onClick={props.onToggleCollapsed}
          >
            {props.collapsed ? <ChevronRightIcon /> : <ChevronLeftIcon />}
          </IconButton>
        </div>
      </header>

      {!props.collapsed && flowMarks(props.column).length > 0 && (
        <div className="card-marks">
          {flowMarks(props.column).map((m) => (
            <span key={m} className="mark">
              {m}
            </span>
          ))}
        </div>
      )}
      {!props.collapsed && props.column.policy && !settings && (
        <p className="muted small column-policy">{props.column.policy}</p>
      )}
      {!props.collapsed && settings && (
        <ColumnSettings column={props.column} onUpdate={props.onUpdateColumn} />
      )}

      <div
        className="cards"
        ref={dropRef}
        hidden={props.collapsed}
        // Клавиатура доходит до конца окна раньше прокрутки: стрелка
        // вниз с последней отрисованной карточки увела бы фокус
        // в никуда — карточки за окном в разметке нет.
        onFocus={(e) => {
          const node = (e.target as HTMLElement).closest<HTMLElement>('[data-card]')
          if (!node) return
          const at = shownCards.indexOf(node.dataset.card ?? '')
          if (at >= 0 && at >= shownCards.length - 5 && total > limit) setLimit((l) => l + CHUNK)
        }}
      >
        {shownCards.map((cardId) => (
          <CardView
            key={cardId}
            cardId={cardId}
            columnId={props.columnId}
            card={props.cards[cardId]}
            unit={props.unit}
            sleDays={props.sleDays}
            people={props.people}
            assignees={props.cardAssignees[cardId] ?? NO_ASSIGNEES}
            onAssign={props.onAssign}
            flash={props.justMoved === cardId}
            labels={props.labels}
            cardLabels={props.cardLabels[cardId] ?? NO_LABELS}
            parent={props.parents[cardId]}
            iteration={props.iterations[cardId]}
            subtasks={props.children[cardId] ?? NO_SUBTASKS}
            onLabel={props.onLabel}
            selected={props.selected.has(cardId)}
            onSelect={props.onSelect}
            onPrioritise={props.onPrioritise}
            onBlock={props.onBlock}
            onUnblock={props.onUnblock}
            columns={props.columns}
            onMoveToColumn={props.onMoveToColumn}
            onOpen={props.onOpenCard}
            onMoveByKeyboard={props.onMoveByKeyboard}
            onNavigate={props.onNavigate}
            onRename={props.onRenameCard}
            onArchive={props.onArchiveCard}
            onDelete={props.onDeleteCard}
          />
        ))}
        {/* Хвост: до него доходит либо прокрутка, либо клавиатура —
            и то и другое означает, что пора дорисовывать. */}
        {total > limit && (
          <div
            ref={tailRef}
            className="cards-tail muted small"
            // Число не для красоты: без него список молча обрывается,
            // а «дальше ещё триста» объясняет и прокрутку, и то, что
            // поиск ищет по всем, а не по показанным.
            onFocus={() => setLimit((l) => l + CHUNK)}
            tabIndex={0}
          >
            Ещё {total - limit}: прокрутите, чтобы показать
          </div>
        )}

        {props.cardIds.length === 0 && (
          <p className="empty">Пусто. Перетащите карточку сюда или перенесите кнопкой на ней.</p>
        )}
      </div>

      {adding ? (
        <NewCardForm
          onCancel={() => setAdding(false)}
          onCreate={(title) => {
            props.onCreateCard(title)
            setAdding(false)
          }}
        />
      ) : (
        !props.collapsed && (
          <button className="add" onClick={() => setAdding(true)}>
            <PlusIcon />
            Добавить карточку
          </button>
        )
      )}
    </section>
  )
}

function NewCardForm({
  onCreate,
  onCancel,
}: {
  onCreate: (title: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState('')
  return (
    <form
      className="new-card"
      onSubmit={(e) => {
        e.preventDefault()
        if (value.trim()) onCreate(value.trim())
      }}
    >
      <textarea
        autoFocus
        value={value}
        placeholder="Что нужно сделать?"
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onCancel()
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            if (value.trim()) onCreate(value.trim())
          }
        }}
      />
      <div className="row">
        <button type="submit" disabled={!value.trim()}>
          Добавить
        </button>
        <button type="button" className="link" onClick={onCancel}>
          Отмена
        </button>
      </div>
    </form>
  )
}

/** Счётчик карточек, он же редактор лимита колонки.
 *
 *  Превышение подсвечивается, но работать не мешает: лимит нужен, чтобы
 *  команда видела перегрузку. Запрещает превышение только жёсткий лимит,
 *  и отказывает в этом сервер — здесь запрета нет намеренно.
 *  Пустое поле снимает лимит. */
function ColumnCount({
  count,
  limit,
  hard,
  onSetLimit,
}: {
  count: number
  limit: number | null
  hard: boolean
  onSetLimit: (limit: number | null) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(limit === null ? '' : String(limit))
  useEffect(() => setDraft(limit === null ? '' : String(limit)), [limit])

  const commit = () => {
    setEditing(false)
    const parsed = parseLimitDraft(draft, limit)
    if (parsed.change) onSetLimit(parsed.limit)
  }

  if (editing)
    return (
      <input
        autoFocus
        type="number"
        min={1}
        className="count-edit"
        aria-label="Лимит карточек в колонке, пусто — без лимита"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit()
          if (e.key === 'Escape') {
            setDraft(limit === null ? '' : String(limit))
            setEditing(false)
          }
        }}
      />
    )

  const { label, over } = limitLabel(count, limit)
  return (
    <button
      className={`count${over ? ' count--over' : ''}`}
      onClick={() => setEditing(true)}
      title={
        limit === null
          ? 'Карточек в колонке. Нажмите, чтобы задать лимит'
          : `${count} из ${limit}${hard ? ', жёсткий лимит' : ''}. Нажмите, чтобы изменить`
      }
    >
      {label}
    </button>
  )
}

/**
 * Разметка колонки: чем она является для потока.
 *
 * Вид колонки и точки старта и финиша — не одно и то же. Очередей и стадий
 * работы бывает много, а границами потока объявляются конкретные: время
 * цикла считается между ними, и переставить их задним числом нельзя так,
 * чтобы прошлые события пересчитались правильно.
 */
function ColumnSettings({
  column,
  onUpdate,
}: {
  column: Column
  onUpdate: (patch: ColumnPatch) => void
}) {
  const [policy, setPolicy] = useState(column.policy)
  useEffect(() => setPolicy(column.policy), [column.policy])

  return (
    <div className="column-settings stack">
      <label className="row row--tight">
        <span className="muted small">Вид</span>
        <select
          value={column.kind}
          aria-label={`Вид колонки «${column.name}»`}
          onChange={(e) => onUpdate({ kind: e.target.value as ColumnKind })}
        >
          <option value="queue">Очередь</option>
          <option value="in_progress">Работа</option>
          <option value="done">Готово</option>
        </select>
      </label>

      <label className="row row--tight">
        <input
          type="checkbox"
          checked={column.isStartedPoint}
          onChange={(e) => onUpdate({ isStartedPoint: e.target.checked })}
        />
        <span className="small">Здесь работа начинается</span>
      </label>
      <label className="row row--tight">
        <input
          type="checkbox"
          checked={column.isFinishedPoint}
          onChange={(e) => onUpdate({ isFinishedPoint: e.target.checked })}
        />
        <span className="small">Здесь работа заканчивается</span>
      </label>
      <label className="row row--tight">
        <input
          type="checkbox"
          checked={column.wipLimitHard}
          disabled={column.wipLimit === null}
          onChange={(e) => onUpdate({ wipLimitHard: e.target.checked })}
        />
        <span className="small">
          Жёсткий лимит{column.wipLimit === null ? ' (сначала задайте лимит)' : ''}
        </span>
      </label>

      <textarea
        className="description"
        rows={2}
        value={policy}
        placeholder="Правило входа: что должно быть сделано, чтобы карточка попала сюда"
        aria-label={`Правило входа в колонку «${column.name}»`}
        onChange={(e) => setPolicy(e.target.value)}
        onBlur={() => policy !== column.policy && onUpdate({ policy })}
      />
    </div>
  )
}
