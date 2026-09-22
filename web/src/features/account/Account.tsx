import { Suspense, lazy, useState } from 'react'
import type { Principal } from '../../shared/api/index.ts'
import { ChevronDownIcon } from '../../shared/ui/icons.tsx'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { t } from '../../shared/i18n/index.ts'

// Сам диалог — отдельным куском: его открывают щелчком по имени, а
// в первой загрузке он со своими формами стоил больше, чем в ней
// оставалось места (web/e2e/perf.spec.ts).
const AccountDialog = lazy(() =>
  import('./AccountDialog.tsx').then((m) => ({ default: m.AccountDialog })),
)

/**
 * Личные настройки: имя в шапке открывает язык, оформление и вход
 * (ROADMAP 30.6).
 *
 * Личное было разбросано по трём местам шапки — имя текстом, «Пароль»
 * и «Выйти» ссылками, тема и язык за кнопкой ◐ без подписи, — и ◐
 * не говорила, что язык там. У Linear и GitHub всё личное за именем
 * или аватаром, и люди ищут его там.
 *
 * Диалог, а не выпадающее меню: внутри формы смены пароля и группы
 * выбора, а меню держит только пункты. Нативный `<dialog>`: ловушку
 * фокуса, Escape и возврат фокуса на имя делает браузер.
 */
export function Account({
  principal,
  onSignOut,
}: {
  principal: Principal
  onSignOut: () => void
}) {
  const [open, setOpen] = useState(false)
  const [leaving, setLeaving] = useState(false)
  // Выбор поводов живёт здесь, а не в диалоге: диалог при закрытии
  // разбирается, а профиль, загруженный при входе, о новом выборе
  // не знает — открыв настройки второй раз, человек увидел бы прежний.
  const [muted, setMuted] = useState(principal.mutedNotifications ?? [])
  const sandbox = Boolean(principal.sandboxExpiresAt)
  // Почта — тоже здесь, по той же причине, что выбор поводов: сменённую
  // в диалоге профиль, загруженный при входе, не знает.
  const [email, setEmail] = useState(principal.email)

  return (
    // Имя классу нужно и печати: личные настройки на бумаге не значат
    // ничего.
    <div className="account">
      <button
        type="button"
        // Тихая кнопка, а не ссылка: у неё уже есть все пять состояний,
        // и она не спорит цветом с действиями шапки.
        className="btn btn--quiet account-name"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={t.account.open(principal.name)}
        onClick={() => setOpen(true)}
      >
        <span className="account-name-text">{principal.name}</span>
        {/* Знак раскрытия: без него имя читается подписью, а не кнопкой,
            и за ним никто не станет искать язык. */}
        <ChevronDownIcon />
      </button>
      {open && (
        <Suspense fallback={null}>
          <AccountDialog
            principal={{ ...principal, email }}
            sandbox={sandbox}
            onEmail={setEmail}
            muted={muted}
            onMuted={setMuted}
            onClose={() => setOpen(false)}
            onSignOut={() => {
              // Диалог закрывается до следующего шага: подтверждение выхода
              // из демо и тост о нём иначе встали бы под него.
              setOpen(false)
              if (sandbox) setLeaving(true)
              else onSignOut()
            }}
          />
        </Suspense>
      )}
      {/* Из песочницы выходят насовсем: пароля нет, и вернуться в неё
          нечем. Необратимое спрашивает. */}
      <ConfirmDialog
        open={leaving}
        title={t.app.leaveDemoTitle}
        confirmLabel={t.app.leaveDemo}
        onCancel={() => setLeaving(false)}
        onConfirm={() => {
          setLeaving(false)
          onSignOut()
        }}
      >
        <p>{t.app.leaveDemoBody}</p>
      </ConfirmDialog>
    </div>
  )
}

