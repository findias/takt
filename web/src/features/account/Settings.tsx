import { useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { NotificationReason } from '../../shared/api/index.ts'
import { LANGS, lang, switchLang, t } from '../../shared/i18n/index.ts'
import type { Lang } from '../../shared/i18n/index.ts'
import { FormError } from '../../shared/ui/Field.tsx'
import {
  THEMES,
  setDensity,
  setTheme,
  storedDensity,
  storedTheme,
} from '../../shared/lib/appearance.ts'
import type { Density, Theme } from '../../shared/lib/appearance.ts'

/**
 * Язык интерфейса. Хранится у человека на сервере и потому встречает
 * его на любом устройстве; применяется перезагрузкой — почему так,
 * сказано в `shared/i18n`.
 *
 * Названия языков — каждое на своём языке: человек, не читающий
 * по-русски, ищет «English», а не «Английский».
 */
export function LanguageSettings({ account }: { account: Lang | null }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const choose = (next: Lang) => {
    if (next === lang && account === next) return
    setBusy(true)
    setError(null)
    api
      .setLang(next)
      .then(() => switchLang(next))
      .catch((e) => {
        setError(e instanceof Error ? e.message : t.account.langFailed)
        setBusy(false)
      })
  }

  return (
    <fieldset className="account-group" disabled={busy} aria-busy={busy || undefined}>
      <legend>{t.account.langLegend}</legend>
      {LANGS.map((l) => (
        <label key={l} lang={l}>
          <input
            type="radio"
            name="account-lang"
            checked={lang === l}
            onChange={() => choose(l)}
          />
          {t.lang[l]}
        </label>
      ))}
      <p className="muted small">
        {t.account.langHint}
        {/* Пока человек не выбирал, язык решает браузер — и об этом
            стоит сказать: иначе непонятно, откуда взялся нынешний. */}
        {account === null && ` ${t.account.langFromBrowser}`}
      </p>
      <FormError>{error}</FormError>
    </fieldset>
  )
}

/**
 * Тема и плотность. Живут в браузере, а не у учётной записи: они часто
 * разные на разных устройствах — и подсказка говорит об этом прямо,
 * рядом с языком, который как раз переезжает.
 */
export function AppearanceSettings() {
  const [theme, pickTheme] = useState<Theme>(storedTheme)
  const [density, pickDensity] = useState<Density>(storedDensity)

  return (
    <div className="account-group account-group--spaced">
      <fieldset className="account-group">
        <legend>{t.account.themeLegend}</legend>
        {THEMES.map((th) => (
          <label key={th}>
            <input
              type="radio"
              name="account-theme"
              checked={theme === th}
              onChange={() => {
                setTheme(th)
                pickTheme(th)
              }}
            />
            {t.appearance[th]}
          </label>
        ))}
      </fieldset>
      <fieldset className="account-group">
        <legend>{t.account.densityLegend}</legend>
        <label>
          <input
            type="checkbox"
            checked={density === 'compact'}
            onChange={(e) => {
              const next: Density = e.target.checked ? 'compact' : 'normal'
              setDensity(next)
              pickDensity(next)
            }}
          />
          {t.appearance.compact}
        </label>
      </fieldset>
      <p className="muted small">{t.account.appearanceHint}</p>
    </div>
  )
}

const REASONS: NotificationReason[] = ['mentioned', 'assigned', 'blocked', 'block_expired']

/**
 * Какие поводы уведомлений присылать (ROADMAP 29.3). Без выбора
 * уведомления отключают целиком: одного лишнего повода хватает, чтобы
 * колокольчик перестали открывать. Хранится у человека, как язык,
 * и применяется сразу — отменить можно тем же флажком.
 */
export function NotificationSettings({
  muted,
  onMuted,
}: {
  muted: NotificationReason[]
  onMuted: (muted: NotificationReason[]) => void
}) {
  const setMuted = onMuted
  const [error, setError] = useState<string | null>(null)

  const toggle = (reason: NotificationReason, on: boolean) => {
    const next = on ? muted.filter((r) => r !== reason) : [...muted, reason]
    const before = muted
    setMuted(next)
    setError(null)
    api.muteNotifications(next).catch((e) => {
      // Не сохранилось — флажок возвращается туда, где он на самом деле.
      setMuted(before)
      setError(e instanceof Error ? e.message : t.account.notifyFailed)
    })
  }

  return (
    <fieldset className="account-group">
      <legend>{t.account.notifyLegend}</legend>
      {REASONS.map((reason) => (
        <label key={reason}>
          <input
            type="checkbox"
            checked={!muted.includes(reason)}
            onChange={(e) => toggle(reason, e.target.checked)}
          />
          {t.account.notifyReason[reason]}
        </label>
      ))}
      <p className="muted small">{t.account.notifyHint}</p>
      <FormError>{error}</FormError>
    </fieldset>
  )
}
