import { useCallback, useEffect, useState } from 'react'
import { ROLE_NAMES, api } from './api'
import type { AuditEntry, Invite, Member, Principal, Role } from './api'
import { actorText, auditText, timeText } from './feedModel'

export function Team({ principal }: { principal: Principal }) {
  const [members, setMembers] = useState<Member[] | null>(null)
  const [invites, setInvites] = useState<Invite[]>([])
  const [error, setError] = useState<string | null>(null)
  const [freshLink, setFreshLink] = useState<string | null>(null)
  const isOwner = principal.role === 'owner'

  const load = useCallback(() => {
    api
      .team()
      .then((r) => {
        setMembers(r.members)
        setInvites(r.invites)
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось загрузить команду'))
  }, [])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <div className="stack">
      {error && <p className="error">{error}</p>}

      <section className="stack">
        <h2 className="section-title">В организации</h2>
        {members === null && <p className="muted">Загружаем…</p>}
        <ul className="member-list">
          {members?.map((m) => (
            <li key={m.userId}>
              <div className="member-who">
                <span>{m.name}</span>
                <span className="muted small">{m.email}</span>
              </div>
              {isOwner && m.userId !== principal.id ? (
                <div className="row">
                  <select
                    value={m.role}
                    aria-label={`Роль: ${m.name}`}
                    onChange={(e) => act(api.setRole(m.userId, e.target.value as Role))}
                  >
                    {(Object.keys(ROLE_NAMES) as Role[]).map((r) => (
                      <option key={r} value={r}>
                        {ROLE_NAMES[r]}
                      </option>
                    ))}
                  </select>
                  <button
                    className="link"
                    onClick={() => act(api.removeMember(m.userId))}
                    aria-label={`Исключить ${m.name}`}
                  >
                    Исключить
                  </button>
                </div>
              ) : (
                <span className="role-chip">{ROLE_NAMES[m.role]}</span>
              )}
            </li>
          ))}
        </ul>
      </section>

      {isOwner && (
        <section className="stack">
          <h2 className="section-title">Пригласить</h2>
          <InviteForm
            onCreated={(invite) => {
              setFreshLink(invite.link ?? null)
              load()
            }}
            onError={setError}
          />

          {freshLink && (
            <div className="note">
              <p className="small">
                Ссылка создана. Она показывается один раз — в базе хранится только
                её отпечаток. Перешлите её приглашённому.
              </p>
              <div className="row">
                <input readOnly value={freshLink} onFocus={(e) => e.target.select()} />
                <button onClick={() => void navigator.clipboard?.writeText(freshLink)}>
                  Скопировать
                </button>
              </div>
            </div>
          )}

          {invites.length > 0 && (
            <>
              <h2 className="section-title">Ждут ответа</h2>
              <ul className="member-list">
                {invites.map((i) => (
                  <li key={i.id}>
                    <div className="member-who">
                      <span>{i.email}</span>
                      <span className="muted small">
                        {ROLE_NAMES[i.role]} · до{' '}
                        {new Date(i.expiresAt).toLocaleDateString('ru-RU')}
                      </span>
                    </div>
                    <button className="link" onClick={() => act(api.revokeInvite(i.id))}>
                      Отозвать
                    </button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </section>
      )}

      <AuditFeed />
    </div>
  )
}

/**
 * Журнал административных действий.
 *
 * Читают его владелец организации и наблюдатель всей организации; всем
 * остальным приходит пустая лента, и раздел просто не появляется. Отказ
 * здесь был бы хуже: он подтверждал бы, что журнал есть и в нём что-то
 * лежит.
 */
function AuditFeed() {
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [next, setNext] = useState<number | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback((before?: number) => {
    api
      .audit(before)
      .then((page) => {
        setEntries((current) => (before ? [...current, ...page.entries] : page.entries))
        setNext(page.next)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }, [])

  useEffect(() => load(), [load])

  if (!loaded || entries.length === 0) return null

  return (
    <section className="stack">
      <h2 className="section-title">Что происходило</h2>
      <p className="muted small">
        Журнал ведёт база, а не приложение: изменение, сделанное в обход интерфейса,
        попадает сюда наравне с остальными. Записи только дописываются.
      </p>
      <ul className="feed">
        {entries.map((e) => (
          <li key={e.id}>
            <span>{auditText(e)}</span>
            <span className="muted small">
              {actorText(e.actor)} · {timeText(e.at)}
            </span>
          </li>
        ))}
      </ul>
      {next !== null && (
        <button className="link" onClick={() => load(next)}>
          Показать раньше
        </button>
      )}
    </section>
  )
}

function InviteForm({
  onCreated,
  onError,
}: {
  onCreated: (invite: Invite) => void
  onError: (message: string) => void
}) {
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('member')
  const [busy, setBusy] = useState(false)

  return (
    <form
      className="row"
      onSubmit={(e) => {
        e.preventDefault()
        if (!email.trim()) return
        setBusy(true)
        api
          .invite(email.trim(), role)
          .then((invite) => {
            setEmail('')
            onCreated(invite)
          })
          .catch((e) => onError(e instanceof Error ? e.message : 'Не удалось пригласить'))
          .finally(() => setBusy(false))
      }}
    >
      <input
        type="email"
        value={email}
        placeholder="Почта коллеги"
        onChange={(e) => setEmail(e.target.value)}
        required
      />
      <select value={role} onChange={(e) => setRole(e.target.value as Role)} aria-label="Роль">
        {(Object.keys(ROLE_NAMES) as Role[]).map((r) => (
          <option key={r} value={r}>
            {ROLE_NAMES[r]}
          </option>
        ))}
      </select>
      <button type="submit" disabled={busy || !email.trim()}>
        Пригласить
      </button>
    </form>
  )
}
