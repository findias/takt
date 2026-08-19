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
import { Button, IconButton } from '../../shared/ui/Button.tsx'
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
  /** Сколько карточек этой колонки скрыл отбор — счётчик, а не признак:
   *  пустая колонка называет число, иначе «Пусто» врёт. */
  hiddenByFilter: number
  /** Сколько карточек колонки показаны как части своих задач, а не
   *  своей строкой. В счёт колонки они входят: это идущая работа,
   *  и лимит одновременной работы считает её на сервере так же. */
  partsInside: number
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
  /** Может ли смотрящий менять доску: у наблюдателя действий нет
   *  вовсе, а не «есть, но откажут». */
  canEdit: boolean
  cardLabels: Record<string, string[]>
  /** cardId → родительская задача, если карточка чья-то подзадача. */
  parents: Record<string, { id: string; title: string; onThisBoard: boolean }>
  /** cardId → название его итерации. Названия, а не идентификаторы:
   *  карточка показывает, и разбирать словарь итераций ей незачем. */
  iterations: Record<string, string>
  /** cardId → её подзадачи, если работа разбита. */
  children: Record<string, Related[]>
  /** Две стороны связи «блокирует»: кого карточка держит и кто держит
   *  её. Считаются на доску — карточка получает готовое. */
  holds: Record<string, Related[]>
  waitsFor: Record<string, Related[]>
  onLabel: (cardId: string, labelId: string, on: boolean) => void
  /** Выделенные карточки — одним набором на доску: массовое действие
   *  спрашивают у доски, а не у колонки. */
  selected: Set<string>
  onSelect: (cardId: string, on: boolean, extend?: boolean) => void
  onPrioritise: (cardId: string, priority: Priority) => void
  onBlock: (cardId: string, reason: string) => void
  onUnblock: (cardId: string) => void
  /** Отметить работу сделанной, не двигая её по колонкам. */
  onMarkDone: (cardId: string, done: boolean) => void
  onSubtask: (parentCardId: string, title: string) => void
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
  // Свёрнутая колонка не показывает ничего, кроме счётчика, — а форма
  // заведения оставалась открытой и торчала из свёрнутой полосы: она
  // ведь закрывается только человеком. Сворачивание её и закрывает.
  useEffect(() => {
    if (props.collapsed) setAdding(false)
  }, [props.collapsed])
  // Только что заведённую карточку надо показать: она встаёт в конец
  // колонки, а конец колонки бывает за краем экрана — форма закрывалась,
  // и на экране не менялось ничего. Ждать приходится следующего показа:
  // карточка появляется, когда доска пересобралась.
  const added = useRef(false)
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
    if (!added.current) return
    added.current = false
    const list = dropRef.current
    const last = list?.querySelector<HTMLElement>('.card:last-of-type')
    // `block: nearest` — прокрутить ровно настолько, чтобы карточку
    // стало видно: доска не должна прыгать, когда прыгать незачем.
    last?.scrollIntoView({ block: 'nearest' })
  }, [props.cardIds])

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
            count={props.cardIds.length + props.partsInside}
            limit={props.column.wipLimit}
            hard={props.column.wipLimitHard}
            onSetLimit={props.onSetLimit}
          />
        </div>
        <div className="row row--tight">
          {/* Имя называет колонку: «Разметка» в шапке каждой колонки
              звучит одинаково, и с диктора их три неотличимых. */}
          {!props.collapsed && props.canEdit && (
            <button
              className="link column-settings-toggle"
              aria-expanded={settings}
              aria-label={`Разметка колонки «${props.name}»`}
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
        <ColumnSettings
          column={props.column}
          onUpdate={props.onUpdateColumn}
          onSetLimit={props.onSetLimit}
        />
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
            canEdit={props.canEdit}
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
            holds={props.holds[cardId] ?? NO_SUBTASKS}
            waitsFor={props.waitsFor[cardId] ?? NO_SUBTASKS}
            onLabel={props.onLabel}
            selected={props.selected.has(cardId)}
            onSelect={props.onSelect}
            onPrioritise={props.onPrioritise}
            onBlock={props.onBlock}
            onUnblock={props.onUnblock}
            onMarkDone={props.onMarkDone}
            onSubtask={props.onSubtask}
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

        {/* Пустая колонка отвечает, почему она пустая. «Пусто.
            Перетащите карточку сюда» при скрытых отбором — враньё:
            человек идёт искать поломку, которой нет, а карточки лежат
            на месте. Кнопка названа та самая, что вернёт их. */}
        {/* Колонка, в которой остались одни части, не пустая: работа
            в ней идёт, просто показана внутри своих задач. «Пусто»
            здесь было бы враньём. */}
        {props.cardIds.length === 0 && props.hiddenByFilter === 0 && props.partsInside > 0 && (
          <p className="empty">
            Здесь только части задач — они показаны внутри самих задач. Всего в колонке:{' '}
            {props.partsInside}.
          </p>
        )}
        {props.cardIds.length === 0 &&
          props.partsInside === 0 &&
          (props.hiddenByFilter > 0 ? (
            <p className="empty">
              Под отбор ничего не подошло: скрыто {props.hiddenByFilter}. Вернёт кнопка «Показать
              все».
            </p>
          ) : (
            <p className="empty">Пусто. Перетащите карточку сюда или перенесите кнопкой на ней.</p>
          ))}
      </div>

      {!props.canEdit ? null : adding ? (
        <NewCardForm
          column={props.name}
          onCancel={() => setAdding(false)}
          onCreate={(title) => {
            props.onCreateCard(title)
            added.current = true
            // Форма остаётся открытой: карточки заводят подряд, а закрытая
            // форма после каждой означает поиск кнопки на каждую вторую.
            // Закрывают её Escape и «Отмена» — то есть человек, а не мы.
          }}
        />
      ) : (
        !props.collapsed && (
          <button
            className="add"
            aria-label={`Завести карточку в «${props.name}»`}
            onClick={() => setAdding(true)}
          >
            <PlusIcon />
            Завести карточку
          </button>
        )
      )}
    </section>
  )
}

/**
 * Заведение карточки.
 *
 * Форма не закрывается после Enter, а очищается и остаётся с фокусом:
 * работа приходит списком, и карточки заводят подряд. Закрытая после
 * каждой форма означала поиск кнопки на каждую вторую карточку — это
 * и было первым, обо что спотыкался проход по интерфейсу.
 */
function NewCardForm({
  column,
  onCreate,
  onCancel,
}: {
  /** Имя колонки — кнопке: «Завести» отдельно от окружения ничего
   *  не говорит, а диктор читает кнопку именно отдельно. */
  column: string
  onCreate: (title: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState('')
  const field = useRef<HTMLTextAreaElement>(null)

  const submit = () => {
    const title = value.trim()
    if (!title) return
    onCreate(title)
    setValue('')
    // Фокус возвращается руками: доска перерисовывается новой карточкой,
    // и без этого он уезжает на `body` — то есть обход с клавиатуры
    // начинается заново со всей страницы.
    field.current?.focus()
  }

  return (
    <form
      className="new-card"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <textarea
        ref={field}
        autoFocus
        value={value}
        placeholder="Что нужно сделать?"
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onCancel()
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            submit()
          }
        }}
      />
      <div className="row">
        <button
          type="submit"
          aria-label={`Завести карточку в «${column}»`}
          disabled={!value.trim()}
        >
          Завести
        </button>
        {/* Отмена — тихая: цвет ссылки рядом с главным действием
            перетягивает взгляд на себя, и первым читается «Отмена». */}
        <Button kind="quiet" type="button" onClick={onCancel}>
          Отмена
        </Button>
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
  onSetLimit,
}: {
  column: Column
  onUpdate: (patch: ColumnPatch) => void
  /** Лимит правится и здесь, а не только счётчиком в шапке: «сначала
   *  задайте лимит» стояло там, где задать его было нечем, и человек
   *  шёл искать поле. */
  onSetLimit: (limit: number | null) => void
}) {
  const [policy, setPolicy] = useState(column.policy)
  useEffect(() => setPolicy(column.policy), [column.policy])
  const [limit, setLimit] = useState(column.wipLimit === null ? '' : String(column.wipLimit))
  useEffect(
    () => setLimit(column.wipLimit === null ? '' : String(column.wipLimit)),
    [column.wipLimit],
  )

  const commitLimit = () => {
    const parsed = parseLimitDraft(limit, column.wipLimit)
    if (parsed.change) onSetLimit(parsed.limit)
  }

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
      {/* Лимит стоит здесь же, над жёсткостью: раньше про него было
          сказано «сначала задайте лимит», а задать его отсюда было
          нечем — правился он нажатием по счётчику в шапке колонки,
          и про это не было сказано нигде. */}
      <label className="row row--tight">
        <span className="muted small">Лимит</span>
        <input
          type="number"
          min={1}
          className="count-edit"
          value={limit}
          placeholder="без лимита"
          aria-label={`Лимит карточек в колонке «${column.name}», пусто — без лимита`}
          onChange={(e) => setLimit(e.target.value)}
          onBlur={commitLimit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') commitLimit()
            if (e.key === 'Escape') setLimit(column.wipLimit === null ? '' : String(column.wipLimit))
          }}
        />
      </label>
      <label className="row row--tight">
        <input
          type="checkbox"
          checked={column.wipLimitHard}
          disabled={column.wipLimit === null}
          onChange={(e) => onUpdate({ wipLimitHard: e.target.checked })}
        />
        <span className="small">
          Жёсткий лимит{column.wipLimit === null ? ' — сначала задайте лимит выше' : ''}
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
