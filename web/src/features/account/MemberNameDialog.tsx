import { useEffect, useRef, useState } from 'react'
import { ApiError, request } from '../../shared/api/index.ts'
import type { Member } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Владелец правит имя участника (ROADMAP 36.2): опечатка при переносе,
 * смена фамилии, человек заведён под псевдонимом. Предлагается только
 * тем, у кого сервер разрешил (`nameEditable`): человек состоит лишь
 * в этой организации, и учётную запись не ведёт провайдер или каталог.
 *
 * Диалог, а не правка на месте: имя стоит под чужой работой — в
 * комментариях, журнале, подписях карточек, — и менять его стоит глядя
 * на то, что вписываешь.
 */
export function MemberNameDialog({
  member,
  onClose,
  onChanged,
}: {
  member: Member
  onClose: () => void
  onChanged: (name: string) => void
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const [name, setName] = useState(member.name)
  const [busy, setBusy] = useState(false)
  const form = useFormErrors()

  useEffect(() => {
    const dialog = ref.current
    if (dialog && !dialog.open) dialog.showModal()
  }, [])
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    dialog.addEventListener('close', onClose)
    return () => dialog.removeEventListener('close', onClose)
  }, [onClose])

  return (
    <dialog className="dialog" ref={ref} aria-label={t.team.nameTitle(member.name)}>
      <h2 className="dialog-title">{t.team.nameTitle(member.name)}</h2>
      <form
        ref={form.ref}
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          if (busy) return
          const found = form.check(e.currentTarget)
          if (Object.keys(found).length > 0) {
            form.report(found)
            return
          }
          setBusy(true)
          form.clear()
          request<{ name: string }>('PUT', `/api/members/${member.userId}/name`, { name })
            .then((r) => {
              // Сначала закрыть, потом доложить: диалог в верхнем слое
              // спрятал бы тост под собой.
              ref.current?.close()
              onChanged(r.name)
            })
            .catch((e) => {
              const text = e instanceof Error ? e.message : t.common.notDone
              const code = e instanceof ApiError ? e.body?.code : undefined
              if (code === 'name_invalid' || code === 'name_same') form.report({ name: text })
              else form.reportForm(text)
            })
            .finally(() => setBusy(false))
        }}
      >
        <div className="dialog-body stack">
          <Field label={t.team.nameNew} hint={t.team.nameHint} {...form.field('name')}>
            {(bind) => (
              <input
                {...bind}
                autoFocus
                name="name"
                type="text"
                autoComplete="off"
                maxLength={100}
                value={name}
                required
                onChange={(e) => setName(e.target.value)}
              />
            )}
          </Field>
          <FormError>{form.formError}</FormError>
        </div>
        <div className="row row--tight dialog-actions">
          <Button kind="primary" type="submit" busy={busy}>
            {t.team.nameSave}
          </Button>
          <Button kind="quiet" type="button" onClick={() => ref.current?.close()}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </dialog>
  )
}
