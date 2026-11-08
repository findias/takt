import { BulkCoverage } from './BulkCoverage.tsx'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/index.ts'
import type { Task } from '../../shared/api/index.ts'
import type { Availability, Vacation } from '../../shared/api/availability.ts'
import { coverageLevel } from '../../entities/availability/model.ts'
import { boardPath } from '../../shared/router/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { ScreenError } from '../../shared/ui/Field.tsx'

export function VacationTasks({ vacation, data, people, onSaved }: { vacation: Vacation; data: Availability; people: Record<string,string>; onSaved: () => void }) {
  const [list, setList] = useState<{ tasks: Task[]; truncated: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    let current = true
    request<{ tasks: Task[]; truncated: boolean }>('GET', `/api/tasks?user=${vacation.userId}`)
      .then((value) => { if (current) setList(value) }).catch((e: unknown) => { if (current) setError(e instanceof Error ? e.message : t.common.notDone) })
    return () => { current = false }
  }, [vacation.userId])
  const s = t.availability
  return <section className="stack stack--tight" aria-label={s.tasks}>
    <h3>{s.tasks}</h3><p className="small muted">{s.prepareHint}</p><ScreenError>{error}</ScreenError>
    {list?.tasks.map((task) => <div className="row" key={task.id}><a className="link" href={boardPath(task.boardId, task.id)}>{task.number} {task.title}</a><span className="small">{s[coverageLevel(data, vacation, task.id)]}</span></div>)}
    {list && !list.tasks.length && <p className="muted">{s.noTasks}</p>}
    {list && <BulkCoverage tasks={list.tasks} vacation={vacation} people={people} onSaved={onSaved} />}
    {list?.truncated && <p className="muted">{t.tasks.truncated(list.tasks.length)}</p>}
  </section>
}
