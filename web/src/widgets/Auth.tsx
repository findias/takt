import { useEffect, useRef, useState } from 'react'
import { MIN_PASSWORD, api } from '../shared/api/index.ts'
import type { AuthMethods, Principal } from '../shared/api/index.ts'

export function Auth({
  onSignedIn,
  notice = null,
}: {
  onSignedIn: (p: Principal) => void
  /** Почему человек снова видит вход: «сеанс кончился». Отказом это
   *  не называется — он ничего не сделал не так. */
  notice?: string | null
}) {
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
  // Текст для диктора ставится с задержкой: смена экрана перебивает
  // объявление, начатое в тот же миг, — правило то же, что и на доске.
  const [said, setSaid] = useState('')
  const heading = useRef<HTMLHeadingElement>(null)

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

  // Экран сменился не по нажатию человека: фокус остался на кнопке,
  // которой больше нет, то есть на `body`, — и клавиатура снова
  // начинает обход с самого начала страницы.
  useEffect(() => {
    if (!notice) {
      setSaid('')
      return
    }
    heading.current?.focus()
    const timer = setTimeout(() => setSaid(notice), 1000)
    return () => clearTimeout(timer)
  }, [notice])

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
        {/* Заголовку можно отдать фокус: возвращать его после подмены
            экрана некуда, а `body` значит «обход с начала страницы». */}
        <h1 ref={heading} tabIndex={-1}>
          {mode === 'login' ? 'Вход' : 'Новая организация'}
        </h1>

        {/* Живой регион один на экран и смонтирован всегда: диктор
            объявляет изменение существующей области, а появление узла
            с текстом пропускает. */}
        <p className="sr-only" role="status">
          {said}
        </p>

        {/* Пока на форме висит отказ, объяснение не показывается:
            две плашки об одном нажатии читаются как две поломки. */}
        {notice && !error && mode === 'login' && (
          <div className="note">
            <p className="small">{notice}</p>
          </div>
        )}

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
          {mode === 'login' ? 'Пароль' : 'Придумайте пароль'}
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            minLength={MIN_PASSWORD}
            required
          />
          {/* Требование сказано до отказа, а не после: придумывать
              пароль и узнавать правило по отказу — значит придумывать
              дважды. Внутри подписи, а не под ней: иначе строка висит
              между полем и кнопкой и читается сама по себе.
              На входе её нет — там пароль уже есть. */}
          {mode === 'register' && (
            <span className="muted small">Не короче {MIN_PASSWORD} символов.</span>
          )}
        </label>

        {error && <p className="error">{error}</p>}

        <button className="primary" type="submit" disabled={busy}>
          {busy ? 'Секунду…' : mode === 'login' ? 'Войти' : 'Завести организацию'}
        </button>
        <button
          type="button"
          className="link"
          onClick={() => {
            setMode(mode === 'login' ? 'register' : 'login')
            setError(null)
          }}
        >
          {mode === 'login' ? 'Завести новую организацию' : 'У меня уже есть аккаунт'}
        </button>
        <p className="muted small">
          Чтобы присоединиться к существующей команде, нужна ссылка-приглашение
          от её владельца.
        </p>
      </form>
    </div>
  )
}
