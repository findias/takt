import { useEffect, useState } from 'react'
import { api, request } from '../shared/api/index.ts'
import type { Member, Principal, Task } from '../shared/api/index.ts'
import { chipClass, labelTitle } from '../entities/label/model.ts'
import { PRIORITY_NAMES, dateWords } from '../entities/card/model.ts'
import { boardPath, navigate, setQuery, useQuery } from '../shared/router/index.ts'
import { ScreenError } from '../shared/ui/Field.tsx'
import { Skeleton } from '../shared/ui/states.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { Board } from './Board.tsx'
import { t } from '../shared/i18n/index.ts'
import { Hint } from '../shared/ui/Hint.tsx'
import { DUES, STATUSES, filterTasks, isFiltered, labelsOf, parseTaskFilter } from './tasksFilter.ts'

const DAY = 24 * 60 * 60 * 1000

// Вызов — здесь, а не в общем клиенте `api`: общий клиент едет в первую
// загрузку каждого экрана, а этот экран грузится отдельно (порог размера
// первой загрузки — web/e2e/perf.spec.ts).
const loadTasks = (user?: string, withDone?: boolean) =>
  request<{ tasks: Task[]; truncated: boolean }>(
    'GET',
    '/api/tasks?' +
      new URLSearchParams({ ...(user ? { user } : {}), ...(withDone ? { done: '1' } : {}) }).toString(),
  )

/**
 * Задачи человека со всех досок (решение владельца 22.09.2026: «вывод
 * задач по пользователю с разных досок»).
 *
 * Чьи задачи и показывать ли законченные — в адресе: ссылку «вот что
 * на Иване» пересылают, и открыться она должна тем же. По умолчанию —
 * свои. Таблица, а не доска: у задач разных досок разные колонки,
 * и общей раскладки у них нет.
 */
export function Tasks({ principal }: { principal: Principal }) {
  const query = useQuery()
  const user = query.get('user') ?? principal.id
  const withDone = query.get('done') === '1'
  const filter = parseTaskFilter(query)
  const s = t.tasks

  const [people, setPeople] = useState<Member[]>([])
  const [list, setList] = useState<{ tasks: Task[]; truncated: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Перечитать список после правок в панели: название, колонка, метки
  // в таблице должны быть теми, что человек только что поставил.
  const [reloadKey, setReloadKey] = useState(0)
  // Открытая задача — в адресе, как на доске: «доска:карточка».
  const [openBoard, openCard] = (query.get('open') ?? '').split(':')
  // Доска открытой задачи остаётся подключённой и после закрытия панели:
  // правка, сделанная перед самым закрытием, подтверждается уже после
  // него, и список должен узнать об этом — а узнаёт он от доски.
  const [panelBoard, setPanelBoard] = useState<string | null>(null)
  useEffect(() => {
    if (openBoard) setPanelBoard(openBoard)
  }, [openBoard])

  useEffect(() => {
    api
      .team()
      .then((r) => setPeople(r.members.filter((m) => m.kind === 'person')))
      .catch(() => setPeople([]))
  }, [])

  useEffect(() => {
    let current = true
    setList(null)
    setError(null)
    loadTasks(user === principal.id ? undefined : user, withDone)
      .then((r) => current && setList(r))
      .catch((e) => current && setError(e instanceof Error ? e.message : s.loadFailed))
    return () => {
      current = false
    }
  }, [user, withDone, principal.id, s])

  // Правка в открытой сбоку задаче — список перечитывается тихо, без
  // заглушки: таблица под панелью не должна мигать на каждую правку.
  useEffect(() => {
    if (reloadKey === 0) return
    let current = true
    loadTasks(user === principal.id ? undefined : user, withDone)
      .then((r) => current && setList(r))
      .catch(() => {})
    return () => {
      current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- повод перечитать только правка; смену человека и отбора ведёт эффект выше
  }, [reloadKey])

  const set = (key: string, value: string | null, also: Record<string, null> = {}) => {
    const next = new URLSearchParams(query)
    for (const [k, v] of Object.entries({ ...also, [key]: value })) {
      if (v === null || v === '') next.delete(k)
      else next.set(k, v)
    }
    setQuery(next, { replace: true })
  }
  const shown = list ? filterTasks(list.tasks, filter) : []
  const labels = list ? labelsOf(list.tasks) : []
  const who = user === principal.id ? principal.name : (people.find((p) => p.userId === user)?.name ?? '')
  const now = Date.now()

  return (
    <div className="stack tasks-screen">
      <div className="row tasks-head">
        <label className="row row--tight">
          <span className="small">{s.whose}</span>
          <select value={user} onChange={(e) =>
              // Метка отбора — из задач прежнего человека; у другого её может
              // не быть, и список молча показал бы пустоту.
              set('user', e.target.value === principal.id ? null : e.target.value, { label: null })
            }>
            <option value={principal.id}>{s.mine}</option>
            {people
              .filter((p) => p.userId !== principal.id)
              .map((p) => (
                <option key={p.userId} value={p.userId}>
                  {p.name}
                </option>
              ))}
          </select>
        </label>
        <label className="row row--tight small">
          <input type="checkbox" checked={withDone} onChange={(e) => set('done', e.target.checked ? '1' : null)} />
          <span>{s.withDone}</span>
        </label>
      </div>
      <div className="row tasks-head tasks-filter">
        <label className="row row--tight">
          <span className="small">{s.status}</span>
          <select value={filter.status} onChange={(e) => set('status', e.target.value)}>
            <option value="">{s.statusAny}</option>
            {STATUSES.map((v) => (
              <option key={v} value={v}>
                {s.statuses[v]}
              </option>
            ))}
          </select>
        </label>
        <label className="row row--tight">
          <span className="small">{s.due}</span>
          <select value={filter.due} onChange={(e) => set('due', e.target.value)}>
            <option value="">{s.dueAny}</option>
            {DUES.map((v) => (
              <option key={v} value={v}>
                {s.dues[v]}
              </option>
            ))}
          </select>
        </label>
        <label className="row row--tight">
          <span className="small">{s.label}</span>
          <select value={filter.label} onChange={(e) => set('label', e.target.value)}>
            <option value="">{s.labelAny}</option>
            {labels.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
        </label>
        {isFiltered(filter) && (
          <Button kind="quiet" onClick={() => set('status', null, { due: null, label: null })}>
            {s.reset}
          </Button>
        )}
        {list && (
          <span className="muted small">
            {isFiltered(filter) ? s.shownOf(shown.length, list.tasks.length) : s.count(list.tasks.length)}
          </span>
        )}
      </div>
      <p className="muted small">
        {s.hint} <Hint topic="tasks" />
      </p>
      <ScreenError>{error}</ScreenError>
      {!list && !error && <Skeleton lines={4} />}
      {list && list.tasks.length === 0 && <p className="muted">{s.empty}</p>}
      {list && list.tasks.length > 0 && shown.length === 0 && <p className="muted">{s.emptyFiltered}</p>}
      {shown.length > 0 && (
        <div className="table-wrap">
          <table className="board-table">
            <caption className="sr-only">{s.caption(who)}</caption>
            <thead>
              <tr>
                <th scope="col">{s.colNumber}</th>
                <th scope="col">{s.colTask}</th>
                <th scope="col">{s.colBoard}</th>
                <th scope="col">{s.colColumn}</th>
                <th scope="col">{s.colDue}</th>
                <th scope="col">{s.colPriority}</th>
                <th scope="col">{s.colLabels}</th>
                <th scope="col" className="num">
                  {s.colAge}
                </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((task) => (
                <tr key={task.id}>
                  <td className="mono muted small">{task.number}</td>
                  <td>
                    {/* Задача открывается здесь же, сбоку, как на доске
                        (владелец 25.09.2026): уход на доску терял и список,
                        и отбор. На доску ведёт её название в соседнем
                        столбце. */}
                    <button className="link table-title" onClick={() => set('open', `${task.boardId}:${task.id}`)}>
                      {task.title}
                    </button>
                    {task.blocked && <span className="mark mark--alarm tasks-mark">{s.blocked}</span>}
                    {task.outcome && <span className="mark tasks-mark">{s.done}</span>}
                  </td>
                  <td className="small">
                    <button className="link" onClick={() => navigate(boardPath(task.boardId))}>
                      {task.boardName}
                    </button>
                  </td>
                  <td className="muted small">{task.column}</td>
                  <td className="small">{task.dueOn ? dateWords(task.dueOn) : '—'}</td>
                  <td className="small">{PRIORITY_NAMES[task.priority]}</td>
                  <td>
                    {task.labels.map((label) => (
                      <span key={label.id} className={chipClass(label)} title={labelTitle(label)}>
                        {label.name}
                      </span>
                    ))}
                  </td>
                  <td className="num small">
                    {task.startedAt && !task.outcome
                      ? s.days(Math.max(0, Math.floor((now - new Date(task.startedAt).getTime()) / DAY)))
                      : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list?.truncated && <p className="muted small">{s.truncated(list.tasks.length)}</p>}
      {panelBoard && (
        <Board
          key={panelBoard}
          boardId={panelBoard}
          cardId={openBoard === panelBoard && openCard ? openCard : null}
          onCard={(cardId) => {
            set('open', cardId ? `${panelBoard}:${cardId}` : null)
          }}
          onChanged={() => setReloadKey((k) => k + 1)}
          unit={principal.estimateUnit}
          meId={principal.id}
          isOwner={principal.role === 'owner'}
          canEdit={principal.role !== 'viewer'}
          onBack={() => set('open', null)}
          panelOnly
        />
      )}
    </div>
  )
}
