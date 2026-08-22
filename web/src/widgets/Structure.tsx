import { useCallback, useEffect, useState } from 'react'
import { VISIBILITY_NAMES, api } from '../shared/api/index.ts'
import type {
  ArchivedTeam,
  Member,
  Observer,
  Principal,
  Team,
  TeamAdmin,
  TeamBoard,
  TeamMember,
} from '../shared/api/index.ts'
import { allowedParents, buildTree, canNestInside, counters } from '../entities/team/model.ts'
import type { TreeNode } from '../entities/team/model.ts'
import { Skeleton } from '../shared/ui/states.tsx'

/**
 * Структура организации: дерево подразделений, их состав и наблюдение.
 *
 * Читать дерево может любой участник — кто с кем работает, не тайна.
 * Менять его может владелец, поэтому у остальных экран остаётся, но
 * действия с него исчезают: показывать кнопку, которая ответит запретом,
 * хуже, чем не показывать её вовсе.
 */
export function Structure({
  principal,
  onOpenBoard,
}: {
  principal: Principal
  /** Доска узла открывается отсюда: структура отвечает на вопрос «чем
   *  занято подразделение», и следующий шаг после ответа — открыть. */
  onOpenBoard: (boardId: string) => void
}) {
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [people, setPeople] = useState<Member[]>([])
  const [observers, setObservers] = useState<Observer[]>([])
  const [admins, setAdmins] = useState<TeamAdmin[]>([])
  const [archived, setArchived] = useState<ArchivedTeam[]>([])
  const [error, setError] = useState<string | null>(null)
  const isOwner = principal.role === 'owner'

  const load = useCallback(() => {
    Promise.all([
      api.listTeams(),
      api.team(),
      api.listObservers(),
      api.listAdmins(),
      api.archivedTeams(),
    ])
      .then(([t, org, obs, adm, arch]) => {
        setTeams(t.teams)
        setPeople(org.members)
        setObservers(obs.observers)
        setAdmins(adm.admins)
        setArchived(arch.teams)
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

  if (teams === null) return <Skeleton lines={3} />
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
              peopleAbove={false}
              people={people}
              isOwner={isOwner}
              onAct={act}
              onOpenBoard={onOpenBoard}
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

      <ArchivedTeams teams={archived} onAct={act} />
    </div>
  )
}

/**
 * Убранные подразделения — и дорога назад.
 *
 * Раздел показывается только когда в архиве что-то есть: пустой он
 * рассказывал бы про архив тем, кто ни разу ничего не убирал.
 *
 * Внутри чего лежал узел — часть выбора, а не украшение: «Ядро» без
 * ответа на «чьё ядро» из архива не выбрать. Узел, старший которого
 * тоже убран, вернуть нельзя, и кнопки у него нет: держать кнопку,
 * которая заведомо ответит отказом, — это отказ, отложенный до нажатия.
 */
function ArchivedTeams({
  teams,
  onAct,
}: {
  teams: ArchivedTeam[]
  onAct: (p: Promise<unknown>) => void
}) {
  if (teams.length === 0) return null
  return (
    <section className="stack">
      <h2 className="section-title">Убранные подразделения</h2>
      <ul className="member-list">
        {teams.map((t) => (
          <li key={t.id}>
            <div className="member-who">
              <span>{t.name}</span>
              <span className="muted small">
                {t.parentName ? `Подразделение «${t.parentName}»` : 'Корневое подразделение'}
              </span>
            </div>
            {t.parentArchived ? (
              <span className="muted small">сперва верните «{t.parentName}»</span>
            ) : (
              <button
                className="link"
                aria-label={`Вернуть из архива: ${t.name}`}
                onClick={() => onAct(api.restoreTeam(t.id))}
              >
                Вернуть
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

function TeamNode({
  node,
  tree,
  // peopleAbove — есть ли хоть один человек в подразделениях над этим.
  // Считается спуском, а не по наличию родителя: родитель бывает
  // и пустым, и тогда «их видят те, кто выше» — неправда.
  peopleAbove,
  people,
  isOwner,
  onAct,
  onOpenBoard,
}: {
  node: TreeNode
  tree: TreeNode[]
  peopleAbove: boolean
  people: Member[]
  isOwner: boolean
  onAct: (p: Promise<unknown>) => void
  onOpenBoard: (boardId: string) => void
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

        {/* Действия узла прижаты к правому краю: имя подразделения
            и счётчики у каждой строки своей длины, и действия, стоящие
            за ними встык, едут по горизонтали от строки к строке —
            глазом столбец не пройти. */}
        {isOwner && (
          <div className="row row--tight tree-actions">
            {canNestInside(node) && (
              <NewTeam parent={node.id} label="+ отдел" onCreate={onAct} />
            )}
            <button
              className="link"
              onClick={() => {
                // Узел из каталога переименовать можно, и это не ошибка:
                // часть провайдеров имена групп не шлёт вовсе. Но там,
                // где шлёт, введённое здесь вернётся к тому, что
                // в каталоге, — и сказано это до ввода, а не после.
                const name = window.prompt(
                  node.fromDirectory
                    ? 'Новое название. Имя этому подразделению даёт каталог: ' +
                        'при следующей синхронизации оно вернётся'
                    : 'Новое название',
                  node.name,
                )
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
            {/* «Убрать» на этом экране значило два разных действия:
                убрать подразделение и вывести человека из его состава,
                и стояли они рядом в одной раскрытой ветке. Имя
                называет объект. */}
            <button
              className="link"
              aria-label={`Убрать подразделение «${node.name}»`}
              onClick={() => onAct(api.archiveTeam(node.id))}
            >
              Убрать подразделение
            </button>
          </div>
        )}
      </div>

      {open && (
        <div className="tree-body stack">
          <TeamMembers
            teamId={node.id}
            fromDirectory={node.fromDirectory}
            boards={node.boards}
            peopleAbove={peopleAbove}
            people={people}
            isOwner={isOwner}
            onAct={onAct}
          />
          {/* Чем занято подразделение — второй вопрос к узлу после
              «кто здесь», и до сих пор раскрытие на него не отвечало,
              хотя число досок в строке узла стояло с самого начала. */}
          <h3 className="section-title">Доски</h3>
          <TeamBoards teamId={node.id} onOpen={onOpenBoard} />
        </div>
      )}

      {node.children.length > 0 && (
        <ul className="tree">
          {node.children.map((child) => (
            <TeamNode
              key={child.id}
              node={child}
              tree={tree}
              peopleAbove={peopleAbove || node.members > 0}
              people={people}
              isOwner={isOwner}
              onAct={onAct}
              onOpenBoard={onOpenBoard}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

function TeamMembers({
  teamId,
  fromDirectory,
  boards,
  peopleAbove,
  people,
  isOwner,
  onAct,
}: {
  teamId: string
  fromDirectory: boolean
  boards: number
  peopleAbove: boolean
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
    <div className="stack">
      <h3 className="section-title">Состав</h3>
      {/* Состав узла из каталога ведёт каталог: провайдеры шлют полную
          замену, и вписанный руками исчезает при следующей
          синхронизации. Выбирает это не наша сторона — единственное,
          что мы можем не делать, это молчать. Раньше молчали: человек
          вписывал участника, действие отвечало «готово», и участник
          пропадал без объяснения. */}
      {fromDirectory && (
        <p className="muted small">
          Подразделение ведёт каталог. Вписанные здесь вручную исчезнут при
          следующей синхронизации — состав меняют в каталоге.
        </p>
      )}
      {members === null ? (
        <Skeleton lines={2} />
      ) : members.length === 0 ? (
        <p className="muted small">
          Никого нет. Участник подразделения работает и во всех отделах под ним,
          а ведущим показывается тот, кто за подразделение отвечает.
          {/* Опустеть подразделение может и само: каталог отключает
              человека, и последний уходит из состава. Доски при этом
              остаются за узлом, а командную доску видит только свой —
              значит, у узла в корне её не видит больше ни один участник.
              Молчать об этом нельзя: доска не пропала и не сломалась,
              она просто перестала быть кому-то видна. */}
          {boards > 0 &&
            (peopleAbove
              ? ' Доски подразделения остались за ним: их видят те, кто состоит выше по дереву.'
              : ' Доски подразделения остались за ним, но рядовому участнику они сейчас' +
                ' не видны: командную доску видит свой, а своих нет ни здесь, ни выше.' +
                ' Доступ остаётся у владельца, у администратора этой области' +
                ' и у наблюдателя за ней.')}
        </p>
      ) : (
        <ul className="member-list">
          {members.map((m) => (
            <li key={m.userId}>
              <div className="member-who">
                <span>{m.name}</span>
                <span className="muted small">{m.email}</span>
              </div>
              {m.lead && <span className="role-chip">Ведущий</span>}
              {isOwner && (
                <button
                  className="link"
                  aria-label={`Убрать из состава: ${m.name}`}
                  onClick={() => act(api.removeTeamMember(teamId, m.userId))}
                >
                  Убрать из состава
                </button>
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
            if (e.target.value) act(api.addTeamMember(teamId, e.target.value))
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

/**
 * Доски подразделения.
 *
 * Спрашиваются при раскрытии узла, а не приезжают вместе с деревом:
 * узлов бывает несколько десятков, раскрывают обычно один, и запрос
 * на каждый узел заранее — это дерево, которое грузится ради того,
 * чего никто не откроет.
 */
function TeamBoards({ teamId, onOpen }: { teamId: string; onOpen: (boardId: string) => void }) {
  const [boards, setBoards] = useState<TeamBoard[] | null>(null)

  useEffect(() => {
    api
      .teamBoards(teamId)
      .then((r) => setBoards(r.boards))
      .catch(() => setBoards([]))
  }, [teamId])

  if (boards === null) return <Skeleton lines={1} />
  if (boards.length === 0) {
    return (
      <p className="muted small">
        Досок нет. Доска попадает сюда, когда у неё выбрано это подразделение, — и
        остаётся, даже если видна всей организации: «чья доска» и «кому видно» разные
        вопросы.
      </p>
    )
  }

  return (
    <ul className="member-list">
      {boards.map((b) => (
        <li key={b.id}>
          <div className="member-who">
            <button className="link" onClick={() => onOpen(b.id)}>
              {b.name}
            </button>
            {/* Ключ и видимость рядом: по ключу доску узнают в переписке,
                а видимость — тот самый вопрос, ради которого в структуру
                и заглядывают. Оба названы словами: «ПОСТ · всей
                организации» — это два обрывка, и первый читается
                опечаткой. */}
            <span className="muted small">
              ключ {b.key} · видна {VISIBILITY_NAMES[b.visibility].toLowerCase()}
            </span>
          </div>
        </li>
      ))}
    </ul>
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
      <button type="submit" aria-label="Завести подразделение" disabled={!name.trim()}>
        Завести
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
                <button
                  className="link"
                  aria-label={`Отозвать наблюдение: ${o.name}`}
                  onClick={() => onAct(api.revokeObservation(o.id))}
                >
                  Отозвать наблюдение
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
                <button
                  className="link"
                  aria-label={`Снять полномочия: ${a.name}`}
                  onClick={() => onAct(api.revokeAdmin(a.id))}
                >
                  Снять полномочия
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
