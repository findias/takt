import { useState } from 'react'
import { ApiError, MIN_PASSWORD, api } from '../../shared/api/index.ts'
import { useToast } from '../../shared/ui/Toast.tsx'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Смена пароля и обрыв чужих сессий.
 *
 * Текущий пароль спрашивается не для порядка: сессию могли украсть,
 * и смена пароля из украденной сессии заперла бы хозяина снаружи.
 * Об успехе говорит тост, а не строка в форме: форма закрывается, а
 * сказать надо о втором действии — что остальные устройства вышли, —
 * о котором никто не просил и иначе не узнает.
 */
export function PasswordForm({ onDone }: { onDone: () => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)
  const form = useFormErrors()
  const notify = useToast()

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
          .changePassword(current, next)
          .then(() => {
            setCurrent('')
            setNext('')
            // Сначала закрыть, потом доложить: форма живёт в диалоге,
            // а он в верхнем слое и спрятал бы тост под собой.
            onDone()
            notify({ text: t.app.passwordChanged, tone: 'info' })
          })
          .catch((e) => {
            const text = e instanceof Error ? e.message : t.app.changeFailed
            // Оба отказа сервера здесь адресные, и адрес у них разный:
            // «текущий неверен» — про первое поле, «совпадает
            // с текущим» — про второе. Общая плашка внизу заставляла бы
            // человека гадать, какое из двух полей переписывать.
            const code = e instanceof ApiError ? e.body?.code : undefined
            if (code === 'password_wrong') form.report({ current: text })
            else if (code === 'password_same') form.report({ next: text })
            else form.reportForm(text)
          })
          .finally(() => setBusy(false))
      }}
    >
      <Field label={t.app.currentPassword} {...form.field('current')}>
        {(bind) => (
          <input
            {...bind}
            autoFocus
            name="current"
            type="password"
            autoComplete="current-password"
            value={current}
            required
            onChange={(e) => setCurrent(e.target.value)}
          />
        )}
      </Field>
      {/* Правило названо до ввода, а не после: придумывать пароль
          и узнавать требование по отказу — значит придумывать дважды. */}
      <Field
        label={t.app.newPassword}
        hint={t.auth.passwordRule(MIN_PASSWORD)}
        {...form.field('next')}
      >
        {(bind) => (
          <input
            {...bind}
            name="next"
            type="password"
            autoComplete="new-password"
            value={next}
            required
            minLength={MIN_PASSWORD}
            onChange={(e) => setNext(e.target.value)}
          />
        )}
      </Field>
      <div className="row">
        {/* Кнопка не гаснет на незаполненной форме: погашенная кнопка
            не объясняет, чего не хватает, а отказ у поля — объясняет. */}
        <button type="submit" aria-label={t.app.changePassword} disabled={busy}>
          {t.app.change}
        </button>
        {/* Отдельным действием, потому что и повод отдельный: сессия
            утекает и без пароля — чужой компьютер, забытая вкладка. */}
        <button
          type="button"
          className="link"
          disabled={busy}
          onClick={() => {
            setBusy(true)
            form.clear()
            api
              .signOutElsewhere()
              .then(() => {
                onDone()
                notify({ text: t.app.signedOutElsewhere, tone: 'info' })
              })
              .catch((e) =>
                form.reportForm(e instanceof Error ? e.message : t.common.notDone),
              )
              .finally(() => setBusy(false))
          }}
        >
          {t.app.signOutEverywhere}
        </button>
      </div>
      <FormError>{form.formError}</FormError>
    </form>
  )
}
