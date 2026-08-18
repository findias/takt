import { useEffect, useState } from 'react'
import { ROLE_NAMES, api } from '../shared/api/index.ts'
import type { InviteInfo, Principal } from '../shared/api/index.ts'
import { Button } from '../shared/ui/Button.tsx'

/**
 * Приём приглашения по ссылке.
 *
 * Почта берётся из самого приглашения и не редактируется: иначе ссылку можно
 * было бы применить к любому адресу. Пользователь задаёт только имя и пароль —
 * и только если аккаунта ещё нет.
 */
export function InviteScreen({
  token,
  onJoined,
  onCancel,
}: {
  token: string
  onJoined: (p: Principal) => void
  onCancel: () => void
}) {
  const [info, setInfo] = useState<InviteInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .inviteInfo(token)
      .then(setInfo)
      .catch((e) => setError(e instanceof Error ? e.message : 'Ссылка не работает'))
  }, [token])

  if (error)
    return (
      <div className="centered">
        <div className="panel">
          <h1>Приглашение не открылось</h1>
          <p className="error">{error}</p>
          <p className="muted small">
            Попросите владельца организации прислать новую ссылку — приглашения
            действуют неделю и отзываются вручную.
          </p>
          <button onClick={onCancel}>На главную</button>
        </div>
      </div>
    )

  if (!info) return <div className="centered">Открываем приглашение…</div>

  const accept = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const principal = await api.acceptInvite(
        token,
        info.needsAccount ? { name: name.trim(), password } : undefined,
      )
      onJoined(principal)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось принять приглашение')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="centered">
      <form className="panel" onSubmit={accept}>
        <h1>Вас зовут в «{info.orgName}»</h1>
        <p className="muted small">
          Приглашение для <strong>{info.email}</strong>, роль —{' '}
          {ROLE_NAMES[info.role].toLowerCase()}.
        </p>

        {info.needsAccount ? (
          <>
            <label>
              Как вас зовут
              <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
            </label>
            <label>
              Придумайте пароль
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                minLength={8}
                required
              />
            </label>
          </>
        ) : (
          <p className="muted small">
            Аккаунт с этой почтой уже есть. Если вы не вошли, сначала войдите под ним.
          </p>
        )}

        <button type="submit" disabled={busy}>
          {busy ? 'Секунду…' : 'Присоединиться'}
        </button>
        <Button kind="quiet" type="button" onClick={onCancel}>
          Не сейчас
        </Button>
      </form>
    </div>
  )
}
