import { useState } from 'react'
import type { FormEvent } from 'react'
import { ApiError } from '../../shared/api/index.ts'
import type { Idea } from '../../shared/api/index.ts'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'
import type { TeamChoice } from '../../entities/idea/model.ts'
import { moveIdea } from './api.ts'

/** Поле, под которое сервер кладёт отказ, — по коду, не по тексту. */
const FIELD_OF_CODE: Record<string, string> = {
  idea_team_unknown: 'team',
  idea_move_outside: 'team',
}

/** Куда можно перенести; всю организацию форма предлагает сама. */
export type MoveTarget = TeamChoice

/**
 * Перенос идеи на другой уровень (ROADMAP 45): один выбор «кому
 * теперь». Список — только то, куда переносящему можно: кнопка,
 * ведущая в отказ, хуже отсутствующей; решает всё равно база (0087).
 */
export function MoveForm({
  idea,
  targets,
  onMoved,
  onCancel,
}: {
  idea: Idea
  targets: MoveTarget[]
  onMoved: (idea: Idea) => void
  onCancel: () => void
}) {
  const s = t.ideas
  const form = useFormErrors()
  const [team, setTeam] = useState(idea.teamId ?? '')
  const [busy, setBusy] = useState(false)

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    form.clear()
    moveIdea(idea.id, team || null)
      .then(onMoved)
      .catch((err: unknown) => {
        const code = err instanceof ApiError ? (err.body?.code as string | undefined) : undefined
        const field = code ? FIELD_OF_CODE[code] : undefined
        const message = err instanceof Error ? err.message : s.moveFailed
        if (field) form.report({ [field]: message })
        else form.reportForm(message)
      })
      .finally(() => setBusy(false))
  }

  return (
    <form className="stack stack--tight idea-decide idea-move" ref={form.ref} onSubmit={submit} noValidate aria-busy={busy || undefined}>
      <Field label={s.moveTitle} hint={s.moveHint} {...form.field('team')}>
        {(bind) => (
          <select {...bind} name="team" autoFocus value={team} disabled={busy} onChange={(e) => setTeam(e.target.value)}>
            <option value="">{s.wholeOrg}</option>
            {targets.map((tm) => (
              <option key={tm.id} value={tm.id}>
                {'  '.repeat(tm.level) + tm.name}
              </option>
            ))}
          </select>
        )}
      </Field>
      <FormError>{form.formError}</FormError>
      <div className="row row--tight">
        <Button kind="primary" type="submit" busy={busy}>
          {s.moveDo}
        </Button>
        <Button kind="quiet" type="button" onClick={onCancel} disabled={busy}>
          {s.cancel}
        </Button>
      </div>
    </form>
  )
}
