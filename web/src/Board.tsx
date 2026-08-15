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
import { limitLabel, parseLimitDraft } from './boardModel.ts'
import type { BaseState } from './boardModel.ts'
import type { Card } from './api'
import { progressLabel } from './cardModel'
import { CardPanel } from './CardPanel'
import { useBoard } from './useBoard'

export function Board({ boardId, onBack }: { boardId: string; onBack: () => void }) {
  const board = useBoard(boardId)
  const [announcement, setAnnouncement] = useState('')
  const [openCard, setOpenCard] = useState<string | null>(null)

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
        setAnnouncement(`«${card.title}» перемещена в колонку «${base.columns[next].name}»`)
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
      setAnnouncement(`«${card.title}» перемещена ${direction === 'up' ? 'выше' : 'ниже'}`)
    },
    [base, order, moveCard],
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

      <div className="columns">
        {base.columnIds.map((columnId) => (
          <ColumnView
            key={columnId}
            name={base.columns[columnId].name}
            columnId={columnId}
            wipLimit={base.columns[columnId].wipLimit}
            wipLimitHard={base.columns[columnId].wipLimitHard}
            cardIds={order[columnId] ?? []}
            cards={base.cards}
            onOpenCard={setOpenCard}
            onMoveByKeyboard={moveByKeyboard}
            onCreateCard={(title) => void board.createCard(columnId, title)}
            onRenameColumn={(name) => void board.renameColumn(columnId, name)}
            onSetLimit={(limit) => void board.setColumnLimit(columnId, limit)}
            onRenameCard={(cardId, title) => void board.renameCard(cardId, title)}
            onArchiveCard={(cardId) => void board.archiveCard(cardId)}
          />
        ))}
        <NewColumn onCreate={(name) => void board.createColumn(name)} />
      </div>

      {openCard && base.cards[openCard] && (
        <CardPanel
          base={base}
          cardId={openCard}
          canEdit
          onClose={() => setOpenCard(null)}
          onDescribe={(id, text) => void board.describeCard(id, text)}
          onLink={(from, to, kind) => void board.linkCards(from, to, kind)}
          onUnlink={(from, to, kind) => void board.unlinkCards(from, to, kind)}
          onBlock={(id, reason) => void board.blockCard(id, reason)}
          onUnblock={(id) => void board.unblockCard(id)}
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
  wipLimit: number | null
  wipLimitHard: boolean
  cardIds: string[]
  cards: BaseState['cards']
  onOpenCard: (cardId: string) => void
  onMoveByKeyboard: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onCreateCard: (title: string) => void
  onRenameColumn: (name: string) => void
  onSetLimit: (limit: number | null) => void
  onRenameCard: (cardId: string, title: string) => void
  onArchiveCard: (cardId: string) => void
}

function ColumnView(props: ColumnProps) {
  const dropRef = useRef<HTMLDivElement>(null)
  const [over, setOver] = useState(false)
  const [adding, setAdding] = useState(false)

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
        <EditableText value={props.name} onSave={props.onRenameColumn} className="column-title" />
        <ColumnCount
          count={props.cardIds.length}
          limit={props.wipLimit}
          hard={props.wipLimitHard}
          onSetLimit={props.onSetLimit}
        />
      </header>

      <div className="cards" ref={dropRef}>
        {props.cardIds.map((cardId) => (
          <CardView
            key={cardId}
            cardId={cardId}
            columnId={props.columnId}
            card={props.cards[cardId]}
            onOpen={() => props.onOpenCard(cardId)}
            onMoveByKeyboard={props.onMoveByKeyboard}
            onRename={(title) => props.onRenameCard(cardId, title)}
            onArchive={() => props.onArchiveCard(cardId)}
          />
        ))}
        {props.cardIds.length === 0 && <p className="empty">Пусто. Перетащите карточку сюда.</p>}
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
  onOpen: () => void
  onMoveByKeyboard: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onRename: (title: string) => void
  onArchive: () => void
}

function CardView({ cardId, columnId, card, onOpen, onMoveByKeyboard, onRename, onArchive }: CardProps) {
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
                  ⛔ {card.blocked.reason}
                </span>
              )}
              {progressLabel(card) && <span className="mark">{progressLabel(card)}</span>}
            </div>
          )}
          <div className="card-actions">
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
