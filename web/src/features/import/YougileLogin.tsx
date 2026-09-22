import { useState } from 'react'
import { t } from '../../shared/i18n/index.ts'
import { importApi } from './api.ts'
import { FormError } from '../../shared/ui/Field.tsx'

/**
 * Вход в YouGile ради ключа API: почта и пароль → компания → ключ.
 * Либо ключ, который у человека уже есть.
 *
 * Пароль живёт только здесь и уходит в первые два запроса; получили
 * ключ — компонент исчезает вместе с паролем. Поля входа — не наши,
 * поэтому `autoComplete="off"`: браузер иначе предложит сохранить
 * пароль от YouGile как пароль от этого сайта.
 */
export function YougileLogin({ onKey }: { onKey: (key: string, created: boolean) => void }) {
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [companies, setCompanies] = useState<{ id: string; name: string }[] | null>(null)
  const [companyId, setCompanyId] = useState('')
  const [manual, setManual] = useState(false)
  const [manualKey, setManualKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const ask = <T,>(call: Promise<T>, then: (v: T) => void) => {
    setBusy(true)
    setError(null)
    call
      .then(then)
      // Отказ — словами сервера: он различает «не пустили», «нет сети»
      // и «подождите» и говорит, что делать в каждом случае.
      .catch((e) => setError(e instanceof Error ? e.message : t.imports.failed))
      .finally(() => setBusy(false))
  }

  return (
    <>
      {!manual && (
        <form
          className="stack stack--tight import-login"
          onSubmit={(e) => {
            e.preventDefault()
            if (companies && companyId) {
              ask(importApi.yougileKey(login, password, companyId), (r) => {
                // Пароль больше не нужен — и не держится дольше нужного.
                setPassword('')
                onKey(r.key, r.created)
              })
              return
            }
            ask(importApi.yougileCompanies(login, password), (r) => {
              setCompanies(r.companies)
              setCompanyId(r.companies[0]?.id ?? '')
            })
          }}
        >
          <label className="import-file">
            <span>{t.imports.ygLogin}</span>
            <input type="email" autoComplete="off" value={login} onChange={(e) => setLogin(e.target.value)} />
          </label>
          <label className="import-file">
            <span>{t.imports.ygPassword}</span>
            <input
              type="password"
              autoComplete="off"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value)
                setCompanies(null)
              }}
            />
          </label>
          {companies && companies.length > 0 && (
            <label className="import-file">
              <span>{t.imports.ygCompany}</span>
              <select value={companyId} onChange={(e) => setCompanyId(e.target.value)}>
                {companies.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <div className="form-row">
            <button className="primary" type="submit" disabled={busy} aria-busy={busy || undefined}>
              {companies && companyId ? t.imports.ygGetKey : t.imports.ygFind}
            </button>
            <button type="button" className="link" onClick={() => setManual(true)}>
              {t.imports.ygHaveKey}
            </button>
          </div>
        </form>
      )}

      {manual && (
        <form
          className="form-row"
          onSubmit={(e) => {
            e.preventDefault()
            onKey(manualKey.trim(), false)
          }}
        >
          <label className="import-file">
            <span>{t.imports.ygKey}</span>
            <input autoComplete="off" value={manualKey} onChange={(e) => setManualKey(e.target.value)} />
          </label>
          <button className="primary" type="submit" disabled={manualKey.trim() === ''}>
            {t.imports.ygUseKey}
          </button>
        </form>
      )}

      <FormError>{error}</FormError>
    </>
  )
}
