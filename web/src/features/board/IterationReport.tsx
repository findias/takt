import { useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { api } from '../../shared/api/index.ts'
import { UNIT_SHORT, rangeWords } from '../../entities/card/model.ts'
import type { EstimateUnit, Iteration, IterationReport as Report } from '../../shared/api/index.ts'

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
      label={`Отчёт по итерации «${iteration.name}»`}
      onClose={onClose}
    >
      {failed && <p className="error">Не удалось прочитать отчёт.</p>}
      {!report && !failed && <p className="muted small">Считаем…</p>}
      {report && (
        <>
          <section className="stack">
            <p className="muted small">
              {rangeWords(iteration.startsOn, iteration.endsOn)}
              {closed
                ? ` · закрыта ${dateText(closed)}, состав застыл`
                : ' · идёт, посчитано на сейчас'}
            </p>
            {iteration.goal && <p>{iteration.goal}</p>}
          </section>

          <section className="stack">
            <h3 className="section-title">Что было в составе</h3>
            {report.totals.committed === 0 ? (
              <p className="muted small">
                Ни одной карточки. {closed ? 'Так она и закрылась.' : 'Пока пусто.'}
              </p>
            ) : (
              <>
                <div className="row row--tight">
                  <Figure
                    label="сделано"
                    value={`${report.totals.done} из ${report.totals.committed}`}
                  />
                  {/* Вес показывается только когда оценены все: сумма без
                      неоценённых врёт в меньшую сторону, и подпись
                      «12 из 20» скрывала бы это молча. */}
                  {report.totals.byWeight && (
                    <Figure
                      label={`сделано ${UNIT_SHORT[unit]}`}
                      value={`${num(report.totals.doneWeight)} из ${num(report.totals.committedWeight)}`}
                    />
                  )}
                  {report.totals.lateAdded > 0 && (
                    <Figure label="пришло после начала" value={String(report.totals.lateAdded)} />
                  )}
                  {report.totals.dropped > 0 && (
                    <Figure label="убрано по дороге" value={String(report.totals.dropped)} />
                  )}
                </div>
                {!report.totals.byWeight && (
                  <p className="muted small">
                    В составе есть неоценённые карточки — вес не считается: сумма без них
                    показала бы меньше, чем было.
                  </p>
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
                      {c.done && <span className="sr-only">Сделана. </span>}
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
  if (c.dropped) out.push('убрана из итерации')
  if (c.lateAdd) out.push('пришла после начала')
  if (c.archived) out.push('убрана с доски')
  if (c.estimate !== null) out.push(`${num(c.estimate)} ${UNIT_SHORT[unit]}`)
  return out.join(' · ')
}

function num(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(2)))
}

function dateText(iso: string): string {
  return new Date(iso).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="figure">
      <span className="figure-value">{value}</span>
      <span className="muted small">{label}</span>
    </div>
  )
}
