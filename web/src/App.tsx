import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import type { BoardInfo, User } from './api'
import { Board } from './Board'

export function App() {
  const [user, setUser] = useState<User | null>(null)
  const [checking, setChecking] = useState(true)
  const [boardId, setBoardId] = useState<string | null>(null)

  useEffect(() => {
    api
      .me()
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setChecking(false))
  }, [])

  if (checking) return <div className="centered">Проверяем сессию…</div>
  if (!user) return <Auth onSignedIn={setUser} />
  if (boardId) return <Board boardId={boardId} onBack={() => setBoardId(null)} />

  return (
    <BoardList
      user={user}
      onOpen={setBoardId}
      onSignOut={() => {
        void api.logout().finally(() => setUser(null))
      }}
    />
  )
}

function Auth({ onSignedIn }: { onSignedIn: (user: User) => void }) {
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
      const user =
        mode === 'login'
          ? await api.login(email, password)
          : await api.register(org, name, email, password)
      onSignedIn(user)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не получилось')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="centered">
      <form className="panel" onSubmit={submit}>
        <h1>{mode === 'login' ? 'Вход' : 'Регистрация'}</h1>

        {mode === 'register' && (
          <>
            <label>
              Организация
              <input value={org} onChange={(e) => setOrg(e.target.value)} placeholder="Моя команда" />
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
          {busy ? 'Секунду…' : mode === 'login' ? 'Войти' : 'Создать команду'}
        </button>
        <button
          type="button"
          className="link"
          onClick={() => {
            setMode(mode === 'login' ? 'register' : 'login')
            setError(null)
          }}
        >
          {mode === 'login' ? 'Создать новую команду' : 'У меня уже есть аккаунт'}
        </button>
      </form>
    </div>
  )
}

function BoardList({
  user,
  onOpen,
  onSignOut,
}: {
  user: User
  onOpen: (id: string) => void
  onSignOut: () => void
}) {
  const [boards, setBoards] = useState<BoardInfo[] | null>(null)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .listBoards()
      .then((r) => setBoards(r.boards))
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось загрузить список'))
  }, [])

  useEffect(load, [load])

  return (
    <div className="centered">
      <div className="panel">
        <header className="panel-header">
          <h1>Доски</h1>
          <button className="link" onClick={onSignOut}>
            Выйти ({user.name})
          </button>
        </header>

        {error && <p className="error">{error}</p>}
        {boards === null && <p>Загружаем…</p>}
        {boards?.length === 0 && <p className="muted">Пока ни одной. Создайте первую.</p>}

        <ul className="board-list">
          {boards?.map((b) => (
            <li key={b.id}>
              <button onClick={() => onOpen(b.id)}>{b.name}</button>
            </li>
          ))}
        </ul>

        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim()) return
            api
              .createBoard(name.trim())
              .then((b) => {
                setName('')
                load()
                onOpen(b.id)
              })
              .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось создать доску'))
          }}
        >
          <input
            value={name}
            placeholder="Название новой доски"
            onChange={(e) => setName(e.target.value)}
          />
          <button type="submit" disabled={!name.trim()}>
            Создать
          </button>
        </form>
      </div>
    </div>
  )
}
