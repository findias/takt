import { useCallback, useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { api } from '../../shared/api/index.ts'
import { AgingChart, CumulativeFlow, CycleScatter } from './charts.tsx'
import { dateWords } from '../../entities/card/model.ts'
import type { FlowReport } from '../../shared/api/index.ts'
import { ScreenError } from '../../shared/ui/Field'
import { t } from '../../shared/i18n/index.ts'
import { Hint } from '../../shared/ui/Hint.tsx'

/**
 * Метрики потока.
 *
 * Показываются проценты, а не средние: у времени цикла всегда длинный
 * хвост, и среднее по нему не отвечает ни на один вопрос. Рядом со всяким
 * числом — на скольких карточках оно посчитано: проценты по трём
 * карточкам не проценты, и делать вид, что проценты, нельзя.
 */
export function Flow({
  boardId,
  sleDays,
  sleProbability,
  iterationsEnabled,
  onClose,
  onPromise,
}: {
  boardId: string
  sleDays: number | null
  sleProbability: number
  /** «Работаем итерациями» (этап 32.4) — настройка того, как команда
   *  работает, рядом с обещанием доски. На полосе над доской её нет:
   *  там на счету каждый орган (e2e «шапка доски не съедает экран»). */
  iterationsEnabled: boolean
  onClose: () => void
  /** Обещание изменилось — доске стоит перечитать себя. */
  onPromise: () => void
}) {
  const [report, setReport] = useState<FlowReport | null>(null)
  const [days, setDays] = useState(90)
  // Без перенесённых — выбор разговора, а не настройка доски: хранить
  // его незачем, открыли поток снова — считаем всё.
  const [withoutImported, setWithoutImported] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .metrics(boardId, days, withoutImported)
      .then(setReport)
      .catch((e) => setError(e instanceof Error ? e.message : t.flow.countFailed))
  }, [boardId, days, withoutImported])

  const [mode, setMode] = usePanelMode()

  useEffect(load, [load])

  // Сколько работы доведено до конца за это время. Прогноз считается
  // ровно из этого, и оговаривать его надо тем же числом, каким
  // оговаривается время цикла.
  const finished = report?.throughput.reduce((sum, week) => sum + week.count, 0) ?? 0

  const days_ = (
    <select
      value={days}
      aria-label={t.flow.period}
      onChange={(e) => setDays(Number(e.target.value))}
    >
      <option value={28}>{t.flow.weeks4}</option>
      <option value={90}>{t.flow.months3}</option>
      <option value={180}>{t.flow.halfYear}</option>
    </select>
  )

  return (
    <Panel
      mode={mode}
      onMode={setMode}
      title={t.flow.title}
      label={t.flow.label}
      onClose={onClose}
      actions={days_}
    >
      <ScreenError>{error}</ScreenError>
      {!report && <p className="muted small">{t.flow.counting}</p>}
      {report && (
        <>

      <Promise
        boardId={boardId}
        days={sleDays}
        probability={sleProbability}
        suggestion={report.cycleTime ? Math.ceil(report.cycleTime.p85) : null}
        onChanged={onPromise}
      />

      <IterationsSwitch boardId={boardId} enabled={iterationsEnabled} onChanged={onPromise} />

      {/* Перенесённое — сказано до цифр, а не после: время цикла ниже
          читается уже с этой оговоркой. Переключатель виден, пока есть
          что отключать, — и после отключения тоже, чтобы вернуться. */}
      {report.imported > 0 && (
        <section className="stack stack--tight">
          <p className="small">{t.flow.importedNote(report.imported)}</p>
          <label>
            <input
              type="checkbox"
              checked={withoutImported}
              onChange={(e) => setWithoutImported(e.target.checked)}
            />
            {t.flow.withoutImported}
          </label>
        </section>
      )}

      <section className="stack">
        <div className="section-head">
          <h3 className="section-title">{t.flow.cycleTime}</h3>
          <Hint topic="cycleTime" />
        </div>
        {report.cycleTime === null ? (
          <p className="muted small">{t.flow.noCycle}</p>
        ) : (
          <>
            <div className="row row--tight">
              <Figure label={t.flow.halfIn} value={t.flow.days(round(report.cycleTime.p50))} />
              <Figure label={t.flow.p85In} value={t.flow.days(round(report.cycleTime.p85))} />
              <Figure label={t.flow.p95In} value={t.flow.days(round(report.cycleTime.p95))} />
            </div>
            <p className="muted small">
              {t.flow.countedBy(report.cycleTime.count, report.cycleTime.count < 10)}
            </p>
            <CycleScatter finished={report.finished} cycleTime={report.cycleTime} />
          </>
        )}
      </section>

      <section className="stack">
        <div className="section-head">
          <h3 className="section-title">{t.flow.now}</h3>
          <Hint topic="age" />
        </div>
        <p className="muted small">{t.flow.wip(report.wip)}</p>
        {report.aging.length === 0 ? (
          <p className="muted small">{t.flow.nothingStarted}</p>
        ) : (
          <>
            {/* Диаграмма перед списком: она отвечает «что застряло»
                одним взглядом, а список — «что именно». */}
            <AgingChart
              aging={report.aging}
              sleDays={sleDays}
              median={report.cycleTime?.p50 ?? null}
            />
          <ul className="member-list">
            {report.aging.map((card) => (
              <li key={card.id}>
                <div className="member-who">
                  <span>
                    {card.blocked && <span aria-hidden="true">⛔ </span>}
                    {card.blocked && <span className="sr-only">{t.flow.blockedSr}</span>}
                    {card.title}
                  </span>
                  <span className="muted small">
                    {card.column}
                    {card.imported && ` · ${t.flow.importedMark}`}
                  </span>
                </div>
                <span className={overdue(card.days, report) ? 'role-chip' : 'muted small'}>
                  {t.flow.days(round(card.days))}
                </span>
              </li>
            ))}
          </ul>
          </>
        )}
      </section>

      {report.flow.length > 1 && (
        <section className="stack">
          <div className="section-head">
            <h3 className="section-title">{t.flow.cfd}</h3>
            <Hint topic="accumulation" />
          </div>
          <CumulativeFlow flow={report.flow} />
        </section>
      )}

      <section className="stack">
        <div className="section-head">
          <h3 className="section-title">{t.flow.throughput}</h3>
          <Hint topic="throughput" />
        </div>
        {/* Пустая сетка столбиков читается как поломка графика, а не как
            «нечего показывать». Пока ни одна карточка не доведена
            до конца, честнее сказать это словами. */}
        {report.throughput.some((w) => w.count > 0) ? (
          <>
            <Bars
              values={report.throughput.map((w) => w.count)}
              // Неделя названа словами: в подсказке столбика стояло
              // «2026-05-18: 0» — машинная запись там, где человек
              // ищет глазами «какая это была неделя».
              labels={report.throughput.map((w) => t.flow.week(dateWords(w.week)))}
            />
            <p className="muted small">
              {t.flow.byWeek}
              {report.discarded > 0 && t.flow.discarded(report.discarded)}
            </p>
          </>
        ) : (
          <p className="muted small">{t.flow.noThroughput}</p>
        )}
      </section>

      {report.forecast && (
        <section className="stack">
          <div className="section-head">
            <h3 className="section-title">{t.flow.forecast}</h3>
            <Hint topic="forecast" />
          </div>
          <table className="figures">
            <thead>
              <tr>
                <th>{t.flow.cards}</th>
                <th>{t.flow.half}</th>
                <th>{t.flow.p85}</th>
                <th>{t.flow.p95}</th>
              </tr>
            </thead>
            <tbody>
              {report.forecast.map((point) => (
                <tr key={point.cards}>
                  <td>{point.cards}</td>
                  <td>{t.flow.days(point.p50)}</td>
                  <td>{t.flow.days(point.p85)}</td>
                  <td>{t.flow.days(point.p95)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted small">
            {t.flow.forecastExplain}
            {/* Оговорка та же, что у времени цикла, и по той же причине:
                прогноз считается из того же прошлого. Без неё «5 карточек
                — 161 день» на трёх доведённых читается как расчёт,
                а это гадание с точностью до дня. */}
            {finished < 10 && t.flow.tooLittle(finished, report.throughput.length)}
          </p>
          </section>
        )}
        </>
      )}
    </Panel>
  )
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="figure">
      <span className="figure-value">{value}</span>
      <span className="muted small">{label}</span>
    </div>
  )
}

/** Столбики без библиотеки: график из десятка значений — это десяток
 *  прямоугольников, и тащить ради них зависимость незачем. */
function Bars({ values, labels }: { values: number[]; labels: string[] }) {
  const top = Math.max(1, ...values)
  return (
    <div
      className="bars"
      role="img"
      // Диктору читаются пары «неделя — сколько»: один ряд чисел
      // без недель не говорит ни о чём, а столбики он не видит.
      aria-label={t.flow.throughputLabel(values.map((value, i) => `${labels[i]} — ${value}`).join(', '))}
    >
      {values.map((value, i) => (
        <div key={labels[i]} className="bar" title={`${labels[i]}: ${value}`}>
          <div className="bar-fill" style={{ height: `${(value / top) * 100}%` }} />
        </div>
      ))}
    </div>
  )
}

/** Карточка старше 85-го процента — та, ради которой этот экран открывают. */
function overdue(days: number, report: FlowReport): boolean {
  return report.cycleTime !== null && days > report.cycleTime.p85
}

function round(value: number): string {
  return value < 10 ? value.toFixed(1) : String(Math.round(value))
}


/**
 * Обещание доски.
 *
 * Kanban Guide требует его как элемент определения потока и говорит, что
 * оно выводится из истории, но однажды посчитанное — становится обещанием
 * и живёт на доске. Поэтому «взять из истории» — кнопка, а не поведение:
 * автоматический пересчёт вернул бы плавающее мерило, из-за которого
 * ухудшения не видно.
 */
function Promise_({
  boardId,
  days,
  probability,
  suggestion,
  onChanged,
}: {
  boardId: string
  days: number | null
  probability: number
  suggestion: number | null
  onChanged: () => void
}) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const save = (next: number | null) => {
    setBusy(true)
    setError(null)
    api
      .setSLE(boardId, next, probability)
      .then(onChanged)
      .catch((e) => setError(e instanceof Error ? e.message : t.common.notDone))
      .finally(() => setBusy(false))
  }

  return (
    <section className="stack">
      <div className="section-head">
        <h3 className="section-title">{t.flow.promise}</h3>
        <Hint topic="promise" />
      </div>
      <ScreenError>{error}</ScreenError>
      {days === null ? (
        <p className="muted small">{t.flow.noPromise}</p>
      ) : (
        <p className="small">
          <strong>{t.flow.promiseIs(probability, days)}</strong>
          {t.flow.promiseExplain}
        </p>
      )}
      {/* Пустой ряд оставлял в панели необъяснимый промежуток: кнопок
          в нём нет, пока обещать нечего и снимать нечего. */}
      {(days !== null || (suggestion !== null && suggestion !== days)) && (
      <div className="row row--tight">
        {suggestion !== null && suggestion !== days && (
          <button disabled={busy} onClick={() => save(suggestion)}>
            {days === null
              ? t.flow.takeFromHistory(suggestion)
              : t.flow.updateFromHistory(suggestion)}
          </button>
        )}
        {days !== null && (
          <button className="link" disabled={busy} onClick={() => save(null)}>
            {t.flow.dropPromise}
          </button>
        )}
      </div>
      )}
      {suggestion === null && days === null && (
        <p className="muted small">{t.flow.nothingToTake}</p>
      )}
    </section>
  )
}

const Promise = Promise_

/** Флажок «Работаем итерациями». Без вопроса: выключение обратимо
 *  и ничего не удаляет — отчёты и состав итераций вернутся с включением. */
function IterationsSwitch({
  boardId,
  enabled,
  onChanged,
}: {
  boardId: string
  enabled: boolean
  onChanged: () => void
}) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  // Флажок меняется сразу, а не после перечитывания доски: иначе он
  // полсекунды показывает прежнее, и кажется, что нажатие не прошло.
  // Отказ возвращает прежнее значение и объясняет причину.
  const [on, setOn] = useState(enabled)
  return (
    <section className="stack stack--tight">
      <h3 className="section-title">{t.flow.howWeWork}</h3>
      <ScreenError>{error}</ScreenError>
      <label className="row row--tight small">
        <input
          type="checkbox"
          checked={on}
          aria-busy={busy || undefined}
          onChange={(e) => {
            const next = e.target.checked
            setOn(next)
            setBusy(true)
            setError(null)
            api
              .setIterations(boardId, next)
              .then(onChanged)
              .catch((err) => {
                setOn(!next)
                setError(err instanceof Error ? err.message : t.common.notDone)
              })
              .finally(() => setBusy(false))
          }}
        />
        <span>{t.flow.iterations}</span>
      </label>
      <p className="muted small">{t.flow.iterationsHint}</p>
    </section>
  )
}
