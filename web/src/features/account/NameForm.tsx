import { useState } from 'react'
import { ApiError, request } from '../../shared/api/index.ts'
import { useToast } from '../../shared/ui/Toast.tsx'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Смена своего имени (ROADMAP 36.2). До этого имя задавалось один раз —
 * при регистрации или приглашении — и опечатка в нём жила вечно.
 *
 * Пароль не спрашивается: имя — не имя для входа. Если учётную запись
 * ведёт провайдер или каталог, формы нет — только объяснение, где менять:
 * провайдер перепишет имя при следующем входе, и правка выглядела бы
 * как несохранившаяся.
 */
export function NameForm({
  current,
  managed,
  onChanged,
}: {
  current: string
  managed: boolean
  onChanged: (name: string) => void
}) {
  const [name, setName] = useState(current)
  const [busy, setBusy] = useState(false)
  const form = useFormErrors()
  const notify = useToast()

  if (managed) return <p className="muted small">{t.account.nameManaged}</p>

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
        request<{ name: string }>('PUT', '/api/me/name', { name })
          .then((r) => {
            setName(r.name)
            onChanged(r.name)
            notify({ text: t.account.nameChanged(r.name), tone: 'info' })
          })
          .catch((e) => {
            const text = e instanceof Error ? e.message : t.app.changeFailed
            const code = e instanceof ApiError ? e.body?.code : undefined
            if (code === 'name_invalid' || code === 'name_same') form.report({ name: text })
            else form.reportForm(text)
          })
          .finally(() => setBusy(false))
      }}
    >
      <Field label={t.account.yourName} {...form.field('name')}>
        {(bind) => (
          <input
            {...bind}
            name="name"
            type="text"
            autoComplete="name"
            maxLength={100}
            value={name}
            required
            onChange={(e) => setName(e.target.value)}
          />
        )}
      </Field>
      <div className="row">
        <button type="submit" aria-label={t.account.changeName} disabled={busy}>
          {t.app.change}
        </button>
      </div>
      <FormError>{form.formError}</FormError>
    </form>
  )
}
