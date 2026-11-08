import { useEffect, useRef, useState } from 'react'
import type { Task } from '../../shared/api/index.ts'
import type { Vacation } from '../../shared/api/availability.ts'
import { availabilityAPI } from '../../shared/api/availability.ts'
import { t } from '../../shared/i18n/index.ts'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'

export function BulkCoverage({ tasks, vacation, people, onSaved }: { tasks: Task[]; vacation: Vacation; people: Record<string, string>; onSaved: () => void }) {
  const [selected, setSelected] = useState<string[]>([]), [sub, setSub] = useState('')
  const [candidates, setCandidates] = useState<string[]>([]), [error, setError] = useState<string | null>(null), [busy, setBusy] = useState(false)
  const ids = useRef(new Map<string, string>())
  const boards = [...new Set(tasks.filter((t) => selected.includes(t.id)).map((t) => t.boardId))].sort().join(',')
  useEffect(() => {
    let current = true
    setCandidates([])
    if (!boards) return
    Promise.all(boards.split(',').map((b) => availabilityAPI.load(b))).then((lists) => {
      if (current) setCandidates(lists[0].eligible.filter((id) => id !== vacation.userId && lists.every((l) => l.eligible.includes(id)) && !lists[0].vacations.some((v) => v.userId === id && !v.cancelled && v.startsOn <= vacation.endsOn && v.endsOn >= vacation.startsOn)))
    }).catch((e: unknown) => { if (current) setError(e instanceof Error ? e.message : t.common.notDone) })
    return () => { current = false }
  }, [boards, vacation.userId, vacation.startsOn, vacation.endsOn])
  const form = useFormErrors()
  const s = t.availability
  if (!tasks.length) return null
  return <form className="stack" aria-label={s.bulk} noValidate ref={form.ref} onSubmit={async (e) => {
    e.preventDefault(); if (busy) return
    if (!sub || !candidates.includes(sub)) { form.report({ sub: s.selectPerson }); return }
    form.clear()
    setBusy(true); setError(null)
    // Каждая строка — отдельная договорённость. При частичном отказе
    // успешные строки снимаются с выбора, неуспешные сохраняют request ID.
    const failures: string[] = []
    for (const task of tasks.filter((task) => selected.includes(task.id))) {
      const key = `${task.id}:${sub}:${vacation.version}`
      const id = ids.current.get(key) ?? crypto.randomUUID(); ids.current.set(key, id)
      try {
        await availabilityAPI.propose({ id, vacationId: vacation.id, vacationVersion: vacation.version, cardId: task.id, substituteId: sub, startsOn: vacation.startsOn, endsOn: vacation.endsOn })
        setSelected((current) => current.filter((id) => id !== task.id))
      } catch (e) { failures.push(`${task.number}: ${e instanceof Error ? e.message : t.common.notDone}`) }
    }
    setError(failures.length ? failures.join('; ') : null); setBusy(false); onSaved()
  }}>
    <h4>{s.bulk}</h4>
    {tasks.map((task) => <label className="row" key={task.id}><input type="checkbox" disabled={busy} checked={selected.includes(task.id)} onChange={(e) => setSelected((ids) => e.target.checked ? [...ids, task.id] : ids.filter((id) => id !== task.id))} />{task.number} {task.title}</label>)}
    {selected.length > 0 && <><Field label={s.substitute} {...form.field('sub')}>{(bind) => <select {...bind} name="sub" required value={candidates.includes(sub) ? sub : ''} disabled={busy} onChange={(e) => { setSub(e.target.value); setError(null) }}><option value="">{s.selectPerson}</option>{candidates.map((id) => <option key={id} value={id}>{people[id] ?? t.common.someone}</option>)}</select>}</Field>
      <button type="submit" className="btn" disabled={busy}>{s.propose}</button></>}
    <FormError>{error}</FormError>
  </form>
}
