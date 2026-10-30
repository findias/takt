import { useState } from 'react'
import type { FormEvent } from 'react'
import { ApiError } from '../../shared/api/index.ts'
import type { Idea } from '../../shared/api/index.ts'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'
import { similarIdeas } from '../../entities/idea/model.ts'
import type { TeamChoice } from '../../entities/idea/model.ts'
import { createIdea } from './api.ts'

/** Поле, под которое сервер кладёт отказ, — по коду, не по тексту. */
const FIELD_OF_CODE: Record<string, string> = {
  idea_title_empty: 'title',
  idea_title_long: 'title',
  idea_body_long: 'body',
  idea_team_unknown: 'team',
}

/**
 * Подать идею (ROADMAP 43, срез 1).
 *
 * Пока набирается название, под ним показываются похожие идеи:
 * поддержать существующую лучше, чем завести вторую такую же, —
 * десять идей по три голоса никто не разберёт, одна с тридцатью
 * разберётся сама. Список для этого уже есть у экрана, запрос не нужен.
 */
export function IdeaForm({
  teams,
  existing,
  onCreated,
  onCancel,
  onSupport,
}: {
  /** Подразделения в порядке дерева: уровень виден отступом. */
  teams: TeamChoice[]
  existing: Idea[]
  onCreated: (idea: Idea) => void
  onCancel: () => void
  /** Нажали похожую: форма сворачивается, голос ставится списком. */
  onSupport: (idea: Idea) => void
}) {
  const s = t.ideas
  const form = useFormErrors()
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [team, setTeam] = useState('')
  const [busy, setBusy] = useState(false)
  const similar = similarIdeas(existing, title)

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (busy) return
    if (!title.trim()) {
      form.report({ title: s.titleRequired })
      return
    }
    setBusy(true)
    form.clear()
    createIdea(title, body, team || null)
      .then(onCreated)
      .catch((err: unknown) => {
        const code = err instanceof ApiError ? (err.body?.code as string | undefined) : undefined
        const field = code ? FIELD_OF_CODE[code] : undefined
        const message = err instanceof Error ? err.message : s.submitFailed
        if (field) form.report({ [field]: message })
        else form.reportForm(message)
      })
      .finally(() => setBusy(false))
  }

  return (
    <form className="stack idea-form" ref={form.ref} onSubmit={submit} noValidate aria-busy={busy || undefined}>
      <h2 className="idea-form-title">{s.formTitle}</h2>
      <Field label={s.title} hint={s.titleHint} {...form.field('title')}>
        {(bind) => (
          <input
            {...bind}
            name="title"
            type="text"
            required
            maxLength={200}
            autoFocus
            value={title}
            disabled={busy}
            onChange={(e) => setTitle(e.target.value)}
          />
        )}
      </Field>
      {similar.length > 0 && (
        <div className="idea-similar" role="note">
          <p className="small">{s.similar}</p>
          <ul className="idea-similar-list">
            {similar.map((i) => (
              <li key={i.id} className="row row--tight">
                <button type="button" className="link" onClick={() => onSupport(i)}>
                  {i.title}
                </button>
                <span className="muted small">{i.voted ? s.supported : s.support} · {i.votes}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Field label={s.body} hint={s.bodyHint} {...form.field('body')}>
        {(bind) => (
          <textarea
            {...bind}
            name="body"
            rows={3}
            maxLength={5000}
            value={body}
            disabled={busy}
            onChange={(e) => setBody(e.target.value)}
          />
        )}
      </Field>
      <Field label={s.toWhom} {...form.field('team')}>
        {(bind) => (
          <select {...bind} name="team" value={team} disabled={busy} onChange={(e) => setTeam(e.target.value)}>
            <option value="">{s.wholeOrg}</option>
            {teams.map((tm) => (
              <option key={tm.id} value={tm.id}>
                {'\u00a0\u00a0'.repeat(tm.level) + tm.name}
              </option>
            ))}
          </select>
        )}
      </Field>
      <FormError>{form.formError}</FormError>
      <div className="row row--tight">
        <Button kind="primary" type="submit" busy={busy}>
          {s.submit}
        </Button>
        <Button kind="quiet" type="button" onClick={onCancel} disabled={busy}>
          {s.cancel}
        </Button>
      </div>
    </form>
  )
}
