import { useState } from 'react'
import type { Coverage } from '../../shared/api/availability.ts'
import { availabilityAPI } from '../../shared/api/availability.ts'
import { boardPath } from '../../shared/router/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { FormError } from '../../shared/ui/Field.tsx'

export function CoverageList({ entries, people, meId, today, onChange, links = false }: {
  entries: Coverage[]; people: Record<string, string>; meId: string; today: string; onChange: () => void; links?: boolean
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const s = t.availability
  const act = async (c: Coverage, status: Coverage['status']) => {
    if (busy) return
    setBusy(true); setError(null)
    try { await availabilityAPI.respond(c, status); onChange() }
    catch (e) { setError(e instanceof Error ? e.message : t.common.notDone); onChange() }
    finally { setBusy(false) }
  }
  return <div className="stack stack--tight">
    <FormError>{error}</FormError>
    {entries.map((c) => <article className="coverage-entry stack stack--tight" key={c.id}>
      {links && <a className="link" href={boardPath(c.boardId, c.cardId)}>{c.cardNumber} {c.cardTitle}</a>}
      <span>{s.replaces(people[c.substituteId] ?? t.common.someone, people[c.userId] ?? t.common.someone)}</span>
      <span className="small muted">{s.range(c.startsOn, c.endsOn)} · {c.endsOn < today ? s.expired : c.status === 'cancelled' ? s.stopped : s[c.status]}</span>
      {!c.valid && c.status !== 'cancelled' && c.status !== 'declined' && c.endsOn >= today && <p className="mark mark--alarm">{s.invalid}</p>}
      <div className="row">
        {c.substituteId === meId && c.status === 'proposed' && c.endsOn >= today && <>
          <button type="button" className="btn" disabled={busy || !c.valid} onClick={() => void act(c, 'accepted')}>{s.accept}</button>
          <button type="button" className="btn btn--quiet" disabled={busy} onClick={() => void act(c, 'declined')}>{s.decline}</button>
        </>}
        {(c.canManage || c.substituteId === meId) && (c.status === 'proposed' || c.status === 'accepted') && c.endsOn >= today &&
          <button type="button" className="link" disabled={busy} onClick={() => void act(c, 'cancelled')}>{s.withdraw}</button>}
      </div>
    </article>)}
  </div>
}
