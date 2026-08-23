import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import {
  ApiError,
  MIN_PASSWORD,
  ROLE_NAMES,
  api,
  onConnectionChange,
  onSessionLost,
} from '../shared/api/index.ts'
import type { Principal } from '../shared/api/index.ts'
import { Board } from '../widgets/Board.tsx'
import { Auth } from '../widgets/Auth.tsx'
import { InviteScreen } from '../widgets/Invite.tsx'
import { BoardList } from '../widgets/BoardList.tsx'
import { Appearance } from '../shared/ui/Appearance.tsx'
import { Skeleton } from '../shared/ui/states.tsx'
import { boardPath, navigate, useRoute } from '../shared/router/index.ts'
import { useDocumentTitle } from '../shared/lib/useDocumentTitle.ts'
import { ToastHost, useToast } from '../shared/ui/Toast.tsx'
import { ErrorBoundary } from '../shared/ui/ErrorBoundary.tsx'
import { Field, FormError, useFormErrors } from '../shared/ui/Field.tsx'
import { ScreenError } from '../shared/ui/Field'

// Экраны организации едут отдельным куском. Работают на доске, а сюда
// заходят раз в месяц — за приглашением, ключом, подпиской, — и грузить
// это вместе с доской значит платить за них при каждом открытии доски.
// Правило то же, что и везде в производительности: самая быстрая
// работа — та, которой нет.
const Team = lazy(() => import('../widgets/Team.tsx').then((m) => ({ default: m.Team })))
const Structure = lazy(() =>
  import('../widgets/Structure.tsx').then((m) => ({ default: m.Structure })),
)

const TABS = [
  ['boards', 'Доски'],
  ['team', 'Команда'],
  ['structure', 'Структура'],
] as const

/** Сообщения общие на всё приложение: их очередь и время жизни —
 *  не забота экрана, который их вызвал. */
export function App() {
  return (
    // Граница снаружи всего: ошибка отрисовки любого экрана обязана
    // оставлять на странице слова и кнопку, а не белое поле.
    <ErrorBoundary>
      <ToastHost>
        <Offline />
        <Screens />
      </ToastHost>
    </ErrorBoundary>
  )
}

/**
 * «Связи с сервером нет» — полосой, пока её нет.
 *
 * Отказ связи до сих пор говорился тостом: «Нет связи с сервером.
 * Карточка вернулась на место», с кнопкой «Повторить». Тост про
 * действие — правильный, но он уезжает через пять секунд, а связь
 * не появляется от того, что сообщение исчезло: между тостами
 * отключённый интерфейс выглядит рабочим, и человек продолжает
 * набирать в пустоту.
 *
 * Разница, которую видно только на чужом продукте (разбор Chromium
 * Issue Tracker 23.08.2026, сеть выключена посреди работы): **разовое
 * событие — тост, длящееся состояние — полоса в постоянном месте.**
 * У них полоса стоит под шапкой во всю ширину и висит, пока связи нет.
 *
 * Живой регион, а не просто красная строка: появление узла диктор
 * пропускает, а изменение существующей области объявляет.
 */
function Offline() {
  const [connected, setConnected] = useState(true)
  useEffect(() => onConnectionChange(setConnected), [])
  // Узел стоит всегда, а меняется текст: скрытую область диктор
  // не читает даже после того, как в ней появилось сообщение, — то же
  // правило, что у отказа формы. Пустой абзац не занимает высоты,
  // поэтому страница от него не прыгает.
  return (
    <p className="offline" role="status">
      {connected
        ? ''
        : 'Связи с сервером нет. Сделанное не потеряется: как только связь вернётся, повторите действие.'}
    </p>
  )
}

function Screens() {
  const [principal, setPrincipal] = useState<Principal | null>(null)
  const [checking, setChecking] = useState(true)
  // Вход после обрыва объясняет, почему человек снова здесь: иначе
  // форма посреди работы выглядит поломкой, а не концом сессии.
  const [ended, setEnded] = useState(false)
  // Номер сессии стоит ключом на рабочих экранах: смена человека
  // за спиной страницы обязана пересобрать их, а не подменить имя
  // в шапке над чужими карточками.
  const [session, setSession] = useState(0)
  // Что открыто — состояние адреса, а не компонента: иначе ссылку
  // на доску прислать нельзя, а перезагрузка возвращает в список.
  const route = useRoute()

  useEffect(() => {
    api
      .me()
      .then(setPrincipal)
      .catch(() => setPrincipal(null))
      .finally(() => setChecking(false))
  }, [])

  // Сеанс кончился где угодно — на любом запросе любого экрана.
  // Здесь единственное место, которое имеет право показать вход:
  // экраны о существовании друг друга не знают и убрать себя не могут.
  // Пока никто не вошёл, отказ «нужно войти» — обычный ответ на первую
  // проверку, и говорить о конце сеанса нечего.
  useEffect(
    () =>
      onSessionLost(() => {
        if (!principal) return
        setPrincipal(null)
        setEnded(true)
      }),
    [principal],
  )

  // Страница, возвращённая кнопкой «назад», приезжает из памяти браузера
  // целиком — с прежним именем в шапке, прежней организацией и прежней
  // ролью. Ничего из этого не перепроверяется само: запросов страница
  // не делает, потому что для неё ничего не изменилось. Спрашиваем,
  // кто вошёл, заново — иначе после выхода «назад» показывает рабочий
  // экран вышедшего, а после смены человека — данные предыдущего.
  //
  // Пока ответ в пути, показываем то же, что и на первой загрузке:
  // рисовать рабочий экран, не зная, чей он, — ровно та поломка,
  // которую здесь и чиним.
  useEffect(() => {
    const restored = (e: PageTransitionEvent) => {
      if (!e.persisted) return
      setChecking(true)
      api
        .me()
        .then((p) => {
          // Сменился человек или организация — все данные под шапкой
          // чужие. Экраны перечитывают своё по своим ключам и о смене
          // сеанса не узнают, поэтому пересобираем их целиком.
          if (principal && (principal.id !== p.id || principal.orgId !== p.orgId)) {
            setSession((n) => n + 1)
          }
          setPrincipal(p)
        })
        .catch(() => setPrincipal(null))
        .finally(() => setChecking(false))
    }
    addEventListener('pageshow', restored)
    return () => removeEventListener('pageshow', restored)
  }, [principal])

  // Заголовок вкладки. Доска и карточка называют себя сами: только они
  // знают, как называется открытое, — а здесь известны лишь разделы.
  useDocumentTitle(
    route.name === 'board'
      ? null
      : route.name === 'invite'
        ? 'Приглашение'
        : (TABS.find(([name]) => name === route.name)?.[1] ?? null),
  )

  // Принятое приглашение заменяет адрес, а не добавляет в историю:
  // возвращаться по «назад» к уже использованной ссылке некуда.
  const leaveInvite = useCallback(() => navigate('/', { replace: true }), [])

  if (route.name === 'invite') {
    return (
      <InviteScreen
        token={route.token}
        // Кто сейчас в браузере: приглашение адресное, и человеку, вошедшему
        // под другой почтой, форму заведения аккаунта показывать незачем —
        // ему нужно другое действие.
        signedInAs={principal?.email ?? null}
        onJoined={(p) => {
          setPrincipal(p)
          leaveInvite()
        }}
        onCancel={leaveInvite}
      />
    )
  }

  if (checking) return <div className="centered">Проверяем сессию…</div>
  if (!principal)
    return (
      <Auth
        notice={
          ended ? 'Сессия истекла — войдите снова. Вы вернётесь на ту же страницу.' : null
        }
        onSignedIn={(p) => {
          setEnded(false)
          setPrincipal(p)
        }}
      />
    )
  if (route.name === 'board')
    return (
      <Board
        key={session}
        boardId={route.boardId}
        cardId={route.cardId}
        onCard={(cardId) => navigate(boardPath(route.boardId, cardId))}
        unit={principal.estimateUnit}
        meId={principal.id}
        isOwner={principal.role === 'owner'}
        canEdit={principal.role !== 'viewer'}
        onBack={() => navigate('/')}
      />
    )

  // Рабочие экраны живут во всю ширину окна: доски, команда и структура —
  // это работа, а не диалог. Карточка по центру осталась там, где ей
  // и место, — на входе и в приглашении: там показывают одну форму,
  // и всё остальное только мешало бы.
  return (
    <div className="app">
      <div className="app-shell">
        <OrgHeader
          principal={principal}
          onSwitched={(p) => {
            setPrincipal(p)
            navigate('/')
          }}
          onSignOut={() => {
            void api.logout().finally(() => {
              setEnded(false)
              setPrincipal(null)
            })
          }}
        />

        <nav className="tabs" aria-label="Разделы">
          {TABS.map(([name, title]) => (
            <button
              key={name}
              className={route.name === name ? 'tab tab--active' : 'tab'}
              aria-current={route.name === name ? 'page' : undefined}
              onClick={() => navigate(name === 'boards' ? '/' : `/${name}`)}
            >
              {title}
            </button>
          ))}
        </nav>

        {/* `tabIndex` — чтобы главной области можно было отдать фокус,
            когда возвращать его больше некуда: диалог, открытый
            из спрятавшегося меню, иначе оставлял фокус на `body`. */}
        <main className="app-main" tabIndex={-1} key={session}>
          {route.name === 'boards' && (
            <BoardList principal={principal} onOpen={(id) => navigate(boardPath(id))} />
          )}
          {/* Заглушка в форме списка, а не слово «загружаем»: кусок
              приезжает за десятки миллисекунд, и мигать словом дольше,
              чем показывать раскладку. */}
          {(route.name === 'team' || route.name === 'structure') && (
            <Suspense fallback={<Skeleton lines={3} />}>
              {route.name === 'team' && <Team principal={principal} />}
              {route.name === 'structure' && (
                <Structure principal={principal} onOpenBoard={(id) => navigate(boardPath(id))} />
              )}
            </Suspense>
          )}
        </main>
      </div>
    </div>
  )
}

function OrgHeader({
  principal,
  onSwitched,
  onSignOut,
}: {
  principal: Principal
  onSwitched: (p: Principal) => void
  onSignOut: () => void
}) {
  const [orgs, setOrgs] = useState<{ orgId: string; orgName: string }[]>([])
  const [creating, setCreating] = useState(false)
  // Можно ли заводить организации: спрашивается там же, где способы
  // входа, — сервер отвечает и то и другое одним ответом.
  const [signup, setSignup] = useState(false)
  useEffect(() => {
    api
      .authMethods()
      // `?.` не для красоты: при выкладке реплики двух версий живут
      // рядом несколько секунд, и старый сервер этого поля не знает.
      // Отсутствие ответа значит «нельзя» — умолчание безопасное.
      .then((m) => setSignup(m.signup?.enabled ?? false))
      .catch(() => setSignup(false))
  }, [])
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [changing, setChanging] = useState(false)

  const load = useCallback(() => {
    api
      .listOrgs()
      .then((r) => setOrgs(r.orgs))
      .catch(() => setOrgs([{ orgId: principal.orgId, orgName: principal.orgName }]))
  }, [principal.orgId, principal.orgName])

  useEffect(load, [load])

  const switchTo = (orgId: string) => {
    if (orgId === principal.orgId) return
    api
      .switchOrg(orgId)
      .then(onSwitched)
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось переключиться'))
  }

  return (
    <header className="org-header">
      <div className="org-row">
        {orgs.length > 1 ? (
          <select
            className="org-select"
            value={principal.orgId}
            onChange={(e) => switchTo(e.target.value)}
            aria-label="Организация"
          >
            {orgs.map((o) => (
              <option key={o.orgId} value={o.orgId}>
                {o.orgName}
              </option>
            ))}
          </select>
        ) : (
          <h1>{principal.orgName}</h1>
        )}
        <span className="role-chip">{ROLE_NAMES[principal.role]}</span>
      </div>

      <div className="org-row muted small">
        <span>{principal.name}</span>
        {/* На закрытой установке организации заводит владелец: правило
            то же, что на экране входа, и место, где его можно обойти,
            должно быть закрыто там же. Иначе «регистрация закрыта»
            означало бы «незнакомцу нельзя, а любому участнику можно». */}
        {signup && (
          <button className="link" onClick={() => setCreating((v) => !v)}>
            {creating ? 'Отмена' : 'Новая организация'}
          </button>
        )}
        <button
          className="link"
          onClick={() => {
            setChanging((v) => !v)
            setError(null)
          }}
        >
          {changing ? 'Отмена' : 'Пароль'}
        </button>
        <button className="link" onClick={onSignOut}>
          Выйти
        </button>
        <Appearance />
      </div>

      {creating && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim()) return
            api
              .createOrg(name.trim())
              .then(() => api.me())
              .then((p) => {
                setName('')
                setCreating(false)
                setError(null)
                load()
                onSwitched(p)
              })
              .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось завести'))
          }}
        >
          {/* Одно поле в ряду с кнопкой: подпись здесь заняла бы строку
              над шапкой ради слова, которое уже стоит в кнопке рядом.
              Но имя у поля обязано быть — иначе диктор читает его как
              «поле ввода», а `placeholder` исчезает с первым символом. */}
          <input
            autoFocus
            value={name}
            aria-label="Название организации"
            placeholder="Название организации"
            onChange={(e) => setName(e.target.value)}
          />
          <button type="submit" aria-label="Завести организацию" disabled={!name.trim()}>
            Завести
          </button>
        </form>
      )}

      {changing && <PasswordForm onDone={() => setChanging(false)} />}

      <ScreenError>{error}</ScreenError>
    </header>
  )
}

/**
 * Смена пароля и обрыв чужих сессий.
 *
 * Текущий пароль спрашивается не для порядка: сессию могли украсть,
 * и смена пароля из украденной сессии заперла бы хозяина снаружи.
 * Об успехе говорит тост, а не строка в форме: форма закрывается, а
 * сказать надо о втором действии — что остальные устройства вышли, —
 * о котором никто не просил и иначе не узнает.
 */
function PasswordForm({ onDone }: { onDone: () => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)
  const form = useFormErrors()
  const notify = useToast()

  return (
    <form
      className="password-form"
      ref={form.ref}
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        if (busy) return
        const found = form.check(e.currentTarget)
        if (Object.keys(found).length > 0) {
          form.report(found)
          return
        }
        setBusy(true)
        form.clear()
        api
          .changePassword(current, next)
          .then(() => {
            setCurrent('')
            setNext('')
            notify({ text: 'Пароль сменён, остальные устройства вышли', tone: 'info' })
            onDone()
          })
          .catch((e) => {
            const text = e instanceof Error ? e.message : 'Не удалось сменить'
            // Оба отказа сервера здесь адресные, и адрес у них разный:
            // «текущий неверен» — про первое поле, «совпадает
            // с текущим» — про второе. Общая плашка внизу заставляла бы
            // человека гадать, какое из двух полей переписывать.
            const code = e instanceof ApiError ? e.body?.code : undefined
            if (code === 'password_wrong') form.report({ current: text })
            else if (code === 'password_same') form.report({ next: text })
            else form.reportForm(text)
          })
          .finally(() => setBusy(false))
      }}
    >
      <Field label="Текущий пароль" {...form.field('current')}>
        {(bind) => (
          <input
            {...bind}
            autoFocus
            name="current"
            type="password"
            autoComplete="current-password"
            value={current}
            required
            onChange={(e) => setCurrent(e.target.value)}
          />
        )}
      </Field>
      {/* Правило названо до ввода, а не после: придумывать пароль
          и узнавать требование по отказу — значит придумывать дважды. */}
      <Field
        label="Новый пароль"
        hint={`Не короче ${MIN_PASSWORD} символов.`}
        {...form.field('next')}
      >
        {(bind) => (
          <input
            {...bind}
            name="next"
            type="password"
            autoComplete="new-password"
            value={next}
            required
            minLength={MIN_PASSWORD}
            onChange={(e) => setNext(e.target.value)}
          />
        )}
      </Field>
      <div className="row">
        {/* Кнопка не гаснет на незаполненной форме: погашенная кнопка
            не объясняет, чего не хватает, а отказ у поля — объясняет. */}
        <button type="submit" aria-label="Сменить пароль" disabled={busy}>
          Сменить
        </button>
        {/* Отдельным действием, потому что и повод отдельный: сессия
            утекает и без пароля — чужой компьютер, забытая вкладка. */}
        <button
          type="button"
          className="link"
          disabled={busy}
          onClick={() => {
            setBusy(true)
            form.clear()
            api
              .signOutElsewhere()
              .then(() => {
                notify({ text: 'Остальные устройства вышли', tone: 'info' })
                onDone()
              })
              .catch((e) =>
                form.reportForm(e instanceof Error ? e.message : 'Не удалось'),
              )
              .finally(() => setBusy(false))
          }}
        >
          Выйти на всех устройствах
        </button>
      </div>
      <FormError>{form.formError}</FormError>
    </form>
  )
}
