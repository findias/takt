import { useEffect, useRef, useState } from 'react'
import { ApiError, request } from '../../shared/api/index.ts'
import type { Member } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Владелец правит почту участника (ROADMAP 23.6): опечатка при переносе,
 * заведённый не на тот адрес. Предлагается только тем, у кого сервер
 * разрешил (`emailEditable`): человек состоит лишь в этой организации,
 * и почту не ведёт провайдер или каталог.
 *
 * Спрашивает, потому что меняет имя для входа чужого человека: после
 * смены он войдёт только новым адресом, и сказать ему об этом должен
 * владелец — писем takt не шлёт.
 */
export function MemberEmailDialog({
  member,
  onClose,
  onChanged,
}: {
  member: Member
  onClose: () => void
  onChanged: (email: string) => void
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const [email, setEmail] = useState(member.email)
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
    <dialog className="dialog" ref={ref} aria-label={t.team.emailTitle(member.name)}>
      <h2 className="dialog-title">{t.team.emailTitle(member.name)}</h2>
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
          request<{ email: string }>('PUT', `/api/members/${member.userId}/email`, { email })
            .then((r) => {
              // Сначала закрыть, потом доложить: диалог в верхнем слое
              // спрятал бы тост под собой.
              ref.current?.close()
              onChanged(r.email)
            })
            .catch((e) => {
              const text = e instanceof Error ? e.message : t.common.notDone
              const code = e instanceof ApiError ? e.body?.code : undefined
              if (code === 'email_invalid' || code === 'email_same' || code === 'email_taken')
                form.report({ email: text })
              else form.reportForm(text)
            })
            .finally(() => setBusy(false))
        }}
      >
        <div className="dialog-body stack">
          <Field label={t.team.emailNew} hint={t.team.emailHint} {...form.field('email')}>
            {(bind) => (
              <input
                {...bind}
                autoFocus
                name="email"
                type="email"
                autoComplete="off"
                value={email}
                required
                onChange={(e) => setEmail(e.target.value)}
              />
            )}
          </Field>
          <FormError>{form.formError}</FormError>
        </div>
        <div className="row row--tight dialog-actions">
          <Button kind="primary" type="submit" busy={busy}>
            {t.team.emailSave}
          </Button>
          <Button kind="quiet" type="button" onClick={() => ref.current?.close()}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </dialog>
  )
}
