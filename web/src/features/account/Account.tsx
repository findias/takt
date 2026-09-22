import { useEffect, useRef, useState } from 'react'
import type { NotificationReason, Principal } from '../../shared/api/index.ts'
import { Button, IconButton } from '../../shared/ui/Button.tsx'
import { ChevronDownIcon, CloseIcon } from '../../shared/ui/icons.tsx'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { TabPanel, Tabs, useTabIds } from '../../shared/ui/Tabs.tsx'
import { t } from '../../shared/i18n/index.ts'
import { AppearanceSettings, LanguageSettings, NotificationSettings } from './Settings.tsx'
import { PasswordForm } from './PasswordForm.tsx'

type Tab = 'lang' | 'appearance' | 'notifications' | 'signin'

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
        <AccountDialog
          principal={principal}
          sandbox={sandbox}
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

function AccountDialog({
  principal,
  sandbox,
  muted,
  onMuted,
  onClose,
  onSignOut,
}: {
  principal: Principal
  sandbox: boolean
  muted: NotificationReason[]
  onMuted: (muted: NotificationReason[]) => void
  onClose: () => void
  onSignOut: () => void
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const [tab, setTab] = useState<Tab>('lang')
  const base = useTabIds()

  // Открывается при монтировании и закрывается размонтированием: так
  // состояние «открыт» живёт в одном месте — у кнопки с именем.
  // Фокус — на выбранную вкладку, а не на крестик, куда его ставит
  // браузер: оттуда стрелки сразу ходят по разделам, а первое нажатие
  // Enter не закрывает только что открытое.
  useEffect(() => {
    const dialog = ref.current
    if (dialog && !dialog.open) dialog.showModal()
    dialog?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus()
  }, [])

  // Закрывается всегда через `close()` — и крестиком, и после смены
  // пароля: тогда браузер сам возвращает фокус на имя. Размонтированный
  // открытым диалог роняет фокус на `body`. Escape делает то же самое
  // силами браузера; обо всех трёх случаях узнаём по событию `close`.
  const close = () => ref.current?.close()
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    dialog.addEventListener('close', onClose)
    return () => dialog.removeEventListener('close', onClose)
  }, [onClose])

  // В песочнице вкладки «Вход» нет: пароля не знает никто, в том числе
  // посетитель, — менять нечего.
  const tabs = [
    { id: 'lang', label: t.account.tabLang },
    { id: 'appearance', label: t.account.tabAppearance },
    { id: 'notifications', label: t.account.tabNotifications },
    ...(sandbox ? [] : [{ id: 'signin', label: t.account.tabSignIn }]),
  ]

  return (
    <dialog className="dialog account-dialog" ref={ref} aria-labelledby={`${base}title`}>
      <div className="account-head">
        <div>
          <h2 className="dialog-title" id={`${base}title`}>
            {t.account.title}
          </h2>
          {/* Почта песочницы выдумана (`…@demo.invalid`) и посетителю
              ничего не говорит. */}
          <p className="muted small account-who">
            {sandbox ? principal.name : `${principal.name} · ${principal.email}`}
          </p>
        </div>
        <IconButton label={t.common.close} onClick={close}>
          <CloseIcon />
        </IconButton>
      </div>

      <Tabs
        base={base}
        tabs={tabs}
        active={tab}
        onSelect={(id) => setTab(id as Tab)}
        label={t.account.tabs}
      />
      <TabPanel base={base} id={tab}>
        {tab === 'lang' && <LanguageSettings account={principal.lang ?? null} />}
        {tab === 'appearance' && <AppearanceSettings />}
        {tab === 'notifications' && (
          <NotificationSettings muted={muted} onMuted={onMuted} />
        )}
        {tab === 'signin' && <PasswordForm onDone={close} />}
      </TabPanel>

      <div className="row dialog-actions account-foot">
        <Button kind="quiet" onClick={onSignOut}>
          {t.app.signOut}
        </Button>
      </div>
    </dialog>
  )
}
