import { useCallback, useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { api } from '../../shared/api/index.ts'
import { AgingChart, CumulativeFlow, CycleScatter } from './charts.tsx'
import { dateWords } from '../../entities/card/model.ts'
import { plural } from '../../shared/lib/plural.ts'
import type { FlowReport } from '../../shared/api/index.ts'

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
  onClose,
  onPromise,
}: {
  boardId: string
  sleDays: number | null
  sleProbability: number
  onClose: () => void
  /** Обещание изменилось — доске стоит перечитать себя. */
  onPromise: () => void
}) {
  const [report, setReport] = useState<FlowReport | null>(null)
  const [days, setDays] = useState(90)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .metrics(boardId, days)
      .then(setReport)
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось посчитать'))
  }, [boardId, days])

  const [mode, setMode] = usePanelMode()

  useEffect(load, [load])

  // Сколько работы доведено до конца за это время. Прогноз считается
  // ровно из этого, и оговаривать его надо тем же числом, каким
  // оговаривается время цикла.
  const finished = report?.throughput.reduce((sum, week) => sum + week.count, 0) ?? 0

  const days_ = (
    <select
      value={days}
      aria-label="За сколько дней"
      onChange={(e) => setDays(Number(e.target.value))}
    >
      <option value={28}>4 недели</option>
      <option value={90}>3 месяца</option>
      <option value={180}>полгода</option>
    </select>
  )

  return (
    <Panel
      mode={mode}
      onMode={setMode}
      title="Поток"
      label="Метрики потока"
      onClose={onClose}
      actions={days_}
    >
      {error && <p className="error">{error}</p>}
      {!report && <p className="muted small">Считаем…</p>}
      {report && (
        <>

      <Promise
        boardId={boardId}
        days={sleDays}
        probability={sleProbability}
        suggestion={report.cycleTime ? Math.ceil(report.cycleTime.p85) : null}
        onChanged={onPromise}
      />

      <section className="stack">
        <h3 className="section-title">Время цикла</h3>
        {report.cycleTime === null ? (
          <p className="muted small">
            Ни одна карточка не доведена до конца за это время. Считать не из чего —
            и любое число здесь было бы выдумкой.
          </p>
        ) : (
          <>
            <div className="row row--tight">
              <Figure label="половина за" value={`${round(report.cycleTime.p50)} дн.`} />
              <Figure label="85 из 100 за" value={`${round(report.cycleTime.p85)} дн.`} />
              <Figure label="95 из 100 за" value={`${round(report.cycleTime.p95)} дн.`} />
            </div>
            <p className="muted small">
              Посчитано по {report.cycleTime.count}{' '}
              {plural(report.cycleTime.count, 'карточке', 'карточкам', 'карточкам')}
              {report.cycleTime.count < 10 && ' — слишком мало, чтобы на это опираться'}.
            </p>
            <CycleScatter finished={report.finished} cycleTime={report.cycleTime} />
          </>
        )}
      </section>

      <section className="stack">
        <h3 className="section-title">Что идёт сейчас</h3>
        <p className="muted small">
          В работе {report.wip}. Возраст важнее времени цикла: время цикла говорит
          о прошлом, возраст — о том, что застряло прямо сейчас.
        </p>
        {report.aging.length === 0 ? (
          <p className="muted small">Ничего не начато.</p>
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
                    {card.blocked && <span className="sr-only">Заблокирована. </span>}
                    {card.title}
                  </span>
                  <span className="muted small">{card.column}</span>
                </div>
                <span className={overdue(card.days, report) ? 'role-chip' : 'muted small'}>
                  {round(card.days)} дн.
                </span>
              </li>
            ))}
          </ul>
          </>
        )}
      </section>

      {report.flow.length > 1 && (
        <section className="stack">
          <h3 className="section-title">Как копится работа</h3>
          <CumulativeFlow flow={report.flow} />
        </section>
      )}

      <section className="stack">
        <h3 className="section-title">Пропускная способность</h3>
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
              labels={report.throughput.map((w) => `неделя ${dateWords(w.week)}`)}
            />
            <p className="muted small">
              По неделям, только доведённое до конца.
              {report.discarded > 0 &&
                ` Ещё ${report.discarded} убрано с доски незавершёнными — в счёт они не идут.`}
            </p>
          </>
        ) : (
          <p className="muted small">
            Пока ничего не доведено до конца — считать нечего. Столбики появятся,
            когда первая карточка дойдёт до колонки, отмеченной финишем.
          </p>
        )}
      </section>

      {report.forecast && (
        <section className="stack">
          <h3 className="section-title">Сколько займёт</h3>
          <table className="figures">
            <thead>
              <tr>
                <th>карточек</th>
                <th>половина</th>
                <th>85 из 100</th>
                <th>95 из 100</th>
              </tr>
            </thead>
            <tbody>
              {report.forecast.map((point) => (
                <tr key={point.cards}>
                  <td>{point.cards}</td>
                  <td>{point.p50} дн.</td>
                  <td>{point.p85} дн.</td>
                  <td>{point.p95} дн.</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted small">
            Прогноз складывает случайные недели из прошлого — тысяча испытаний.
            Он говорит только одно: что будет, если дальше будет как было.
            {/* Оговорка та же, что у времени цикла, и по той же причине:
                прогноз считается из того же прошлого. Без неё «5 карточек
                — 161 день» на трёх доведённых читается как расчёт,
                а это гадание с точностью до дня. */}
            {finished < 10 &&
              ` Пока в прошлом всего ${finished} ${plural(
                finished,
                'карточка',
                'карточки',
                'карточек',
              )} за ${report.throughput.length} ${plural(
                report.throughput.length,
                'неделю',
                'недели',
                'недель',
              )} — слишком мало, чтобы на это опираться.`}
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
      aria-label={`Пропускная способность: ${values
        .map((value, i) => `${labels[i]} — ${value}`)
        .join(', ')}`}
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
      .catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
      .finally(() => setBusy(false))
  }

  return (
    <section className="stack">
      <h3 className="section-title">Обещание доски</h3>
      {error && <p className="error">{error}</p>}
      {days === null ? (
        <p className="muted small">
          Обещания нет. Доска без истории обещать и не может — но как только
          появятся доведённые до конца карточки, обещание стоит назвать:
          с ним сравнивается возраст того, что идёт сейчас.
        </p>
      ) : (
        <p className="small">
          <strong>
            {probability}% работы проходит доску за {days}{' '}
            {plural(days, 'день', 'дня', 'дней')}
          </strong>
          . С этим сроком сравнивается возраст карточек: перешагнувшая его
          получает метку прямо на доске.
        </p>
      )}
      {/* Пустой ряд оставлял в панели необъяснимый промежуток: кнопок
          в нём нет, пока обещать нечего и снимать нечего. */}
      {(days !== null || (suggestion !== null && suggestion !== days)) && (
      <div className="row row--tight">
        {suggestion !== null && suggestion !== days && (
          <button disabled={busy} onClick={() => save(suggestion)}>
            {days === null ? 'Взять из истории' : 'Обновить по истории'}: {suggestion}{' '}
            {plural(suggestion, 'день', 'дня', 'дней')}
          </button>
        )}
        {days !== null && (
          <button className="link" disabled={busy} onClick={() => save(null)}>
            Снять обещание
          </button>
        )}
      </div>
      )}
      {suggestion === null && days === null && (
        <p className="muted small">
          Взять из истории пока нечего: ни одна карточка не доведена до конца.
        </p>
      )}
    </section>
  )
}

const Promise = Promise_
