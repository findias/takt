import { useState } from 'react'
import type { FormEvent } from 'react'
import { ApiError } from '../../shared/api/index.ts'
import type { BoardInfo, Idea, IdeaDecisionKind } from '../../shared/api/index.ts'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'
import { decideIdea } from './api.ts'

const KINDS: IdeaDecisionKind[] = ['taken', 'declined', 'duplicate', 'later']

/** Поле, под которое сервер кладёт отказ, — по коду, не по тексту. */
const FIELD_OF_CODE: Record<string, string> = {
  idea_reason_required: 'note',
  idea_board_required: 'board',
  idea_board_readonly: 'board',
  idea_target_required: 'target',
  idea_target_self: 'target',
}

/**
 * Решение по идее словами (ROADMAP 43, срез 2): четыре исхода, у каждого
 * своё обязательное — доска у «берём», причина у «не берём», другая идея
 * у «уже есть». Проверяется на отправке, своими словами, и сервер
 * отвечает теми же полями по коду.
 */
export function DecisionForm({
  idea,
  boards,
  others,
  onDecided,
  onCancel,
}: {
  idea: Idea
  /** Доски, в которые решающий пишет; пусто, пока грузятся. */
  boards: BoardInfo[]
  /** Остальные идеи — кандидаты в «уже есть». */
  others: Idea[]
  onDecided: (idea: Idea) => void
  onCancel: () => void
}) {
  const s = t.ideas
  const form = useFormErrors()
  const [kind, setKind] = useState<IdeaDecisionKind>('taken')
  const [board, setBoard] = useState('')
  const [target, setTarget] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (busy) return
    const missing: Record<string, string> = {}
    if (kind === 'declined' && !note.trim()) missing.note = s.reasonRequired
    if (kind === 'taken' && !board) missing.board = s.boardRequired
    if (kind === 'duplicate' && !target) missing.target = s.targetRequired
    if (Object.keys(missing).length > 0) {
      form.report(missing)
      return
    }
    setBusy(true)
    form.clear()
    decideIdea(idea.id, {
      decision: kind,
      note,
      ...(kind === 'taken' ? { boardId: board } : {}),
      ...(kind === 'duplicate' ? { intoIdeaId: target } : {}),
    })
      .then(onDecided)
      .catch((err: unknown) => {
        const code = err instanceof ApiError ? (err.body?.code as string | undefined) : undefined
        const field = code ? FIELD_OF_CODE[code] : undefined
        const message = err instanceof Error ? err.message : s.decideFailed
        if (field) form.report({ [field]: message })
        else form.reportForm(message)
      })
      .finally(() => setBusy(false))
  }

  return (
    <form className="stack stack--tight idea-decide" ref={form.ref} onSubmit={submit} noValidate aria-busy={busy || undefined}>
      <fieldset className="idea-kinds" disabled={busy}>
        <legend className="small">{s.decideTitle}</legend>
        {KINDS.map((k) => (
          <label key={k}>
            <input type="radio" name="decision" checked={kind === k} onChange={() => setKind(k)} />
            {s.kinds[k]}
          </label>
        ))}
      </fieldset>
      <p className="muted small idea-kind-hint">{s.kindHints[kind]}</p>
      {kind === 'taken' && (
        <Field label={s.board} hint={s.boardHint} {...form.field('board')}>
          {(bind) => (
            <select {...bind} name="board" value={board} disabled={busy} onChange={(e) => setBoard(e.target.value)}>
              <option value="">{s.boardPick}</option>
              {boards.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name}
                </option>
              ))}
            </select>
          )}
        </Field>
      )}
      {kind === 'duplicate' && (
        <Field label={s.target} {...form.field('target')}>
          {(bind) => (
            <select {...bind} name="target" value={target} disabled={busy} onChange={(e) => setTarget(e.target.value)}>
              <option value="">{s.targetPick}</option>
              {others.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.title}
                </option>
              ))}
            </select>
          )}
        </Field>
      )}
      <Field label={s.note} hint={s.noteHint} {...form.field('note')}>
        {(bind) => (
          <textarea
            {...bind}
            name="note"
            rows={2}
            maxLength={2000}
            required={kind === 'declined'}
            value={note}
            disabled={busy}
            onChange={(e) => setNote(e.target.value)}
          />
        )}
      </Field>
      <FormError>{form.formError}</FormError>
      <div className="row row--tight">
        <Button kind="primary" type="submit" busy={busy}>
          {s.save}
        </Button>
        <Button kind="quiet" type="button" onClick={onCancel} disabled={busy}>
          {s.cancel}
        </Button>
      </div>
    </form>
  )
}
