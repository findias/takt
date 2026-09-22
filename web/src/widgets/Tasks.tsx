import { useEffect, useState } from 'react'
import { api } from '../shared/api/index.ts'
import type { Member, Principal, Task } from '../shared/api/index.ts'
import { chipClass, labelTitle } from '../entities/label/model.ts'
import { PRIORITY_NAMES, dateWords } from '../entities/card/model.ts'
import { boardPath, navigate, setQuery, useQuery } from '../shared/router/index.ts'
import { ScreenError } from '../shared/ui/Field.tsx'
import { Skeleton } from '../shared/ui/states.tsx'
import { t } from '../shared/i18n/index.ts'

const DAY = 24 * 60 * 60 * 1000

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
  const s = t.tasks

  const [people, setPeople] = useState<Member[]>([])
  const [list, setList] = useState<{ tasks: Task[]; truncated: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)

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
    api
      .tasks(user === principal.id ? undefined : user, withDone)
      .then((r) => current && setList(r))
      .catch((e) => current && setError(e instanceof Error ? e.message : s.loadFailed))
    return () => {
      current = false
    }
  }, [user, withDone, principal.id, s])

  const set = (key: string, value: string | null) => {
    const next = new URLSearchParams(query)
    if (value === null) next.delete(key)
    else next.set(key, value)
    setQuery(next, { replace: true })
  }
  const who = user === principal.id ? principal.name : (people.find((p) => p.userId === user)?.name ?? '')
  const now = Date.now()

  return (
    <div className="stack">
      <div className="row tasks-head">
        <label className="row row--tight">
          <span className="small">{s.whose}</span>
          <select value={user} onChange={(e) => set('user', e.target.value === principal.id ? null : e.target.value)}>
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
        {list && <span className="muted small">{s.count(list.tasks.length)}</span>}
      </div>
      <p className="muted small">{s.hint}</p>
      <ScreenError>{error}</ScreenError>
      {!list && !error && <Skeleton lines={4} />}
      {list && list.tasks.length === 0 && <p className="muted">{s.empty}</p>}
      {list && list.tasks.length > 0 && (
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
              {list.tasks.map((task) => (
                <tr key={task.id}>
                  <td className="mono muted small">{task.number}</td>
                  <td>
                    <button className="link table-title" onClick={() => navigate(boardPath(task.boardId, task.id))}>
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
    </div>
  )
}
