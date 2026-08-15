import { useEffect, useState } from 'react'
import { api } from './api'
import type { AuthMethods, Principal } from './api'

export function Auth({ onSignedIn }: { onSignedIn: (p: Principal) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [org, setOrg] = useState('')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  // Причина неудавшегося входа через провайдера приезжает параметром
  // адреса: туда браузер возвращается с чужого сайта, и рассказать о ней
  // иначе неоткуда.
  const [error, setError] = useState<string | null>(
    () => new URLSearchParams(location.search).get('error'),
  )
  const [busy, setBusy] = useState(false)
  const [methods, setMethods] = useState<AuthMethods | null>(null)

  // Кнопка корпоративного входа появляется, только если он настроен.
  // Показывать её всегда и объяснять отказ после нажатия — значит
  // предлагать дверь, которой нет.
  useEffect(() => {
    api.authMethods().then(setMethods).catch(() => setMethods(null))
  }, [])

  // Параметр из адреса убирается, чтобы обновление страницы не показывало
  // вчерашнюю ошибку.
  useEffect(() => {
    if (location.search.includes('error=')) {
      history.replaceState(null, '', location.pathname)
    }
  }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const principal =
        mode === 'login'
          ? await api.login(email, password)
          : await api.register(org, name, email, password)
      onSignedIn(principal)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не получилось')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="centered">
      <form className="panel" onSubmit={submit}>
        <h1>{mode === 'login' ? 'Вход' : 'Новая организация'}</h1>

        {mode === 'login' && methods?.oidc.enabled && (
          <>
            {/* Сверху, а не снизу: там, где корпоративный вход настроен,
                он и есть обычный способ, а пароль — исключение. */}
            <a className="button button--wide" href="/api/auth/oidc/start">
              Войти через {methods.oidc.label ?? 'провайдера'}
            </a>
            <p className="divider small muted">или по паролю</p>
          </>
        )}

        {mode === 'register' && (
          <>
            <label>
              Название организации
              <input
                value={org}
                onChange={(e) => setOrg(e.target.value)}
                placeholder="Моя команда"
              />
            </label>
            <label>
              Как вас зовут
              <input value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
          </>
        )}
        <label>
          Почта
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
            required
          />
        </label>
        <label>
          Пароль
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            minLength={8}
            required
          />
        </label>

        {error && <p className="error">{error}</p>}

        <button type="submit" disabled={busy}>
          {busy ? 'Секунду…' : mode === 'login' ? 'Войти' : 'Создать организацию'}
        </button>
        <button
          type="button"
          className="link"
          onClick={() => {
            setMode(mode === 'login' ? 'register' : 'login')
            setError(null)
          }}
        >
          {mode === 'login' ? 'Создать новую организацию' : 'У меня уже есть аккаунт'}
        </button>
        <p className="muted small">
          Чтобы присоединиться к существующей команде, нужна ссылка-приглашение
          от её владельца.
        </p>
      </form>
    </div>
  )
}
