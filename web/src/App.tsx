import { useCallback, useEffect, useState } from 'react'
import { ROLE_NAMES, api } from './api'
import type { Principal } from './api'
import { Board } from './Board'
import { Auth } from './Auth'
import { InviteScreen } from './Invite'
import { Team } from './Team'
import { BoardList } from './BoardList'

/** Приглашение приходит ссылкой вида /invite/<токен>. */
function inviteTokenFromLocation(): string | null {
  const match = window.location.pathname.match(/^\/invite\/(.+)$/)
  return match ? match[1] : null
}

export function App() {
  const [principal, setPrincipal] = useState<Principal | null>(null)
  const [checking, setChecking] = useState(true)
  const [invite, setInvite] = useState<string | null>(inviteTokenFromLocation())
  const [boardId, setBoardId] = useState<string | null>(null)
  const [tab, setTab] = useState<'boards' | 'team'>('boards')

  useEffect(() => {
    api
      .me()
      .then(setPrincipal)
      .catch(() => setPrincipal(null))
      .finally(() => setChecking(false))
  }, [])

  const clearInvite = useCallback(() => {
    window.history.replaceState(null, '', '/')
    setInvite(null)
  }, [])

  if (invite) {
    return (
      <InviteScreen
        token={invite}
        onJoined={(p) => {
          setPrincipal(p)
          setBoardId(null)
          setTab('boards')
          clearInvite()
        }}
        onCancel={clearInvite}
      />
    )
  }

  if (checking) return <div className="centered">Проверяем сессию…</div>
  if (!principal) return <Auth onSignedIn={setPrincipal} />
  if (boardId) return <Board boardId={boardId} onBack={() => setBoardId(null)} />

  return (
    <div className="centered">
      <div className="panel">
        <OrgHeader
          principal={principal}
          onSwitched={(p) => {
            setPrincipal(p)
            setBoardId(null)
          }}
          onSignOut={() => {
            void api.logout().finally(() => setPrincipal(null))
          }}
        />

        <nav className="tabs">
          <button
            className={tab === 'boards' ? 'tab tab--active' : 'tab'}
            onClick={() => setTab('boards')}
          >
            Доски
          </button>
          <button
            className={tab === 'team' ? 'tab tab--active' : 'tab'}
            onClick={() => setTab('team')}
          >
            Команда
          </button>
        </nav>

        {tab === 'boards' ? (
          <BoardList principal={principal} onOpen={setBoardId} />
        ) : (
          <Team principal={principal} />
        )}
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
