import { useState } from 'react'
import { api } from './api'
import type { Principal } from './api'

export function Auth({ onSignedIn }: { onSignedIn: (p: Principal) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [org, setOrg] = useState('')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

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
