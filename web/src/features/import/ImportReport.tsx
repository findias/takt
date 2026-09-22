import type { ImportReport as Report } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Что получится — или получилось. Один вид на оба случая: предпросмотр
 * обязан выглядеть ровно так, как потом будет отчёт, иначе человек
 * сверяет две разные картинки.
 *
 * Потери называются списком, а не числом: «7 без исполнителя» ничего
 * не говорит о том, кого позвать в организацию, а почта — говорит.
 */
export function ImportReport({ report, boardName }: { report: Report; boardName: string }) {
  const s = t.imports
  const hidden = report.skipped.filter((x) => !x.number).length
  return (
    <section className="stack stack--tight import-report" aria-label={report.applied ? undefined : s.preview}>
      <p>
        <strong>{report.applied ? s.created(report.created) : s.willCreate(report.created, report.rows)}</strong>{' '}
        {report.newBoard && !report.applied ? s.toNewBoard(boardName) : s.toBoard(report.boardName)}
      </p>
      {report.parts + report.links + report.comments > 0 && (
        <p>{s.relations(report.parts, report.links, report.comments)}</p>
      )}

      {report.newColumns.length > 0 && (
        <div>
          <h4>{report.newBoard ? s.columns : s.newColumns}</h4>
          <ul className="import-list">
            {report.newColumns.map((c) => (
              <li key={c.name}>
                {c.name} <span className="muted">· {s.kinds[c.kind]}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {report.newLabels.length > 0 && (
        <p>
          <span className="muted">{s.labels}:</span> {report.newLabels.join(', ')}
        </p>
      )}
      {report.archivedLabels.length > 0 && (
        <p>
          <span className="muted">{s.archivedLabels}:</span> {report.archivedLabels.join(', ')}
        </p>
      )}

      {report.missingPeople.length > 0 && (
        <div>
          <h4>{s.missing}</h4>
          <p className="muted small">{s.missingHint}</p>
          <ul className="import-list">
            {report.missingPeople.map((p) => (
              <li key={p.email || p.name}>
                {p.email ? s.missingPerson(p.email, p.cards) : s.noEmail(p.name ?? '', p.cards)}
                {p.label && p.label !== p.email && s.personLabel(p.label)}
              </li>
            ))}
          </ul>
        </div>
      )}

      {report.assignedLater > 0 && <p>{s.assignedLater(report.assignedLater)}</p>}
      {(report.unlabeled ?? 0) > 0 && <p>{s.unlabeled(report.unlabeled ?? 0)}</p>}
      {report.skipped.length > 0 && (
        <p>
          {s.skipped(report.skipped.length)}
          {hidden > 0 && ` ${hidden} — ${s.skippedHidden}.`}
        </p>
      )}

      {/* Что источник знает, а мы не везём, — списком до переноса:
          молчаливая потеря хуже названной. */}
      {report.lost.length > 0 && (
        <div>
          <h4>{s.lost}</h4>
          <ul className="import-list">
            {report.lost.map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
        </div>
      )}

      {report.problems.length > 0 && (
        <div>
          <h4>{s.problems}</h4>
          <p className="muted small">{s.problemsHint}</p>
          <ul className="import-list">
            {report.problems.map((p, i) => (
              <li key={i}>
                <span className="muted">{s.row(p.row)}:</span> {p.message}
              </li>
            ))}
          </ul>
        </div>
      )}

      {report.dates.length > 0 && (
        <p className="small">
          <span className="muted">{s.dates}:</span>{' '}
          {report.dates.map((d) => s.dateOf(d.header, s.dateFormats[d.format])).join('; ')}
        </p>
      )}
      <p className="muted small">{s.history}</p>
    </section>
  )
}
