import type { FlowReport } from '../../shared/api/index.ts'

/**
 * Диаграммы потока.
 *
 * Рисуются вручную в SVG, как и столбики пропускной способности рядом:
 * график из полусотни точек — это полсотни кружков, и тащить ради них
 * библиотеку незачем. Заодно график остаётся в теме и в токенах доски,
 * а не приносит свою палитру.
 *
 * Числа на этом экране были и раньше — проценты, возраст, столбики.
 * Диаграммы отвечают на то, чего числа не говорят: где работа копится
 * (накопительная), насколько разбросано время цикла (точечная) и что
 * застряло прямо сейчас (старение). Медиана в двадцать точек по три дня
 * и в три точки по двадцать — одна и та же; разговор на разборе разный.
 */

// Система координат общая у всех трёх: подписи слева, дни снизу.
//
// Ширина близка к ширине боковой панели намеренно. SVG растягивается
// по месту, и вместе с ним растягивается всё внутри: при вдвое более
// широкой системе координат подписи в 9 единиц выходили на экране
// шестью пикселями и не читались.
const W = 360
const H = 150
const PAD = { left: 24, right: 8, top: 10, bottom: 20 }
const PLOT = { w: W - PAD.left - PAD.right, h: H - PAD.top - PAD.bottom }

/**
 * Накопительная диаграмма потока.
 *
 * Три полосы площадью: сделано снизу, работа над ним, очередь сверху.
 * Порядок не произволен — снизу то, что уже не изменится, и тогда
 * толщина каждой полосы читается как «сколько сейчас в этом состоянии»,
 * а наклон нижней границы — как темп.
 *
 * Сервер считает это с шестого этапа, и до сих пор ответ никуда
 * не показывался.
 */
export function CumulativeFlow({ flow }: { flow: FlowReport['flow'] }) {
  if (flow.length < 2) return null

  const top = Math.max(1, ...flow.map((d) => d.queued + d.inProgress + d.done))
  const x = (i: number) => PAD.left + (i / (flow.length - 1)) * PLOT.w
  const y = (value: number) => PAD.top + PLOT.h - (value / top) * PLOT.h

  // Полосы кладутся снизу вверх, каждая поверх суммы предыдущих.
  const bands = [
    { key: 'done', tone: 'var(--accent)', opacity: 0.85, of: (d: FlowReport['flow'][number]) => d.done },
    {
      key: 'progress',
      tone: 'var(--accent)',
      opacity: 0.45,
      of: (d: FlowReport['flow'][number]) => d.done + d.inProgress,
    },
    {
      key: 'queued',
      tone: 'var(--ink-3)',
      opacity: 0.22,
      of: (d: FlowReport['flow'][number]) => d.done + d.inProgress + d.queued,
    },
  ]

  const last = flow[flow.length - 1]
  return (
    <figure className="chart">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Накопительная диаграмма потока за ${flow.length} дней. Сейчас: в очереди ${last.queued}, в работе ${last.inProgress}, сделано ${last.done}.`}
      >
        <Grid top={top} />
        {/* Сверху вниз: каждая следующая полоса перекрывает нижнюю
            своей площадью, поэтому рисуем от самой высокой. */}
        {[...bands].reverse().map((band) => (
          <path
            key={band.key}
            d={area(flow.map((d, i) => [x(i), y(band.of(d))]))}
            fill={band.tone}
            fillOpacity={band.opacity}
          />
        ))}
      </svg>
      <figcaption className="muted small">
        Снизу вверх: сделано, в работе, в очереди. Полоса, растущая вверх без движения
        нижней границы, — это работа, которая копится, а не идёт.
      </figcaption>
    </figure>
  )
}

/**
 * Точечная диаграмма времени цикла: точка — доведённая карточка,
 * по горизонтали день финиша, по вертикали дни в работе.
 *
 * Линии процентилей — те же числа, что стоят над диаграммой. Смысл
 * диаграммы в том, что видно, чем эти числа набраны: ровным облаком
 * или парой выбросов.
 */
export function CycleScatter({
  finished,
  cycleTime,
}: {
  finished: FlowReport['finished']
  cycleTime: NonNullable<FlowReport['cycleTime']>
}) {
  if (finished.length === 0) return null

  const days = finished.map((c) => c.days)
  const top = Math.max(1, ...days, cycleTime.p95)
  const from = new Date(finished[0].finishedOn).getTime()
  const to = new Date(finished[finished.length - 1].finishedOn).getTime()
  const span = Math.max(1, to - from)
  const x = (day: string) => PAD.left + ((new Date(day).getTime() - from) / span) * PLOT.w
  const y = (value: number) => PAD.top + PLOT.h - (value / top) * PLOT.h

  return (
    <figure className="chart">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Время цикла по карточкам: ${finished.length} точек, от ${round(Math.min(...days))} до ${round(Math.max(...days))} дней.`}
      >
        <Grid top={top} />
        {/* Подписи расходятся по вертикали, если сами линии сошлись:
            наложившиеся подписи — это две нечитаемые вместо одной
            читаемой. Двигается подпись, а не линия: линия стоит там,
            где стоит число. */}
        {spread([
          { label: 'половина', y: y(cycleTime.p50) },
          { label: '85 из 100', y: y(cycleTime.p85) },
        ]).map((line) => (
          <g key={line.label}>
            <line
              x1={PAD.left}
              x2={W - PAD.right}
              y1={line.y}
              y2={line.y}
              stroke="var(--accent)"
              strokeDasharray="4 3"
              strokeOpacity={0.6}
            />
            <text x={W - PAD.right} y={line.labelY} className="chart-note" textAnchor="end">
              {line.label}
            </text>
          </g>
        ))}
        {finished.map((c) => (
          <circle
            key={c.id}
            cx={x(c.finishedOn)}
            cy={y(c.days)}
            r={3}
            fill={c.days > cycleTime.p85 ? 'var(--warn)' : 'var(--accent)'}
            fillOpacity={0.75}
          >
            <title>{`${c.title}: ${round(c.days)} дн.`}</title>
          </circle>
        ))}
      </svg>
      <figcaption className="muted small">
        Каждая точка — доведённая карточка. Красные прошли дольше 85 из 100: по ним и стоит
        спрашивать, что случилось.
      </figcaption>
    </figure>
  )
}

/**
 * Диаграмма старения: что идёт прямо сейчас и сколько уже идёт.
 *
 * По горизонтали — колонки в порядке доски, по вертикали дни. Обещание
 * доски проведено линией: карточка выше неё — та, ради которой этот
 * экран открывают.
 */
export function AgingChart({
  aging,
  sleDays,
  median,
}: {
  aging: FlowReport['aging']
  sleDays: number | null
  median: number | null
}) {
  if (aging.length === 0) return null

  const columns = [...new Set(aging.map((c) => c.column))]
  const top = Math.max(1, ...aging.map((c) => c.days), sleDays ?? 0)
  const x = (column: string) =>
    PAD.left + ((columns.indexOf(column) + 0.5) / columns.length) * PLOT.w
  const y = (value: number) => PAD.top + PLOT.h - (value / top) * PLOT.h

  return (
    <figure className="chart">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Возраст идущей работы: ${aging.length} карточек, самая старая ${round(Math.max(...aging.map((c) => c.days)))} дней.`}
      >
        <Grid top={top} />
        {median !== null && (
          <line
            x1={PAD.left}
            x2={W - PAD.right}
            y1={y(median)}
            y2={y(median)}
            stroke="var(--rule-strong)"
            strokeDasharray="4 3"
          />
        )}
        {sleDays !== null && (
          <g>
            <line
              x1={PAD.left}
              x2={W - PAD.right}
              y1={y(sleDays)}
              y2={y(sleDays)}
              stroke="var(--warn)"
              strokeDasharray="4 3"
            />
            <text x={W - PAD.right} y={y(sleDays) - 3} className="chart-note" textAnchor="end">
              обещание
            </text>
          </g>
        )}
        {aging.map((c) => (
          <circle
            key={c.id}
            cx={x(c.column)}
            cy={y(c.days)}
            r={4}
            fill={c.blocked ? 'var(--warn)' : 'var(--accent)'}
            fillOpacity={c.blocked ? 0.9 : 0.7}
          >
            <title>{`${c.title}: ${round(c.days)} дн. в «${c.column}»${c.blocked ? ', заблокирована' : ''}`}</title>
          </circle>
        ))}
        {columns.map((column) => (
          <text key={column} x={x(column)} y={H - 5} className="chart-note" textAnchor="middle">
            {column}
          </text>
        ))}
      </svg>
      <figcaption className="muted small">
        Заблокированные красным: они стареют, ничего не делая.
      </figcaption>
    </figure>
  )
}

/** Раздвигает подписи по вертикали, чтобы они не наезжали друг
 *  на друга: сверху вниз, каждая следующая не ближе строки к предыдущей. */
function spread<T extends { y: number }>(lines: T[]): (T & { labelY: number })[] {
  const sorted = [...lines].sort((a, b) => a.y - b.y)
  let previous = -Infinity
  return sorted.map((line) => {
    const labelY = Math.max(line.y - 3, previous + 11)
    previous = labelY
    return { ...line, labelY }
  })
}

/** Две линии и подпись верха: без них у высоты нет масштаба, и точка
 *  «высоко» ничего не значит. */
function Grid({ top }: { top: number }) {
  return (
    <g>
      {[0, 0.5, 1].map((share) => (
        <line
          key={share}
          x1={PAD.left}
          x2={W - PAD.right}
          y1={PAD.top + PLOT.h * (1 - share)}
          y2={PAD.top + PLOT.h * (1 - share)}
          stroke="var(--rule)"
        />
      ))}
      <text x={PAD.left - 5} y={PAD.top + 4} className="chart-note" textAnchor="end">
        {Math.round(top)}
      </text>
      <text x={PAD.left - 5} y={PAD.top + PLOT.h} className="chart-note" textAnchor="end">
        0
      </text>
    </g>
  )
}

/** Замкнутая площадь под ломаной: от первой точки по верху и обратно
 *  по нулевой линии. */
function area(points: [number, number][]): string {
  const line = points.map(([px, py], i) => `${i === 0 ? 'M' : 'L'}${px} ${py}`).join(' ')
  const base = PAD.top + PLOT.h
  return `${line} L${points[points.length - 1][0]} ${base} L${points[0][0]} ${base} Z`
}

function round(value: number): string {
  return value < 10 ? value.toFixed(1) : String(Math.round(value))
}
