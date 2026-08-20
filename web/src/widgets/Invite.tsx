import { useEffect, useState } from 'react'
import { MIN_PASSWORD, ROLE_NAMES, api } from '../shared/api/index.ts'
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
  signedInAs,
  onJoined,
  onCancel,
}: {
  token: string
  /** Почта того, кто сейчас в браузере, или null, если никто не вошёл. */
  signedInAs: string | null
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

  // Полноэкранный отказ — только когда не открылось само приглашение.
  // Пока он стоял на любой ошибке, неудача при вступлении подменяла собой
  // весь экран: «приглашение не открылось» на открытом приглашении,
  // и человеку некуда было нажать, кроме «на главную».
  if (error && !info)
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

  // Приглашение адресное. Вошедшему под другой почтой заводить второй
  // аккаунт не на что — ему нужно выйти, поэтому вместо формы он видит
  // объяснение и одну кнопку. Сервер это же проверяет и отказывает
  // с кодом invite_other_email: экран может ошибиться, а он — нет.
  const wrongAccount =
    signedInAs !== null && signedInAs.toLowerCase() !== info.email.toLowerCase()

  const signOutAndReopen = () => {
    setBusy(true)
    api
      .logout()
      // Страница перезагружается, а не прячет форму: приглашение открывается
      // заново и уже незнакомцу — с полями имени и пароля, если аккаунта нет.
      .then(() => location.reload())
      .catch((e) => {
        setError(e instanceof Error ? e.message : 'Не удалось выйти')
        setBusy(false)
      })
  }

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

        {wrongAccount ? (
          <>
            {/* Адрес приглашения назван строкой выше, здесь только второй —
                тот, под которым человек вошёл. */}
            <p>
              Вы вошли как <strong>{signedInAs}</strong> — это другой адрес.
            </p>
            <button type="button" disabled={busy} onClick={signOutAndReopen}>
              Выйти и открыть приглашение заново
            </button>
            <Button kind="quiet" type="button" onClick={onCancel}>
              Не сейчас
            </Button>
          </>
        ) : info.needsAccount ? (
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
                minLength={MIN_PASSWORD}
                required
              />
              <span className="muted small">Не короче {MIN_PASSWORD} символов.</span>
            </label>
          </>
        ) : (
          <p className="muted small">
            Аккаунт с этой почтой уже есть. Если вы не вошли, сначала войдите под ним.
          </p>
        )}

        {error && <p className="error">{error}</p>}

        {!wrongAccount && (
          <>
            <button type="submit" disabled={busy}>
              {busy ? 'Секунду…' : 'Присоединиться'}
            </button>
            <Button kind="quiet" type="button" onClick={onCancel}>
              Не сейчас
            </Button>
          </>
        )}
      </form>
    </div>
  )
}
