import { useEffect, useState } from 'react'
import { ApiError, MIN_PASSWORD, api } from '../shared/api/index.ts'
import type { PasswordLinkInfo, Principal } from '../shared/api/index.ts'
import { Field, FormError, ScreenError, useFormErrors } from '../shared/ui/Field.tsx'
import { locale, t } from '../shared/i18n/index.ts'

/**
 * Ссылка «задать пароль» (ROADMAP 23.6).
 *
 * Её выпускает владелец тому, кого завёл перенос, или тому, кто забыл
 * пароль: писем takt не шлёт. Почта не редактируется — ссылка адресная.
 * Поле пароля одно, как при регистрации и в приглашении: ошибся —
 * владелец выпустит новую ссылку, и прежняя от этого погаснет.
 */
export function PasswordLinkScreen({
  token,
  onSignedIn,
  onCancel,
}: {
  token: string
  onSignedIn: (p: Principal) => void
  onCancel: () => void
}) {
  const s = t.passwordLink
  const [info, setInfo] = useState<PasswordLinkInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const form = useFormErrors()

  useEffect(() => {
    api
      .passwordLinkInfo(token)
      .then(setInfo)
      .catch((e) => setError(e instanceof Error ? e.message : s.failed))
  }, [token, s])

  if (error && !info)
    return (
      <div className="centered">
        <div className="panel">
          <h1>{s.failed}</h1>
          <ScreenError>{error}</ScreenError>
          <button onClick={onCancel}>{s.toHome}</button>
        </div>
      </div>
    )
  if (!info) return <div className="centered">{s.opening}</div>

  return (
    <div className="centered">
      <form
        className="panel"
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
            .usePasswordLink(token, password)
            .then(onSignedIn)
            .catch((e) => {
              const text = e instanceof Error ? e.message : t.common.notDone
              const code = e instanceof ApiError ? e.body?.code : undefined
              if (code === 'password_short') form.report({ password: text })
              else form.reportForm(text)
            })
            .finally(() => setBusy(false))
        }}
      >
        <h1>{s.title(info.orgName)}</h1>
        <p className="muted small">
          {s.forWhom(info.name, info.email)}{' '}
          {s.until(new Date(info.expiresAt).toLocaleDateString(locale()))}
        </p>
        <Field label={s.password} hint={t.auth.passwordRule(MIN_PASSWORD)} {...form.field('password')}>
          {(bind) => (
            <input
              {...bind}
              name="password"
              type="password"
              autoComplete="new-password"
              autoFocus
              minLength={MIN_PASSWORD}
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>
        <FormError>{form.formError}</FormError>
        <button type="submit" disabled={busy}>
          {busy ? t.auth.wait : s.submit}
        </button>
      </form>
    </div>
  )
}
