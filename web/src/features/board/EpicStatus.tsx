import { useEffect, useState } from 'react'
import { api, EPIC_STATUS_KINDS } from '../../shared/api/index.ts'
import type { EpicStatus, EpicStatusKind } from '../../shared/api/index.ts'
import { statusWord, timeText } from '../../entities/feed/model.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { StatusChip } from './StatusChip.tsx'
import { Field, FormError } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Статус эпика словами (ROADMAP 42.3, срез 10): «идёт», «под угрозой»,
 * «сорван» — с автором, датой и пояснением. Полоса и прогноз считают
 * части; статус — то, что об эпике говорит ведущий его человек, и чего
 * из чисел не вывести. Записи не правятся, а добавляются: история —
 * тоже ответ руководству, «когда это стало под угрозой».
 *
 * Текущий статус приходит с карточкой (снимок доски), история — своим
 * запросом при раскрытии: на панели её читают реже, чем последний.
 */
export function EpicStatusBlock({
  boardId,
  cardId,
  current,
  canEdit,
}: {
  boardId: string
  cardId: string
  current: EpicStatus | undefined
  canEdit: boolean
}) {
  const [editing, setEditing] = useState(false)
  const [history, setHistory] = useState<EpicStatus[] | null>(null)
  const [open, setOpen] = useState(false)
  const s = t.panel

  // История перечитывается, когда меняется текущий статус: своя запись
  // или чужая, пришедшая потоком.
  useEffect(() => {
    if (!open) return
    let live = true
    api
      .epicStatuses(cardId)
      .then((r) => live && setHistory(r.statuses))
      .catch(() => live && setHistory([]))
    return () => {
      live = false
    }
  }, [open, cardId, current?.id])

  return (
    <section className="stack stack--tight epic-status">
      <div className="row row--between">
        <h3 className="section-title">{s.epicStatus}</h3>
        {canEdit && !editing && (
          <Button kind="quiet" type="button" onClick={() => setEditing(true)}>
            {s.epicStatusUpdate}
          </Button>
        )}
      </div>
      {current ? <StatusRow status={current} /> : <p className="muted small">{s.epicStatusNone}</p>}
      {editing && (
        <StatusForm
          boardId={boardId}
          cardId={cardId}
          initial={current?.status ?? 'on_track'}
          onDone={() => setEditing(false)}
        />
      )}
      {current && (
        <details className="epic-status-history" open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
          <summary className="small">
            {history ? s.epicStatusHistory(history.length) : s.epicStatusHistoryClosed}
          </summary>
          {history === null ? (
            <p className="muted small">{s.epicStatusLoading}</p>
          ) : (
            <ul className="epic-status-list">
              {history.map((h) => (
                <li key={h.id}>
                  <StatusRow status={h} />
                </li>
              ))}
            </ul>
          )}
        </details>
      )}
    </section>
  )
}

function StatusRow({ status }: { status: EpicStatus }) {
  return (
    <div className="stack stack--tight">
      <div className="row row--tight">
        <StatusChip status={status.status} />
        <span className="muted small">
          {t.panel.epicStatusBy(status.author ?? t.talk.noName, timeText(status.at))}
        </span>
      </div>
      {status.note && <p className="epic-status-note">{status.note}</p>}
    </div>
  )
}

function StatusForm({
  boardId,
  cardId,
  initial,
  onDone,
}: {
  boardId: string
  cardId: string
  initial: EpicStatusKind
  onDone: () => void
}) {
  const [kind, setKind] = useState<EpicStatusKind>(initial)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const s = t.panel

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError(null)
    // Ответ не кладётся в состояние: сервер двигает версию доски, и
    // текущий статус приедет с карточкой потоком, как чужой.
    api
      .setEpicStatus(boardId, cardId, kind, note)
      .then(onDone)
      .catch((err) => setError(err instanceof Error ? err.message : s.epicStatusFailed))
      .finally(() => setBusy(false))
  }

  return (
    <form className="stack stack--tight epic-status-form" onSubmit={submit}>
      <fieldset className="epic-status-kinds" disabled={busy} aria-busy={busy || undefined}>
        <legend className="small">{s.epicStatusPick}</legend>
        {EPIC_STATUS_KINDS.map((k) => (
          <label key={k}>
            <input type="radio" name="epic-status" checked={kind === k} onChange={() => setKind(k)} />
            {statusWord(k)}
          </label>
        ))}
      </fieldset>
      <Field label={s.epicStatusNote} hint={s.epicStatusNoteHint}>
        {(bind) => (
          <textarea {...bind} rows={2} maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)} />
        )}
      </Field>
      <FormError>{error}</FormError>
      <div className="row row--tight">
        <Button kind="primary" type="submit" busy={busy}>
          {s.epicStatusSave}
        </Button>
        <Button kind="quiet" type="button" onClick={onDone} disabled={busy}>
          {s.epicStatusCancel}
        </Button>
      </div>
    </form>
  )
}
