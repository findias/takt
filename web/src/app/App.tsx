import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import { ROLE_NAMES, api } from '../shared/api/index.ts'
import type { Principal } from '../shared/api/index.ts'
import { Board } from '../widgets/Board.tsx'
import { Auth } from '../widgets/Auth.tsx'
import { InviteScreen } from '../widgets/Invite.tsx'
import { BoardList } from '../widgets/BoardList.tsx'
import { Appearance } from '../shared/ui/Appearance.tsx'
import { Skeleton } from '../shared/ui/states.tsx'
import { boardPath, navigate, useRoute } from '../shared/router/index.ts'
import { ToastHost } from '../shared/ui/Toast.tsx'
import { ErrorBoundary } from '../shared/ui/ErrorBoundary.tsx'

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
        <Screens />
      </ToastHost>
    </ErrorBoundary>
  )
}

function Screens() {
  const [principal, setPrincipal] = useState<Principal | null>(null)
  const [checking, setChecking] = useState(true)
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

  // Принятое приглашение заменяет адрес, а не добавляет в историю:
  // возвращаться по «назад» к уже использованной ссылке некуда.
  const leaveInvite = useCallback(() => navigate('/', { replace: true }), [])

  if (route.name === 'invite') {
    return (
      <InviteScreen
        token={route.token}
        onJoined={(p) => {
          setPrincipal(p)
          leaveInvite()
        }}
        onCancel={leaveInvite}
      />
    )
  }

  if (checking) return <div className="centered">Проверяем сессию…</div>
  if (!principal) return <Auth onSignedIn={setPrincipal} />
  if (route.name === 'board')
    return (
      <Board
        boardId={route.boardId}
        cardId={route.cardId}
        onCard={(cardId) => navigate(boardPath(route.boardId, cardId))}
        unit={principal.estimateUnit}
        meId={principal.id}
        isOwner={principal.role === 'owner'}
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
            void api.logout().finally(() => setPrincipal(null))
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
        <main className="app-main" tabIndex={-1}>
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
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

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
        <button className="link" onClick={() => setCreating((v) => !v)}>
          {creating ? 'Отмена' : 'Новая организация'}
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
              .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось создать'))
          }}
        >
          <input
            autoFocus
            value={name}
            placeholder="Название организации"
            onChange={(e) => setName(e.target.value)}
          />
          <button type="submit" disabled={!name.trim()}>
            Создать
          </button>
        </form>
      )}

      {error && <p className="error">{error}</p>}
    </header>
  )
}
