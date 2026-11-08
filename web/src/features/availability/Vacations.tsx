import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Member, Principal, Team } from '../../shared/api/index.ts'
import { api } from '../../shared/api/index.ts'
import type { Vacation } from '../../shared/api/availability.ts'
import { availabilityAPI } from '../../shared/api/availability.ts'
import { overlaps, shiftDay } from '../../entities/availability/model.ts'
import { useQuery, setQuery } from '../../shared/router/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { ScreenError, Field, useFormErrors } from '../../shared/ui/Field.tsx'
import { useAvailability } from './useAvailability.ts'
import { VacationForm } from './VacationForm.tsx'
import { VacationTasks } from './VacationTasks.tsx'
import { CoverageList } from './CoverageList.tsx'
import { PickList } from '../../shared/ui/PickList.tsx'
import { buildTree, managedIds } from '../../entities/team/model.ts'

export function Vacations({ principal }: { principal: Principal }) {
  const { data, error, refresh } = useAvailability()
  const [people, setPeople] = useState<Member[]>([])
  const [teams, setTeams] = useState<Team[]>([])
  const [members, setMembers] = useState<Set<string> | null>(null)
  const [failure, setFailure] = useState<string | null>(null)
  const [edit, setEdit] = useState<Vacation | null>(null)
  const [prepare, setPrepare] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const query = useQuery(), chosenTeams = [...new Set(query.getAll('vacationTeam').filter(Boolean))], mine = query.get('mine') === '1'
  const teamKey = chosenTeams.slice().sort().join(',')
  const month = /^\d{4}-(0[1-9]|1[0-2])$/.test(query.get('month') ?? '') && query.get('month')! >= '0001-01' && query.get('month')! <= '9998-12' ? query.get('month')! : (data?.today ?? new Date().toISOString()).slice(0, 7)
  const start = `${month}-01`, next = new Date(`${start}T00:00:00Z`)
  next.setUTCMonth(next.getUTCMonth() + 1)
  const days = Math.round((next.getTime() - Date.parse(`${start}T00:00:00Z`)) / 86400000), end = shiftDay(start, days - 1)
  const set = (key: string, value: string | null) => { const q = new URLSearchParams(query); if (value) q.set(key, value); else q.delete(key); setQuery(q) }
  const s = t.availability
  const calendar = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el=calendar.current
    if (!el || !data || data.today < start || data.today > end) return
    const target=el.querySelector<HTMLElement>(`[data-day="${data.today}"]`)
    const name=el.querySelector<HTMLElement>('th')
    if(target&&name) el.scrollLeft += target.getBoundingClientRect().left-el.getBoundingClientRect().left-name.getBoundingClientRect().width
  }, [start,end,data?.today])
  useEffect(() => {
    let current = true
    Promise.all([api.team(), api.listTeams()]).then(([a, b]) => { if (current) { setPeople(a.members.filter((p) => p.kind === 'person')); setTeams(b.teams) } })
      .catch((e: unknown) => { if (current) setFailure(e instanceof Error ? e.message : t.common.notDone) })
    return () => { current = false }
  }, [])
  useEffect(() => {
    let current = true
    if (!teamKey) { setMembers(null); return }
    setMembers(new Set())
    const ids = managedIds(buildTree(teams), teamKey.split(','))
    Promise.all([...ids].map((id) => api.teamMembers(id))).then((lists) => { if (current) setMembers(new Set(lists.flatMap((r) => r.members.map((p) => p.userId)))) })
      .catch((e: unknown) => { if (current) setFailure(e instanceof Error ? e.message : t.common.notDone) })
    return () => { current = false }
  }, [teamKey, teams])
  const act = async (v: Vacation) => {
    if (busy) return
    setBusy(true); setFailure(null)
    try { await availabilityAPI.save(v); refresh() } catch (e) { setFailure(e instanceof Error ? e.message : t.common.notDone); refresh() } finally { setBusy(false) }
  }
  const names = Object.fromEntries(people.map((p) => [p.userId, p.name]))
  const visiblePeople = people.filter((p) => (!mine || p.userId === principal.id) && (!members || members.has(p.userId)))
  const own = data?.vacations.filter((v) => v.userId === principal.id && (v.endsOn >= data.today || v.id === prepare)) ?? []
  const selected = data?.vacations.find((v) => v.id === prepare)
  return <section className="stack vacations-screen" aria-label={s.title}>
    <div className="row"><h2>{s.title}</h2><button type="button" className="btn" disabled={!data} onClick={() => setEdit({ id: crypto.randomUUID(), userId: principal.id, startsOn: data!.today, endsOn: data!.today, note: '', cancelled: false, version: 0 })}>{s.add}</button></div>
    <ScreenError>{failure || error}</ScreenError>
    {error && <button type="button" className="btn" onClick={refresh}>{s.retry}</button>}
    {data && <p className="small muted">{s.zone}: {data.timeZone}. {s.zoneHint}</p>}
    {edit && <VacationForm key={edit.id} value={edit} onSaved={() => { setPrepare(edit.id); setEdit(null); refresh() }} onClose={() => setEdit(null)} />}
    <div className="row vacation-tools">
      <Field label={s.month}>{(bind) => <input {...bind} type="month" min="0001-01" max="9998-12" value={month} onChange={(e) => { if (e.target.value) set('month', e.target.value) }} />}</Field>
      <PickList label={s.teams} anyText={s.allTeams} addText={s.addTeam}
        options={teams.map((team) => ({ id: team.id, name: `${'　'.repeat(Math.max(0, team.depth - 1))}${team.name}` }))}
        chosen={chosenTeams} removeLabel={s.removeTeam} onChange={(ids) => {
          const next = new URLSearchParams(query)
          next.delete('vacationTeam')
          for (const id of ids) next.append('vacationTeam', id)
          setQuery(next)
        }} />
      {chosenTeams.length > 0 && <button type="button" className="link" onClick={() => set('vacationTeam', null)}>{s.allTeams}</button>}
      <label className="row"><input type="checkbox" checked={mine} onChange={(e) => set('mine', e.target.checked ? '1' : null)} />{s.mine}</label>
    </div>
    <p className="small muted">{s.teamsHint}</p>
    {data && <div ref={calendar} className="vacation-calendar" role="region" aria-label={`${s.title}: ${month}`} tabIndex={0}>
      <table><thead><tr><th scope="col">{s.people}</th>{Array.from({ length: days }, (_, i) => <th scope="col" key={i} data-day={shiftDay(start,i)}>{i + 1}</th>)}</tr></thead>
        <tbody>{visiblePeople.map((p) => {
          const vacations = data.vacations.filter((v) => v.userId === p.userId && !v.cancelled && overlaps(v.startsOn, v.endsOn, start, end))
          return <tr key={p.userId}><th scope="row">{p.name}</th>{Array.from({ length: days }, (_, i) => {
            const date = shiftDay(start, i), v = vacations.find((v) => v.startsOn <= date && v.endsOn >= date)
            return <td key={date} className={`${date === data.today ? 'vacation-today' : ''}${v ? ' vacation-day' : ''}`}>
              {v && <button type="button" className="vacation-cell" aria-label={s.away(p.name, v.startsOn, v.endsOn)} title={s.away(p.name, v.startsOn, v.endsOn)} onClick={() => setPrepare(v.id)}><span aria-hidden="true">{i === 0 || date === v.startsOn ? '●' : '—'}</span></button>}
            </td>
          })}</tr>
        })}</tbody></table>
    </div>}
    {data && !data.vacations.some((v) => !v.cancelled && visiblePeople.some((p) => p.userId === v.userId) && overlaps(v.startsOn, v.endsOn, start, end)) && <p className="muted">{s.empty}</p>}
    <h3>{s.mine}</h3>
    {own.map((v) => <article key={v.id} className="vacation-entry stack stack--tight">
      <div className="row"><strong>{s.range(v.startsOn, v.endsOn)}</strong>{v.cancelled && <span>{s.cancelled}</span>}</div>
      {v.note && <p>{v.note}</p>}
      <div className="row"><button type="button" className="link" disabled={busy} onClick={() => setEdit(v)}>{s.editAction}</button>
        <button type="button" className="link" disabled={busy} onClick={() => void act({ ...v, cancelled: !v.cancelled })}>{v.cancelled ? s.restore : s.cancel}</button>
        {!v.cancelled && <button type="button" className="link" onClick={() => setPrepare(v.id)}>{s.preparing}</button>}</div>
    </article>)}
    {selected && data && <section className="stack vacation-entry" aria-label={s.preparing}>
      <h3>{s.away(names[selected.userId] ?? t.common.someone, selected.startsOn, selected.endsOn)}</h3>
      {selected.note && <p>{selected.note}</p>}
      {!selected.cancelled && <VacationTasks key={selected.id} vacation={selected} data={data} people={names} onSaved={refresh} />}
      <CoverageList entries={data.coverages.filter((c) => c.vacationId === selected.id)} people={names} meId={principal.id} today={data.today} onChange={refresh} links />
    </section>}
    {data && <section className="stack" aria-label={s.futureCover}><h3>{s.futureCover}</h3>
      <CoverageList entries={data.coverages.filter((c) => (c.userId === principal.id || c.substituteId === principal.id) && c.endsOn >= data.today)} people={names} meId={principal.id} today={data.today} onChange={refresh} links />
    </section>}
    {principal.role === 'owner' && data && <TimeZone key={data.timeZone} value={data.timeZone} onSaved={refresh} />}
  </section>
}

function TimeZone({ value, onSaved }: { value: string; onSaved: () => void }) {
  const [zone, setZone] = useState(value), [error, setError] = useState<string | null>(null), [busy, setBusy] = useState(false)
  const form = useFormErrors()
  return <form className="stack" noValidate ref={form.ref} onSubmit={async (e) => { e.preventDefault(); if (busy) return; const found=form.check(e.currentTarget); if(Object.keys(found).length){form.report(found);return}; form.clear(); setBusy(true); setError(null); try { await availabilityAPI.zone(zone); onSaved() } catch (e) { setError(e instanceof Error ? e.message : t.common.notDone) } finally { setBusy(false) } }}>
    <Field label={t.availability.zone} {...form.field('zone')} error={error ?? form.field('zone').error}>{(bind) => <input {...bind} name="zone" value={zone} required disabled={busy} onChange={(e) => { setZone(e.target.value); setError(null) }} />}</Field>
    <button type="submit" className="btn btn--quiet" disabled={busy}>{t.availability.zoneSave}</button>
  </form>
}
