import { useEffect, useState } from 'react'
import { LINK_KIND_NAMES, api } from './api'
import type { BoardEvent, EstimateUnit, Iteration, LinkKind } from './api'
import { actorText, eventText, timeText } from './feedModel'
import type { BaseState } from './boardModel'
import { candidatesForSubtask, cardDetails, progressLabel, progressRatio } from './cardModel'
import type { Related } from './cardModel'

/**
 * Карточка целиком: описание, подзадачи, связи, блокировка.
 *
 * Подзадача может лежать на доске другой команды — ради этого связи и
 * вынесены в отдельную таблицу. Экран обязан это показывать: у подзадачи
 * видно, где она идёт и чья это команда, иначе «три из пяти» превращается
 * в число без смысла.
 */
export function CardPanel({
  base,
  boardId,
  cardId,
  unit,
  canEdit,
  onClose,
  onDescribe,
  onEstimate,
  onLink,
  onUnlink,
  onBlock,
  onUnblock,
  onIteration,
}: {
  base: BaseState
  boardId: string
  cardId: string
  unit: EstimateUnit
  canEdit: boolean
  onClose: () => void
  onDescribe: (cardId: string, description: string) => void
  onEstimate: (cardId: string, estimate: number | null) => void
  onLink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onUnlink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onBlock: (cardId: string, reason: string) => void
  onUnblock: (cardId: string) => void
  /** null убирает карточку из текущей итерации. */
  onIteration: (cardId: string, iterationId: string | null) => void
}) {
  const details = cardDetails(base, cardId)
  if (!details) return null
  const { card } = details
  const label = progressLabel(card, unit)

  return (
    <aside className="panel-card" aria-label={`Карточка «${card.title}»`}>
      <header className="row row--between">
        <h2 className="section-title">{card.title}</h2>
        <button className="link" onClick={onClose}>
          Закрыть
        </button>
      </header>

      {card.blocked ? (
        <div className="blocked">
          <div className="stack">
            <strong>Заблокирована</strong>
            <span className="small">{card.blocked.reason}</span>
            <span className="muted small">
              с {new Date(card.blocked.blockedAt).toLocaleString('ru-RU')}
            </span>
          </div>
          {canEdit && (
            <button onClick={() => onUnblock(card.id)}>Снять</button>
          )}
        </div>
      ) : (
        canEdit && <BlockForm onBlock={(reason) => onBlock(card.id, reason)} />
      )}

      <IterationPicker
        iterations={base.iterations}
        current={base.cardIterations[card.id] ?? null}
        canEdit={canEdit}
        onChange={(id) => onIteration(card.id, id)}
      />

      <Estimate
        value={card.estimate}
        unit={unit}
        canEdit={canEdit}
        onSave={(value) => onEstimate(card.id, value)}
      />

      <Description
        value={card.description}
        canEdit={canEdit}
        onSave={(text) => onDescribe(card.id, text)}
      />

      {details.parent && (
        <section className="stack">
          <h3 className="section-title">Часть задачи</h3>
          <RelatedRow
            related={details.parent}
            canEdit={canEdit}
            onRemove={() => onUnlink(details.parent!.id, card.id, 'subtask')}
          />
        </section>
      )}

      <section className="stack">
        <div className="row row--between">
          <h3 className="section-title">Подзадачи</h3>
          {label && <span className="muted small">{label}</span>}
        </div>

        {label && (
          <div
            className="progress"
            role="progressbar"
            aria-valuenow={card.progress?.done ?? 0}
            aria-valuemin={0}
            aria-valuemax={card.progress?.total ?? 0}
            aria-label={`Готово ${label}`}
          >
            <div className="progress-fill" style={{ width: `${progressRatio(card) * 100}%` }} />
          </div>
        )}

        {details.subtasks.length === 0 && (
          <p className="muted small">
            Подзадач нет. Подзадача может идти на доске другой команды — прогресс
            всё равно посчитается здесь.
          </p>
        )}
        {details.subtasks.map((s) => (
          <RelatedRow
            key={s.id}
            related={s}
            canEdit={canEdit}
            onRemove={() => onUnlink(card.id, s.id, 'subtask')}
          />
        ))}

        {canEdit && (
          <LinkPicker
            base={base}
            details={details}
            onPick={(toCard, kind) => onLink(card.id, toCard, kind)}
          />
        )}
      </section>

      <History boardId={boardId} cardId={card.id} version={card.version} />

      {details.related.length > 0 && (
        <section className="stack">
          <h3 className="section-title">Связи</h3>
          {details.related.map((r) => (
            <RelatedRow
              key={`${r.kind}-${r.id}`}
              related={r}
              canEdit={canEdit}
              showKind
              onRemove={() => onUnlink(card.id, r.id, r.kind)}
            />
          ))}
        </section>
      )}
    </aside>
  )
}

function RelatedRow({
  related,
  canEdit,
  showKind,
  onRemove,
}: {
  related: Related
  canEdit: boolean
  showKind?: boolean
  onRemove: () => void
}) {
  return (
    <div className={`related${related.reachable ? '' : ' related--hidden'}`}>
      <div className="member-who">
        <span>
          {related.done && <span aria-label="готово">✓ </span>}
          {related.blocked && <span aria-label="заблокирована">⛔ </span>}
          {related.title}
        </span>
        <span className="muted small">
          {showKind ? `${LINK_KIND_NAMES[related.kind]} · ` : ''}
          {related.where}
        </span>
      </div>
      {canEdit && related.reachable && (
        <button className="link" onClick={onRemove}>
          Убрать
        </button>
      )}
    </div>
  )
}

/**
 * Итерация карточки.
 *
 * Предлагаются только открытые: закрытая итерация — утверждение о том, что
 * было сделано, и дописывать в неё задним числом нельзя. Текущая итерация
 * показывается всегда, даже закрытая, иначе карточка выглядела бы ничьей.
 */
function IterationPicker({
  iterations,
  current,
  canEdit,
  onChange,
}: {
  iterations: Iteration[]
  current: string | null
  canEdit: boolean
  onChange: (iterationId: string | null) => void
}) {
  const open = iterations.filter((i) => i.closedAt === null)
  const currentIteration = iterations.find((i) => i.id === current)
  if (open.length === 0 && !currentIteration) return null

  if (!canEdit) {
    return (
      <p className="muted small">
        Итерация: {currentIteration ? currentIteration.name : 'не назначена'}
      </p>
    )
  }

  return (
    <label className="row row--tight">
      <span className="muted small">Итерация</span>
      <select
        value={current ?? ''}
        aria-label="Итерация карточки"
        onChange={(e) => onChange(e.target.value || null)}
      >
        <option value="">Без итерации</option>
        {currentIteration?.closedAt && (
          <option value={currentIteration.id}>{currentIteration.name} (закрыта)</option>
        )}
        {open.map((i) => (
          <option key={i.id} value={i.id}>
            {i.name}
          </option>
        ))}
      </select>
    </label>
  )
}

// Единицы называются коротко: подпись стоит рядом с полем, и повторять
// «очков» в каждой строке незачем.
const UNIT_SHORT: Record<EstimateUnit, string> = {
  points: 'очк.',
  hours: 'ч',
  days: 'дн.',
}

/**
 * Оценка карточки.
 *
 * Пустое поле — «не оценена», и это не то же самое, что ноль: прогресс
 * родителя считается весом только когда оценены все подзадачи. Одна
 * неоценённая — и счёт возвращается к штукам, потому что вес ноль молча
 * выкинул бы работу из знаменателя.
 */
function Estimate({
  value,
  unit,
  canEdit,
  onSave,
}: {
  value: number | null
  unit: EstimateUnit
  canEdit: boolean
  onSave: (value: number | null) => void
}) {
  const [draft, setDraft] = useState(value === null ? '' : String(value))
  useEffect(() => setDraft(value === null ? '' : String(value)), [value])

  if (!canEdit) {
    return (
      <p className="muted small">
        Оценка: {value === null ? 'не поставлена' : `${value} ${UNIT_SHORT[unit]}`}
      </p>
    )
  }

  const commit = () => {
    const trimmed = draft.trim()
    if (trimmed === '') {
      if (value !== null) onSave(null)
      return
    }
    const parsed = Number(trimmed.replace(',', '.'))
    if (!Number.isFinite(parsed) || parsed <= 0) {
      setDraft(value === null ? '' : String(value))
      return
    }
    if (parsed !== value) onSave(parsed)
  }

  return (
    <label className="row row--tight">
      <span className="muted small">Оценка</span>
      <input
        type="text"
        inputMode="decimal"
        className="estimate-input"
        value={draft}
        placeholder="—"
        aria-label={`Оценка карточки, ${UNIT_SHORT[unit]}`}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
      />
      <span className="muted small">{UNIT_SHORT[unit]}</span>
    </label>
  )
}

function Description({
  value,
  canEdit,
  onSave,
}: {
  value: string
  canEdit: boolean
  onSave: (text: string) => void
}) {
  const [draft, setDraft] = useState(value)
  // Карточку могли изменить и не мы: описание перечитывается, когда
  // пришёл новый снимок, но не затирает то, что человек уже печатает.
  useEffect(() => setDraft(value), [value])

  if (!canEdit) {
    return value ? (
      <p className="description">{value}</p>
    ) : (
      <p className="muted small">Описания нет.</p>
    )
  }

  return (
    <textarea
      className="description"
      rows={4}
      value={draft}
      placeholder="Описание: что нужно сделать и что считать сделанным"
      aria-label="Описание карточки"
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => draft !== value && onSave(draft)}
    />
  )
}

function BlockForm({ onBlock }: { onBlock: (reason: string) => void }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')

  if (!open) {
    return (
      <button className="link" onClick={() => setOpen(true)}>
        Отметить блокировку
      </button>
    )
  }

  return (
    <form
      className="row"
      onSubmit={(e) => {
        e.preventDefault()
        if (!reason.trim()) return
        onBlock(reason.trim())
        setReason('')
        setOpen(false)
      }}
    >
      <input
        autoFocus
        value={reason}
        placeholder="Чего ждём"
        aria-label="Причина блокировки"
        onChange={(e) => setReason(e.target.value)}
      />
      <button type="submit" disabled={!reason.trim()}>
        Отметить
      </button>
      <button type="button" className="link" onClick={() => setOpen(false)}>
        Отмена
      </button>
    </form>
  )
}

/**
 * Выбор карточки для связи. Предлагаются только карточки этой доски:
 * связать с чужой можно, но выбирать её здесь не из чего — для этого
 * нужен поиск по организации, а его ещё нет.
 */
function LinkPicker({
  base,
  details,
  onPick,
}: {
  base: BaseState
  details: ReturnType<typeof cardDetails>
  onPick: (toCard: string, kind: LinkKind) => void
}) {
  const [kind, setKind] = useState<LinkKind>('subtask')
  if (!details) return null
  const candidates = candidatesForSubtask(base, details)
  if (candidates.length === 0) return null

  return (
    <div className="row row--tight">
      <select value={kind} onChange={(e) => setKind(e.target.value as LinkKind)} aria-label="Вид связи">
        {(Object.keys(LINK_KIND_NAMES) as LinkKind[]).map((k) => (
          <option key={k} value={k}>
            {LINK_KIND_NAMES[k]}
          </option>
        ))}
      </select>
      <select
        value=""
        aria-label="Карточка для связи"
        onChange={(e) => e.target.value && onPick(e.target.value, kind)}
      >
        <option value="">Выбрать карточку…</option>
        {candidates.map((c) => (
          <option key={c.id} value={c.id}>
            {c.title}
          </option>
        ))}
      </select>
    </div>
  )
}

/**
 * История карточки.
 *
 * Читается отдельным запросом, а не приходит в снимке: у доски событий
 * тысячи, а нужны они на одной карточке и по требованию. Перечитывается
 * при изменении карточки — версия для того и есть.
 */
function History({
  boardId,
  cardId,
  version,
}: {
  boardId: string
  cardId: string
  version: number
}) {
  const [events, setEvents] = useState<BoardEvent[] | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let alive = true
    api
      .boardEvents(boardId, cardId)
      .then((feed) => alive && setEvents(feed.events))
      .catch(() => alive && setFailed(true))
    return () => {
      alive = false
    }
  }, [boardId, cardId, version])

  if (failed) return <p className="muted small">Историю не удалось прочитать.</p>
  if (!events) return <p className="muted small">Загружаем историю…</p>

  return (
    <section className="stack">
      <h3 className="section-title">История</h3>
      <ul className="feed">
        {events.map((e) => (
          <li key={e.id}>
            <span>{eventText(e)}</span>
            <span className="muted small">
              {actorText(e.actor)} · {timeText(e.at)}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}
