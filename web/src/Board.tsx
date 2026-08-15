import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { combine } from '@atlaskit/pragmatic-drag-and-drop/combine'
import {
  draggable,
  dropTargetForElements,
  monitorForElements,
} from '@atlaskit/pragmatic-drag-and-drop/element/adapter'
import {
  attachClosestEdge,
  extractClosestEdge,
} from '@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge'
import type { Edge } from '@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge'
import { flowIssues, flowMarks, limitLabel, parseLimitDraft } from './boardModel.ts'
import type { BaseState } from './boardModel.ts'
import { api } from './api'
import type { Card, Column, ColumnKind, EstimateUnit, Iteration } from './api'
import type { ColumnPatch } from './useBoard'
import { progressLabel } from './cardModel'
import { CardPanel } from './CardPanel'
import { Flow } from './Flow'
import { Appearance } from './Appearance'
import { useBoard } from './useBoard'

export function Board({
  boardId,
  unit,
  meId,
  onBack,
}: {
  boardId: string
  unit: EstimateUnit
  meId: string
  onBack: () => void
}) {
  const board = useBoard(boardId)
  const [announcement, setAnnouncement] = useState('')

  // Объявление ставится с задержкой около секунды: смена фокуса, которая
  // неизбежно следует за перемещением, иначе перебивает его, и скринридер
  // читает пустоту. Так это решено в live-region у Atlassian, и по той же
  // причине здесь role="status", а не alert — alert читается ненадёжно.
  const announce = useCallback((text: string) => {
    setAnnouncement('')
    window.setTimeout(() => setAnnouncement(text), 1000)
  }, [])

  // Узел карточки перемонтируется в новой колонке, и фокус улетает
  // в body. Возвращаем его руками — иначе человек, работающий
  // с клавиатуры, теряет место после каждого переноса.
  const refocus = useCallback((cardId: string) => {
    window.setTimeout(() => {
      document.querySelector<HTMLElement>(`[data-card="${cardId}"]`)?.focus()
    }, 50)
  }, [])
  const [openCard, setOpenCard] = useState<string | null>(null)
  const [showFlow, setShowFlow] = useState(false)

  const { base, order, moveCard } = board

  // Один монитор на всю доску: он знает и источник, и цель, и порядок
  // колонки — вычислять намерение по частям в отдельных обработчиках
  // значит собирать его из неполных данных.
  useEffect(() => {
    return monitorForElements({
      canMonitor: ({ source }) => source.data.kind === 'card',
      onDrop({ source, location }) {
        const target = location.current.dropTargets[0]
        if (!target) return
        const cardId = source.data.cardId as string

        if (target.data.kind === 'column') {
          moveCard(cardId, target.data.columnId as string, { place: 'end' })
          return
        }

        const overCardId = target.data.cardId as string
        const columnId = target.data.columnId as string
        if (overCardId === cardId) return

        // Порядок без перетаскиваемой карточки: иначе соседом окажется
        // она сама, и намерение получится бессмысленным.
        const list = (order[columnId] ?? []).filter((id) => id !== cardId)
        const at = list.indexOf(overCardId)
        const edge = extractClosestEdge(target.data)

        if (edge === 'bottom') {
          moveCard(cardId, columnId, { place: 'after', afterCardId: overCardId })
        } else if (at <= 0) {
          moveCard(cardId, columnId, { place: 'start' })
        } else {
          moveCard(cardId, columnId, { place: 'after', afterCardId: list[at - 1] })
        }
      },
    })
  }, [moveCard, order])

  // Перетаскивание — не единственный способ переместить карточку.
  // Тот же moveCard вызывается с клавиатуры, поэтому доска остаётся
  // управляемой без мыши.
  const moveByKeyboard = useCallback(
    (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => {
      if (!base) return
      const card = base.cards[cardId]
      if (!card) return
      const columnIndex = base.columnIds.indexOf(card.columnId)

      if (direction === 'left' || direction === 'right') {
        const next = base.columnIds[columnIndex + (direction === 'left' ? -1 : 1)]
        if (!next) return
        moveCard(cardId, next, { place: 'end' })
        announce(
          `Карточка «${card.title}» перенесена из «${base.columns[card.columnId].name}» ` +
            `в «${base.columns[next].name}», последняя из ${(order[next]?.length ?? 0) + 1}`,
        )
        refocus(cardId)
        return
      }

      const list = order[card.columnId] ?? []
      const at = list.indexOf(cardId)
      if (direction === 'up') {
        if (at <= 0) return
        if (at === 1) moveCard(cardId, card.columnId, { place: 'start' })
        else moveCard(cardId, card.columnId, { place: 'after', afterCardId: list[at - 2] })
      } else {
        if (at < 0 || at >= list.length - 1) return
        moveCard(cardId, card.columnId, { place: 'after', afterCardId: list[at + 1] })
      }
      const to = direction === 'up' ? at : at + 2
      announce(
        `Карточка «${card.title}» перенесена на позицию ${to} из ${list.length} ` +
          `в колонке «${base.columns[card.columnId].name}»`,
      )
      refocus(cardId)
    },
    [base, order, moveCard, announce, refocus],
  )

  /**
   * Перенос указателем без перетаскивания.
   *
   * Это не удобство, а требование: WCAG 2.5.7 прямо говорит, что
   * клавиатурного эквивалента недостаточно — нужен путь, выполнимый
   * одним кликом. Перетаскивание таким путём не является, а меню
   * на карточке — является.
   */
  const moveToColumn = useCallback(
    (cardId: string, columnId: string) => {
      if (!base) return
      const card = base.cards[cardId]
      if (!card || card.columnId === columnId) return
      moveCard(cardId, columnId, { place: 'end' })
      announce(
        `Карточка «${card.title}» перенесена из «${base.columns[card.columnId].name}» ` +
          `в «${base.columns[columnId].name}»`,
      )
      refocus(cardId)
    },
    [base, moveCard, announce, refocus],
  )

  if (board.loadError) {
    return (
      <div className="centered">
        <p>{board.loadError}</p>
        <button onClick={() => void board.reload()}>Попробовать ещё раз</button>
      </div>
    )
  }
  if (!base) return <div className="centered">Загружаем доску…</div>

  return (
    <div className="board-screen">
      <header className="board-header">
        <button className="link" onClick={onBack}>
          ← Все доски
        </button>
        <h1>{base.info.name}</h1>
        <span className="version" title="Версия доски растёт с каждой операцией">
          v{base.info.version}
        </span>
        {board.pending > 0 && (
          <span className="pending" title="Изменения ещё не подтверждены сервером">
            сохраняем… {board.pending}
          </span>
        )}
        {/* Тема и плотность живут и здесь. Плотность нужна ровно там, где
            много карточек, то есть на доске, — а переключатель до сих пор
            стоял только в списке досок, где он бесполезен. */}
        <div className="board-header-tail">
          <Appearance />
        </div>
      </header>

      <div className="notices">
        {board.notices.map((n) => (
          <div key={n.id} className={`notice notice--${n.tone}`} role="status">
            <span>{n.text}</span>
            {n.retry && (
              <button
                onClick={() => {
                  n.retry?.()
                  board.dismiss(n.id)
                }}
              >
                Повторить
              </button>
            )}
            <button className="close" onClick={() => board.dismiss(n.id)} aria-label="Закрыть">
              ×
            </button>
          </div>
        ))}
      </div>

      {/* Одна полоса, а не три: подсказка о потоке, итерации и переход
          к потоку — это всё «про доску целиком», и разносить их
          по отдельным строкам значит съедать высоту у самих колонок. */}
      <div className="board-toolbar">
        <div className="row row--between">
          <div className="row">
            <FlowHint columns={base.columnIds.map((id) => base.columns[id])} />
            <Iterations boardId={boardId} iterations={base.iterations} onChanged={board.reload} />
          </div>
          <button
            className="link"
            onClick={() => {
              // Две панели разом перекрывают друг друга, а в модальном
              // режиме ещё и спорят за фокус. Открываем по одной.
              setOpenCard(null)
              setShowFlow((v) => !v)
            }}
          >
            Поток
          </button>
        </div>
      </div>

      {Object.keys(base.cards).length === 0 && (
        <div className="note empty-board board-toolbar" role="note">
          <p className="small">
            <strong>На доске ещё нет карточек.</strong> Это не поломка —
            просто здесь пока ничего не заводили.
          </p>
          <p className="muted small">
            Заведите первую в колонке «{base.columns[base.columnIds[0]]?.name ?? 'первой'}».
            Дальше её можно перетащить мышью, перенести кнопкой на самой карточке
            или клавишами: Ctrl со стрелками.
          </p>
        </div>
      )}

      <div className="columns">
        {base.columnIds.map((columnId) => (
          <ColumnView
            key={columnId}
            name={base.columns[columnId].name}
            columnId={columnId}
            column={base.columns[columnId]}
            cardIds={order[columnId] ?? []}
            cards={base.cards}
            unit={unit}
            columns={base.columnIds.map((id) => base.columns[id])}
            onMoveToColumn={moveToColumn}
            onOpenCard={(id) => {
              setShowFlow(false)
              setOpenCard(id)
            }}
            onMoveByKeyboard={moveByKeyboard}
            onCreateCard={(title) => void board.createCard(columnId, title)}
            onRenameColumn={(name) => void board.renameColumn(columnId, name)}
            onSetLimit={(limit) => void board.setColumnLimit(columnId, limit)}
            onUpdateColumn={(patch) => void board.updateColumn(columnId, patch)}
            onRenameCard={(cardId, title) => void board.renameCard(cardId, title)}
            onArchiveCard={(cardId) => void board.archiveCard(cardId)}
          />
        ))}
        <NewColumn onCreate={(name) => void board.createColumn(name)} />
      </div>

      {showFlow && <Flow boardId={boardId} onClose={() => setShowFlow(false)} />}

      {openCard && base.cards[openCard] && (
        <CardPanel
          base={base}
          boardId={boardId}
          cardId={openCard}
          unit={unit}
          meId={meId}
          canEdit
          onClose={() => setOpenCard(null)}
          onDescribe={(id, text) => void board.describeCard(id, text)}
          onEstimate={(id, value) => void board.estimateCard(id, value)}
          onLink={(from, to, kind) => void board.linkCards(from, to, kind)}
          onUnlink={(from, to, kind) => void board.unlinkCards(from, to, kind)}
          onBlock={(id, reason) => void board.blockCard(id, reason)}
          onUnblock={(id) => void board.unblockCard(id)}
          onField={(id, fieldId, value) => void board.setCardField(id, fieldId, value)}
          onIteration={(id, iterationId) => {
            const current = base.cardIterations[id]
            // Перенос — это выход из одного и вход в другой, и оба факта
            // остаются в истории: карточка не может идти в двух сразу.
            if (current) void board.removeFromIteration(id, current)
            if (iterationId) void board.addToIteration(id, iterationId)
          }}
        />
      )}

      <div className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {announcement}
      </div>
    </div>
  )
}

type ColumnProps = {
  columnId: string
  name: string
  column: Column
  cardIds: string[]
  cards: BaseState['cards']
  unit: EstimateUnit
  columns: Column[]
  onMoveToColumn: (cardId: string, columnId: string) => void
  onOpenCard: (cardId: string) => void
  onMoveByKeyboard: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onCreateCard: (title: string) => void
  onRenameColumn: (name: string) => void
  onSetLimit: (limit: number | null) => void
  onUpdateColumn: (patch: ColumnPatch) => void
  onRenameCard: (cardId: string, title: string) => void
  onArchiveCard: (cardId: string) => void
}

function ColumnView(props: ColumnProps) {
  const dropRef = useRef<HTMLDivElement>(null)
  const [over, setOver] = useState(false)
  const [adding, setAdding] = useState(false)
  const [settings, setSettings] = useState(false)

  useEffect(() => {
    const element = dropRef.current
    if (!element) return
    return dropTargetForElements({
      element,
      canDrop: ({ source }) => source.data.kind === 'card',
      getData: () => ({ kind: 'column', columnId: props.columnId }),
      onDragEnter: () => setOver(true),
      onDragLeave: () => setOver(false),
      onDrop: () => setOver(false),
    })
  }, [props.columnId])

  return (
    <section className={`column${over ? ' column--over' : ''}`} aria-label={props.name}>
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
        <button
          className="link column-settings-toggle"
          aria-expanded={settings}
          onClick={() => setSettings((v) => !v)}
        >
          Разметка
        </button>
      </header>

      {flowMarks(props.column).length > 0 && (
        <div className="card-marks">
          {flowMarks(props.column).map((m) => (
            <span key={m} className="mark">
              {m}
            </span>
          ))}
        </div>
      )}
      {props.column.policy && !settings && (
        <p className="muted small column-policy">{props.column.policy}</p>
      )}
      {settings && (
        <ColumnSettings column={props.column} onUpdate={props.onUpdateColumn} />
      )}

      <div className="cards" ref={dropRef}>
        {props.cardIds.map((cardId) => (
          <CardView
            key={cardId}
            cardId={cardId}
            columnId={props.columnId}
            card={props.cards[cardId]}
            unit={props.unit}
            columns={props.columns}
            onMoveToColumn={props.onMoveToColumn}
            onOpen={() => props.onOpenCard(cardId)}
            onMoveByKeyboard={props.onMoveByKeyboard}
            onRename={(title) => props.onRenameCard(cardId, title)}
            onArchive={() => props.onArchiveCard(cardId)}
          />
        ))}
        {props.cardIds.length === 0 && (
          <p className="empty">
            Пусто. Перетащите карточку сюда или перенесите кнопкой на ней.
          </p>
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
        <button className="add" onClick={() => setAdding(true)}>
          + Добавить карточку
        </button>
      )}
    </section>
  )
}

type CardProps = {
  cardId: string
  columnId: string
  card: Card | undefined
  unit: EstimateUnit
  columns: Column[]
  onMoveToColumn: (cardId: string, columnId: string) => void
  onOpen: () => void
  onMoveByKeyboard: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onRename: (title: string) => void
  onArchive: () => void
}

function CardView({
  cardId,
  columnId,
  card,
  unit,
  columns,
  onMoveToColumn,
  onOpen,
  onMoveByKeyboard,
  onRename,
  onArchive,
}: CardProps) {
  const title = card?.title ?? '…'
  const ref = useRef<HTMLElement>(null)
  const [dragging, setDragging] = useState(false)
  const [edge, setEdge] = useState<Edge | null>(null)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    const element = ref.current
    if (!element) return
    const data = { kind: 'card', cardId, columnId }
    return combine(
      draggable({
        element,
        getInitialData: () => data,
        onDragStart: () => setDragging(true),
        onDrop: () => setDragging(false),
      }),
      dropTargetForElements({
        element,
        canDrop: ({ source }) => source.data.kind === 'card',
        getData: ({ input, element }) =>
          attachClosestEdge(data, { input, element, allowedEdges: ['top', 'bottom'] }),
        onDrag: ({ self }) => setEdge(extractClosestEdge(self.data)),
        onDragLeave: () => setEdge(null),
        onDrop: () => setEdge(null),
      }),
    )
  }, [cardId, columnId])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!(e.ctrlKey || e.metaKey)) return
    const map: Record<string, 'left' | 'right' | 'up' | 'down'> = {
      ArrowLeft: 'left',
      ArrowRight: 'right',
      ArrowUp: 'up',
      ArrowDown: 'down',
    }
    const direction = map[e.key]
    if (!direction) return
    e.preventDefault()
    onMoveByKeyboard(cardId, direction)
  }

  return (
    <article
      ref={ref}
      className={`card${dragging ? ' card--dragging' : ''}${edge ? ` card--edge-${edge}` : ''}`}
      tabIndex={0}
      data-card={cardId}
      role="group"
      aria-label={`Карточка «${title}». Ctrl со стрелками перемещает её.`}
      onKeyDown={onKeyDown}
    >
      {editing ? (
        <EditableText
          value={title}
          autoFocus
          onSave={(next) => {
            onRename(next)
            setEditing(false)
          }}
          onCancel={() => setEditing(false)}
          className="card-title"
        />
      ) : (
        <>
          <span className="card-title" onDoubleClick={() => setEditing(true)}>
            {title}
          </span>
          {card && (card.blocked || card.progress) && (
            <div className="card-marks">
              {card.blocked && (
                <span className="mark mark--blocked" title={card.blocked.reason}>
                  {/* Глиф прячем: скринридер прочитает ⛔ как «знак въезд
                      запрещён» — слово рядом надёжнее. */}
                  <span aria-hidden="true">⛔ </span>
                  Заблокирована: {card.blocked.reason}
                </span>
              )}
              {progressLabel(card, unit) && (
                <span className="mark">{progressLabel(card, unit)}</span>
              )}
            </div>
          )}
          <div className="card-actions">
            {/* Перенос без перетаскивания. Не удобство, а требование
                WCAG 2.5.7: клавиатурного эквивалента недостаточно, нужен
                путь, выполнимый одним нажатием. */}
            <select
              value=""
              className="card-move"
              aria-label={`Перенести «${title}» в другую колонку`}
              onChange={(e) => {
                if (e.target.value) onMoveToColumn(cardId, e.target.value)
              }}
            >
              <option value="">Перенести…</option>
              {columns
                .filter((c) => c.id !== columnId)
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
            </select>
            <button onClick={onOpen} aria-label={`Открыть «${title}»`}>
              Открыть
            </button>
            <button onClick={() => setEditing(true)} aria-label={`Переименовать «${title}»`}>
              Переименовать
            </button>
            <button onClick={onArchive} aria-label={`Удалить «${title}»`}>
              Удалить
            </button>
          </div>
        </>
      )}
    </article>
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

function NewColumn({ onCreate }: { onCreate: (name: string) => void }) {
  const [adding, setAdding] = useState(false)
  const [value, setValue] = useState('')
  if (!adding)
    return (
      <button className="column column--ghost" onClick={() => setAdding(true)}>
        + Колонка
      </button>
    )
  return (
    <form
      className="column column--ghost"
      onSubmit={(e) => {
        e.preventDefault()
        if (value.trim()) {
          onCreate(value.trim())
          setValue('')
          setAdding(false)
        }
      }}
    >
      <input
        autoFocus
        value={value}
        placeholder="Название колонки"
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && setAdding(false)}
      />
      <button type="submit">Создать</button>
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

function EditableText({
  value,
  onSave,
  onCancel,
  className,
  autoFocus,
}: {
  value: string
  onSave: (next: string) => void
  onCancel?: () => void
  className?: string
  autoFocus?: boolean
}) {
  const [editing, setEditing] = useState(Boolean(autoFocus))
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])

  const commit = useMemo(
    () => () => {
      const next = draft.trim()
      if (next && next !== value) onSave(next)
      else onCancel?.()
      setEditing(false)
    },
    [draft, value, onSave, onCancel],
  )

  if (!editing)
    return (
      <button className={`inline-edit ${className ?? ''}`} onClick={() => setEditing(true)}>
        {value}
      </button>
    )

  return (
    <input
      autoFocus
      className={className}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
        if (e.key === 'Escape') {
          setDraft(value)
          setEditing(false)
          onCancel?.()
        }
      }}
    />
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

/**
 * Чего не хватает доске для метрик потока. Показывается один раз сверху,
 * а не на каждой колонке: это свойство доски целиком.
 */
function FlowHint({ columns }: { columns: Column[] }) {
  const issues = flowIssues(columns)
  if (issues.length === 0) return null
  return (
    <div className="note" role="note">
      {issues.map((text) => (
        <p key={text} className="small">
          {text}
        </p>
      ))}
    </div>
  )
}

/**
 * Итерации доски.
 *
 * Закрытие необратимо, поэтому спрашивается подтверждением: это
 * утверждение «вот что было сделано», а не отметка о прочтении.
 */
function Iterations({
  boardId,
  iterations,
  onChanged,
}: {
  boardId: string
  iterations: Iteration[]
  onChanged: () => void
}) {
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const open = iterations.filter((i) => i.closedAt === null)

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(onChanged).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <div className="iterations">
      {error && <p className="error">{error}</p>}
      <div className="row row--tight">
        {open.length === 0 && !adding && <span className="muted small">Итераций нет</span>}
        {open.map((i) => (
          <span key={i.id} className="mark" title={i.goal}>
            {i.name} · {i.startsOn}—{i.endsOn} · {i.cardCount}
            <button
              className="link"
              onClick={() => {
                if (window.confirm(`Закрыть «${i.name}»? Состав замрёт, вернуть нельзя.`)) {
                  act(api.closeIteration(boardId, i.id))
                }
              }}
            >
              закрыть
            </button>
          </span>
        ))}
        {!adding && (
          <button className="link" onClick={() => setAdding(true)}>
            + итерация
          </button>
        )}
      </div>

      {adding && (
        <form
          className="row row--tight"
          onSubmit={(e) => {
            e.preventDefault()
            const form = e.currentTarget
            const data = new FormData(form)
            const name = String(data.get('name') ?? '').trim()
            if (!name) return
            act(
              api.createIteration(boardId, {
                name,
                goal: String(data.get('goal') ?? ''),
                startsOn: String(data.get('startsOn') ?? ''),
                endsOn: String(data.get('endsOn') ?? ''),
              }),
            )
            setAdding(false)
          }}
        >
          <input name="name" autoFocus placeholder="Название" required />
          <input name="startsOn" type="date" required aria-label="Начало" />
          <input name="endsOn" type="date" required aria-label="Конец" />
          <input name="goal" placeholder="Цель" />
          <button type="submit">Создать</button>
          <button type="button" className="link" onClick={() => setAdding(false)}>
            Отмена
          </button>
        </form>
      )}
    </div>
  )
}
