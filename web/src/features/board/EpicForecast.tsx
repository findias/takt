import { useEffect, useState } from 'react'
import { request } from '../../shared/api/index.ts'
import { dateWords } from '../../entities/card/model.ts'
import { ScopeChart } from '../flow/charts.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Эпик с прогнозом (ROADMAP 42.3, срез 4). К «готово X из Y» —
 * когда будет готово остальное и как рос объём. Прогноз — тем же
 * Монте-Карло, что на «Потоке», по пропускной способности досок
 * команд, где лежат части; три даты, как везде: половина, 85 и 95
 * из ста. Без доведённых частей — «считать не из чего», а не дата:
 * прошлое доски говорит о доске, а не об этом эпике.
 */
type Forecast = {
  total: number
  done: number
  remaining: number
  weeks: number
  finished: number
  boards: number
  forecast: { p50: string; p85: string; p95: string } | null
  scope: { week: string; scope: number; done: number }[]
}

const load = (cardId: string) => request<Forecast>('GET', `/api/cards/${cardId}/forecast`)

export function EpicForecast({ cardId, version }: { cardId: string; version: number }) {
  const [data, setData] = useState<Forecast | null>(null)
  // Перечитывается с версией доски: доделали часть — дата сдвинулась.
  useEffect(() => {
    let current = true
    load(cardId)
      .then((f) => current && setData(f))
      .catch(() => current && setData(null))
    return () => {
      current = false
    }
  }, [cardId, version])
  if (!data || data.total === 0) return null
  const s = t.panel
  return (
    <section className="stack stack--tight epic-forecast">
      <h4 className="small report-name">{s.epicForecast}</h4>
      {data.remaining === 0 ? (
        <p className="muted small">{s.epicAllDone}</p>
      ) : data.forecast ? (
        <>
          <div className="row row--tight">
            <Figure label={s.epicHalf} value={dateWords(data.forecast.p50)} />
            <Figure label={s.epicP85} value={dateWords(data.forecast.p85)} />
            <Figure label={s.epicP95} value={dateWords(data.forecast.p95)} />
          </div>
          <p className="muted small">{s.epicForecastHint(data.remaining, data.finished, data.weeks, data.boards)}</p>
        </>
      ) : (
        <p className="muted small">{data.done === 0 ? s.epicNoDonePart : s.epicNoThroughput(data.weeks)}</p>
      )}
      {data.scope.length > 1 && (
        <ScopeChart
          points={data.scope}
          label={s.epicScopeLabel(data.scope.length, data.total, data.done)}
          caption={s.epicScopeCaption}
        />
      )}
    </section>
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
