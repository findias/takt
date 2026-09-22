import { useEffect, useRef, useState } from 'react'
import type { NotificationReason, Principal } from '../../shared/api/index.ts'
import { Button, IconButton } from '../../shared/ui/Button.tsx'
import { CloseIcon } from '../../shared/ui/icons.tsx'
import { TabPanel, Tabs, useTabIds } from '../../shared/ui/Tabs.tsx'
import { t } from '../../shared/i18n/index.ts'
import { AppearanceSettings, LanguageSettings, NotificationSettings } from './Settings.tsx'
import { PasswordForm } from './PasswordForm.tsx'
import { EmailForm } from './EmailForm.tsx'

type Tab = 'lang' | 'appearance' | 'notifications' | 'signin'

export function AccountDialog({
  principal,
  sandbox,
  onEmail,
  muted,
  onMuted,
  onClose,
  onSignOut,
}: {
  principal: Principal
  sandbox: boolean
  onEmail: (email: string) => void
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
        {tab === 'signin' && (
          // Два дела входа — адрес и пароль — двумя разделами с именами:
          // без них четыре поля подряд читаются одной формой.
          <div className="account-sections">
            <section aria-labelledby={`${base}email`}>
              <h3 id={`${base}email`}>{t.account.emailLegend}</h3>
              <EmailForm managed={Boolean(principal.emailManaged)} onChanged={onEmail} />
            </section>
            <section aria-labelledby={`${base}password`}>
              <h3 id={`${base}password`}>{t.account.passwordLegend}</h3>
              <PasswordForm onDone={close} />
            </section>
          </div>
        )}
      </TabPanel>

      <div className="row dialog-actions account-foot">
        <Button kind="quiet" onClick={onSignOut}>
          {t.app.signOut}
        </Button>
      </div>
    </dialog>
  )
}
