import { useCallback, useEffect, useState } from 'react'
import { TEAM_ROLE_NAMES, api } from './api'
import type {
  Member,
  Observer,
  Principal,
  Team,
  TeamAdmin,
  TeamMember,
  TeamRole,
} from './api'
import { allowedParents, buildTree, canNestInside, counters } from './structureModel'
import type { TreeNode } from './structureModel'

/**
 * Структура организации: дерево подразделений, их состав и наблюдение.
 *
 * Читать дерево может любой участник — кто с кем работает, не тайна.
 * Менять его может владелец, поэтому у остальных экран остаётся, но
 * действия с него исчезают: показывать кнопку, которая ответит запретом,
 * хуже, чем не показывать её вовсе.
 */
export function Structure({ principal }: { principal: Principal }) {
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [people, setPeople] = useState<Member[]>([])
  const [observers, setObservers] = useState<Observer[]>([])
  const [admins, setAdmins] = useState<TeamAdmin[]>([])
  const [error, setError] = useState<string | null>(null)
  const isOwner = principal.role === 'owner'

  const load = useCallback(() => {
    Promise.all([api.listTeams(), api.team(), api.listObservers(), api.listAdmins()])
      .then(([t, org, obs, adm]) => {
        setTeams(t.teams)
        setPeople(org.members)
        setObservers(obs.observers)
        setAdmins(adm.admins)
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось загрузить структуру'))
  }, [])

  useEffect(load, [load])

  // Отказы базы здесь содержательные — про глубину, цикл, непустой узел, —
  // и показывать их надо как есть, а не заменять общим «не получилось».
  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  if (teams === null) return <p className="muted">Загружаем…</p>
  const tree = buildTree(teams)

  return (
    <div className="stack">
      {error && <p className="error">{error}</p>}

      <section className="stack">
        <div className="row row--between">
          <h2 className="section-title">Подразделения</h2>
          {isOwner && <NewTeam parent={null} label="Новое подразделение" onCreate={act} />}
        </div>

        {tree.length === 0 && (
          <p className="muted small">
            Подразделений пока нет. Пока их нет, все доски видны всей организации.
          </p>
        )}

        <ul className="tree">
          {tree.map((node) => (
            <TeamNode
              key={node.id}
              node={node}
              tree={tree}
              people={people}
              isOwner={isOwner}
              onAct={act}
            />
          ))}
        </ul>
      </section>

      <Administration
        admins={admins}
        teams={teams}
        people={people}
        isOwner={isOwner}
        onAct={act}
      />

      <Observation
        observers={observers}
        teams={teams}
        people={people}
        isOwner={isOwner}
        onAct={act}
      />
    </div>
  )
}

function TeamNode({
  node,
  tree,
  people,
  isOwner,
  onAct,
}: {
  node: TreeNode
  tree: TreeNode[]
  people: Member[]
  isOwner: boolean
  onAct: (p: Promise<unknown>) => void
}) {
  const [open, setOpen] = useState(false)
  const counts = counters(node)
  const parents = allowedParents(tree, node)

  return (
    <li className="tree-node">
      <div className="tree-row">
        <button
          className="link tree-name"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
        >
          {open ? '▾' : '▸'} {node.name}
        </button>
        {counts && <span className="muted small">{counts}</span>}

        {isOwner && (
          <div className="row row--tight">
            {canNestInside(node) && (
              <NewTeam parent={node.id} label="+ отдел" onCreate={onAct} />
            )}
            <button
              className="link"
              onClick={() => {
                const name = window.prompt('Новое название', node.name)
                if (name && name.trim() && name !== node.name) {
                  onAct(api.renameTeam(node.id, name.trim()))
                }
              }}
            >
              Переименовать
            </button>
            <select
              value=""
              aria-label={`Перенести: ${node.name}`}
              onChange={(e) => {
                if (!e.target.value) return
                const to = e.target.value === 'root' ? null : e.target.value
                onAct(api.moveTeam(node.id, to))
              }}
            >
              <option value="">Перенести…</option>
              {node.parentId && <option value="root">В корень</option>}
              {parents.map((p) => (
                <option key={p.id} value={p.id}>
                  В «{p.name}»
                </option>
              ))}
            </select>
            <button className="link" onClick={() => onAct(api.archiveTeam(node.id))}>
              Убрать
            </button>
          </div>
        )}
      </div>

      {open && <TeamMembers teamId={node.id} people={people} isOwner={isOwner} onAct={onAct} />}

      {node.children.length > 0 && (
        <ul className="tree">
          {node.children.map((child) => (
            <TeamNode
              key={child.id}
              node={child}
              tree={tree}
              people={people}
              isOwner={isOwner}
              onAct={onAct}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

function TeamMembers({
  teamId,
  people,
  isOwner,
  onAct,
}: {
  teamId: string
  people: Member[]
  isOwner: boolean
  onAct: (p: Promise<unknown>) => void
}) {
  const [members, setMembers] = useState<TeamMember[] | null>(null)

  const load = useCallback(() => {
    api
      .teamMembers(teamId)
      .then((r) => setMembers(r.members))
      .catch(() => setMembers([]))
  }, [teamId])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => onAct(p.then(load))
  const outside = people.filter((p) => !members?.some((m) => m.userId === p.userId))

  return (
    <div className="tree-body stack">
      {members === null ? (
        <p className="muted small">Загружаем…</p>
      ) : members.length === 0 ? (
        <p className="muted small">Никого нет. Участник подразделения работает и во всех отделах под ним.</p>
      ) : (
        <ul className="member-list">
          {members.map((m) => (
            <li key={m.userId}>
              <div className="member-who">
                <span>{m.name}</span>
                <span className="muted small">{m.email}</span>
              </div>
              {isOwner ? (
                <div className="row row--tight">
                  <select
                    value={m.role}
                    aria-label={`Роль в подразделении: ${m.name}`}
                    onChange={(e) => act(api.addTeamMember(teamId, m.userId, e.target.value as TeamRole))}
                  >
                    {(Object.keys(TEAM_ROLE_NAMES) as TeamRole[]).map((r) => (
                      <option key={r} value={r}>
                        {TEAM_ROLE_NAMES[r]}
                      </option>
                    ))}
                  </select>
                  <button className="link" onClick={() => act(api.removeTeamMember(teamId, m.userId))}>
                    Убрать
                  </button>
                </div>
              ) : (
                <span className="role-chip">{TEAM_ROLE_NAMES[m.role]}</span>
              )}
            </li>
          ))}
        </ul>
      )}

      {isOwner && outside.length > 0 && (
        <select
          value=""
          aria-label="Добавить в подразделение"
          onChange={(e) => {
            if (e.target.value) act(api.addTeamMember(teamId, e.target.value, 'member'))
          }}
        >
          <option value="">Добавить человека…</option>
          {outside.map((p) => (
            <option key={p.userId} value={p.userId}>
              {p.name}
            </option>
          ))}
        </select>
      )}
    </div>
  )
}

function NewTeam({
  parent,
  label,
  onCreate,
}: {
  parent: string | null
  label: string
  onCreate: (p: Promise<unknown>) => void
}) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')

  if (!open) {
    return (
      <button className="link" onClick={() => setOpen(true)}>
        {label}
      </button>
    )
  }

  return (
    <form
      className="row row--tight"
      onSubmit={(e) => {
        e.preventDefault()
        if (!name.trim()) return
        onCreate(api.createTeam(name.trim(), parent))
        setName('')
        setOpen(false)
      }}
    >
      <input
        autoFocus
        value={name}
        placeholder="Название"
        onChange={(e) => setName(e.target.value)}
        onBlur={() => !name.trim() && setOpen(false)}
      />
      <button type="submit" disabled={!name.trim()}>
        Создать
      </button>
    </form>
  )
}

/**
 * Наблюдение. Признаком его сделать было нельзя: признак у человека
 * означает «вся организация» и ничего другого выразить не может.
 */
function Observation({
  observers,
  teams,
  people,
  isOwner,
  onAct,
}: {
  observers: Observer[]
  teams: Team[]
  people: Member[]
  isOwner: boolean
  onAct: (p: Promise<unknown>) => void
}) {
  const [userId, setUserId] = useState('')
  const [teamId, setTeamId] = useState('')

  return (
    <section className="stack">
      <h2 className="section-title">Наблюдение</h2>
      <p className="muted small">
        Наблюдатель видит доски подразделения и всех отделов под ним, но ничего в них
        не меняет. Закрытые доски не видны и наблюдателю — они открываются поимённо.
      </p>

      {observers.length === 0 ? (
        <p className="muted small">Наблюдателей нет.</p>
      ) : (
        <ul className="member-list">
          {observers.map((o) => (
            <li key={o.id}>
              <div className="member-who">
                <span>{o.name}</span>
                <span className="muted small">
                  {o.teamName ? `Подразделение «${o.teamName}»` : 'Вся организация'}
                </span>
              </div>
              {isOwner && (
                <button className="link" onClick={() => onAct(api.revokeObservation(o.id))}>
                  Отозвать
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {isOwner && people.length > 0 && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!userId) return
            onAct(api.grantObservation(userId, teamId || null))
            setUserId('')
            setTeamId('')
          }}
        >
          <select value={userId} onChange={(e) => setUserId(e.target.value)} aria-label="Кому">
            <option value="">Кому…</option>
            {people.map((p) => (
              <option key={p.userId} value={p.userId}>
                {p.name}
              </option>
            ))}
          </select>
          <select value={teamId} onChange={(e) => setTeamId(e.target.value)} aria-label="За чем">
            <option value="">За всей организацией</option>
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                За «{t.name}»
              </option>
            ))}
          </select>
          <button type="submit" disabled={!userId}>
            Выдать
          </button>
        </form>
      )}
    </section>
  )
}

/**
 * Кто за что отвечает.
 *
 * Администратор подразделения заводит команды под собой, вписывает в них
 * людей и распоряжается досками своей области — и не трогает соседнюю.
 * Раздаёт это только владелец организации: полномочие, размножающее само
 * себя, перестаёт быть ограниченным.
 */
function Administration({
  admins,
  teams,
  people,
  isOwner,
  onAct,
}: {
  admins: TeamAdmin[]
  teams: Team[]
  people: Member[]
  isOwner: boolean
  onAct: (p: Promise<unknown>) => void
}) {
  const [userId, setUserId] = useState('')
  const [teamId, setTeamId] = useState('')

  return (
    <section className="stack">
      <h2 className="section-title">Кто за что отвечает</h2>
      <p className="muted small">
        Администратор подразделения заводит отделы под собой, вписывает людей
        и распоряжается досками своей области. Корневые подразделения и раздачу
        полномочий владелец организации оставляет за собой.
      </p>

      {admins.length === 0 ? (
        <p className="muted small">Никто не назначен.</p>
      ) : (
        <ul className="member-list">
          {admins.map((a) => (
            <li key={a.id}>
              <div className="member-who">
                <span>{a.name}</span>
                <span className="muted small">Подразделение «{a.teamName}»</span>
              </div>
              {isOwner && (
                <button className="link" onClick={() => onAct(api.revokeAdmin(a.id))}>
                  Снять
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {isOwner && people.length > 0 && teams.length > 0 && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!userId || !teamId) return
            onAct(api.grantAdmin(userId, teamId))
            setUserId('')
            setTeamId('')
          }}
        >
          <select value={userId} onChange={(e) => setUserId(e.target.value)} aria-label="Кому">
            <option value="">Кому…</option>
            {people.map((p) => (
              <option key={p.userId} value={p.userId}>
                {p.name}
              </option>
            ))}
          </select>
          <select value={teamId} onChange={(e) => setTeamId(e.target.value)} aria-label="За что">
            <option value="">За какое подразделение…</option>
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          <button type="submit" disabled={!userId || !teamId}>
            Назначить
          </button>
        </form>
      )}
    </section>
  )
}
