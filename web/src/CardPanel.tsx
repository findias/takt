import { useEffect, useState } from 'react'
import { LINK_KIND_NAMES } from './api'
import type { LinkKind } from './api'
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
  cardId,
  canEdit,
  onClose,
  onDescribe,
  onLink,
  onUnlink,
  onBlock,
  onUnblock,
}: {
  base: BaseState
  cardId: string
  canEdit: boolean
  onClose: () => void
  onDescribe: (cardId: string, description: string) => void
  onLink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onUnlink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onBlock: (cardId: string, reason: string) => void
  onUnblock: (cardId: string) => void
}) {
  const details = cardDetails(base, cardId)
  if (!details) return null
  const { card } = details
  const label = progressLabel(card)

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
