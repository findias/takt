import { useState } from 'react'
import { ApiError, api } from '../../shared/api/index.ts'
import { useToast } from '../../shared/ui/Toast.tsx'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Смена своей почты (ROADMAP 23.6).
 *
 * Почта — имя для входа, поэтому спрашивается пароль, как при смене
 * пароля. Писем takt не шлёт, и новый адрес ничем не подтверждается;
 * сервер помечает его, и корпоративный вход по нему запись не привяжет.
 * Если почту ведёт провайдер, формы нет — только объяснение, где её менять.
 */
export function EmailForm({
  managed,
  onChanged,
}: {
  managed: boolean
  onChanged: (email: string) => void
}) {
  const [email, setEmail] = useState('')
  const [current, setCurrent] = useState('')
  const [busy, setBusy] = useState(false)
  const form = useFormErrors()
  const notify = useToast()

  if (managed) return <p className="muted small">{t.account.emailManaged}</p>

  return (
    <form
      className="password-form"
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
        api
          .changeEmail(current, email)
          .then((r) => {
            setEmail('')
            setCurrent('')
            onChanged(r.email)
            notify({ text: t.account.emailChanged(r.email), tone: 'info' })
          })
          .catch((e) => {
            const text = e instanceof Error ? e.message : t.app.changeFailed
            // Отказ — под поле, к которому он относится: «пароль не тот»
            // и «адрес занят» переписывают в разных местах.
            const code = e instanceof ApiError ? e.body?.code : undefined
            if (code === 'password_wrong') form.report({ current: text })
            else if (code === 'email_invalid' || code === 'email_same' || code === 'email_taken')
              form.report({ email: text })
            else form.reportForm(text)
          })
          .finally(() => setBusy(false))
      }}
    >
      <Field label={t.account.newEmail} {...form.field('email')}>
        {(bind) => (
          <input
            {...bind}
            name="email"
            type="email"
            autoComplete="email"
            value={email}
            required
            onChange={(e) => setEmail(e.target.value)}
          />
        )}
      </Field>
      {/* Не «Текущий пароль»: так же названо поле формы пароля ниже,
          и два одинаковых имени на вкладке путают и диктор, и глаз. */}
      <Field label={t.account.yourPassword} {...form.field('current')}>
        {(bind) => (
          <input
            {...bind}
            name="current"
            type="password"
            autoComplete="current-password"
            value={current}
            required
            onChange={(e) => setCurrent(e.target.value)}
          />
        )}
      </Field>
      <div className="row">
        <button type="submit" aria-label={t.account.changeEmail} disabled={busy}>
          {t.app.change}
        </button>
      </div>
      <FormError>{form.formError}</FormError>
    </form>
  )
}
