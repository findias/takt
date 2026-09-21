import { useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { api } from '../../shared/api/index.ts'
import { UNIT_SHORT, rangeWords } from '../../entities/card/model.ts'
import type { EstimateUnit, Iteration, IterationReport as Report } from '../../shared/api/index.ts'
import { ScreenError } from '../../shared/ui/Field'
import { locale, t } from '../../shared/i18n/index.ts'

/**
 * Отчёт по итерации.
 *
 * То, ради чего вхождение карточки в итерацию моделируется интервалом,
 * а не полем: поле отвечает «где карточка сейчас», а спрашивают всегда
 * другое — что было в спринте на момент его закрытия, что прилетело
 * после начала, что выкинули по дороге. Ответ копился в базе с миграции
 * 0015 и до сих пор не читался ниоткуда.
 *
 * Открытая итерация тоже показывается — на «сейчас». Разница видна
 * в подписи: у закрытой момент неподвижен, у открытой поедет.
 */
export function IterationReport({
  boardId,
  iteration,
  unit,
  onOpenCard,
  onClose,
}: {
  boardId: string
  iteration: Iteration
  unit: EstimateUnit
  onOpenCard: (cardId: string) => void
  onClose: () => void
}) {
  const [report, setReport] = useState<Report | null>(null)
  const [failed, setFailed] = useState(false)
  const [mode, setMode] = usePanelMode()

  useEffect(() => {
    let alive = true
    api
      .iterationReport(boardId, iteration.id)
      .then((r) => alive && setReport(r))
      .catch(() => alive && setFailed(true))
    return () => {
      alive = false
    }
  }, [boardId, iteration.id])

  const closed = report?.iteration.closedAt ?? iteration.closedAt

  return (
    <Panel
      mode={mode}
      onMode={setMode}
      title={iteration.name}
      label={t.flowReport.reportOf(iteration.name)}
      onClose={onClose}
    >
      <ScreenError>{failed ? t.flowReport.reportFailed : undefined}</ScreenError>
      {!report && !failed && <p className="muted small">{t.flowReport.counting}</p>}
      {report && (
        <>
          <section className="stack">
            <p className="muted small">
              {rangeWords(iteration.startsOn, iteration.endsOn)}
              {closed
                ? t.flowReport.closedOn(dateText(closed))
                : t.flowReport.running}
            </p>
            {iteration.goal && <p>{iteration.goal}</p>}
          </section>

          <section className="stack">
            <h3 className="section-title">{t.flowReport.scope}</h3>
            {report.totals.committed === 0 ? (
              <p className="muted small">
                {t.flowReport.noCards} {closed ? t.flowReport.closedEmpty : t.flowReport.stillEmpty}
              </p>
            ) : (
              <>
                <div className="row row--tight">
                  <Figure
                    label={t.flowReport.doneLabel}
                    value={t.flowReport.ofTotal(report.totals.done, report.totals.committed)}
                  />
                  {/* Вес показывается только когда оценены все: сумма без
                      неоценённых врёт в меньшую сторону, и подпись
                      «12 из 20» скрывала бы это молча. */}
                  {report.totals.byWeight && (
                    <Figure
                      label={t.flowReport.doneWeight(UNIT_SHORT[unit])}
                      value={t.flowReport.ofTotal(num(report.totals.doneWeight), num(report.totals.committedWeight))}
                    />
                  )}
                  {report.totals.lateAdded > 0 && (
                    <Figure label={t.flowReport.lateAdded} value={String(report.totals.lateAdded)} />
                  )}
                  {report.totals.dropped > 0 && (
                    <Figure label={t.flowReport.dropped} value={String(report.totals.dropped)} />
                  )}
                </div>
                {!report.totals.byWeight && (
                  <p className="muted small">{t.flowReport.unestimated}</p>
                )}
              </>
            )}
          </section>

          <section className="stack">
            <ul className="member-list">
              {report.cards.map((c) => (
                <li key={c.id}>
                  <div className="member-who">
                    <button className="link related-open" onClick={() => onOpenCard(c.id)}>
                      {c.done && <span aria-hidden="true">✓ </span>}
                      {c.done && <span className="sr-only">{t.flowReport.doneSr}</span>}
                      {c.number} · {c.title}
                    </button>
                    <span className="muted small">{marks(c, unit) || ' '}</span>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        </>
      )}
    </Panel>
  )
}

/** Подписи под карточкой отчёта. Порядок от важного к мелкому:
 *  выбыла — важнее того, когда пришла, а вес важнее обоих только тогда,
 *  когда он есть. */
function marks(c: Report['cards'][number], unit: EstimateUnit): string {
  const out: string[] = []
  if (c.dropped) out.push(t.flowReport.cardDropped)
  if (c.lateAdd) out.push(t.flowReport.cardLate)
  if (c.archived) out.push(t.flowReport.cardArchived)
  if (c.estimate !== null) out.push(`${num(c.estimate)} ${UNIT_SHORT[unit]}`)
  return out.join(' · ')
}

function num(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(2)))
}

function dateText(iso: string): string {
  return new Date(iso).toLocaleDateString(locale(), { day: 'numeric', month: 'long' })
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="figure">
      <span className="figure-value">{value}</span>
      <span className="muted small">{label}</span>
    </div>
  )
}
