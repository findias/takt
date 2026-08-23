import { useEffect, useRef, useState } from 'react'
import { ApiError, MIN_PASSWORD, api } from '../shared/api/index.ts'
import type { AuthMethods, Principal } from '../shared/api/index.ts'
import { Field, FormError, useFormErrors } from '../shared/ui/Field.tsx'

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
  const form = useFormErrors()
  const { reportForm } = form
  // Причина неудавшегося входа через провайдера приезжает параметром
  // адреса: туда браузер возвращается с чужого сайта, и рассказать о ней
  // иначе неоткуда. Поля у неё нет — вход шёл вообще не по форме.
  useEffect(() => {
    const reason = new URLSearchParams(location.search).get('error')
    if (reason) reportForm(reason)
  }, [reportForm])
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

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    // Проверка на отправке, а не на вводе: до первой отправки форма
    // молчит, потому что незаполненное поле в наполовину заполненной
    // форме — это не ошибка, а середина работы.
    const found = form.check(e.currentTarget)
    if (Object.keys(found).length > 0) {
      form.report(found)
      return
    }
    setBusy(true)
    form.clear()
    try {
      const principal =
        mode === 'login'
          ? await api.login(email, password)
          : await api.register(org, name, email, password)
      onSignedIn(principal)
    } catch (e) {
      const text = e instanceof Error ? e.message : 'Не получилось'
      // Занятая почта — отказ одному полю, и он встаёт под ним:
      // общая плашка внизу формы заставляет искать, к чему она.
      // Различается кодом, а не текстом.
      if (e instanceof ApiError && e.body?.code === 'email_taken') {
        form.report({ email: text })
      } else {
        // «Неверная почта или пароль» намеренно не называет, что из двух:
        // назвать — значит рассказать, заведён ли такой адрес. Поля
        // у такого отказа нет, поэтому он стоит над кнопкой.
        form.reportForm(text)
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="centered">
      <form className="panel" ref={form.ref} noValidate onSubmit={submit}>
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
        {notice && !form.formError && mode === 'login' && (
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
            {/* Название необязательно: пустое сервер заменит на «Моя
                команда», и требовать его на первом же экране — значит
                просить решение там, где его ещё не приняли. */}
            <Field label="Название организации" {...form.field('org')}>
              {(bind) => (
                <input
                  {...bind}
                  name="org"
                  value={org}
                  onChange={(e) => setOrg(e.target.value)}
                  placeholder="Моя команда"
                />
              )}
            </Field>
            <Field label="Как вас зовут" {...form.field('name')}>
              {(bind) => (
                <input
                  {...bind}
                  name="name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                />
              )}
            </Field>
          </>
        )}
        <Field label="Почта" {...form.field('email')}>
          {(bind) => (
            <input
              {...bind}
              name="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="username"
              required
            />
          )}
        </Field>
        {/* Требование сказано до отказа, а не после: придумывать пароль
            и узнавать правило по отказу — значит придумывать дважды.
            На входе подсказки нет — там пароль уже есть. */}
        <Field
          label={mode === 'login' ? 'Пароль' : 'Придумайте пароль'}
          hint={mode === 'register' ? `Не короче ${MIN_PASSWORD} символов.` : undefined}
          {...form.field('password')}
        >
          {(bind) => (
            <input
              {...bind}
              name="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              minLength={MIN_PASSWORD}
              required
            />
          )}
        </Field>

        <FormError>{form.formError}</FormError>

        <button className="primary" type="submit" disabled={busy}>
          {busy ? 'Секунду…' : mode === 'login' ? 'Войти' : 'Завести организацию'}
        </button>
        {/* На закрытой установке организации заводит владелец, и кнопки,
            ведущей к отказу, быть не должно: предлагать дверь, которой
            нет, — то же самое, что показывать корпоративный вход там,
            где он не настроен. Пока ответ не приехал, кнопки тоже нет:
            появившаяся и исчезнувшая кнопка хуже отсутствующей.

            Обратный переход показывается всегда: человек, попавший
            на заведение организации по прежней ссылке, должен иметь
            дорогу назад. */}
        {(mode === 'register' || methods?.signup?.enabled) && (
          <button
            type="button"
            className="link"
            onClick={() => {
              setMode(mode === 'login' ? 'register' : 'login')
              form.clear()
            }}
          >
            {mode === 'login' ? 'Завести новую организацию' : 'У меня уже есть аккаунт'}
          </button>
        )}
        <p className="muted small">
          {methods && !methods.signup?.enabled
            ? 'Организации на этой установке заводит владелец. Чтобы присоединиться, нужна ссылка-приглашение от него.'
            : 'Чтобы присоединиться к существующей команде, нужна ссылка-приглашение от её владельца.'}
        </p>
      </form>
    </div>
  )
}
