import { useEffect, useState } from 'react'
import { api, request } from '../shared/api/index.ts'
import type { Iteration } from '../shared/api/index.ts'
import { PRIORITY_NAMES } from '../entities/card/model.ts'
import { setQuery, useQuery } from '../shared/router/index.ts'
import { PickList } from '../shared/ui/PickList.tsx'
import { Slices } from '../features/reports/Slices.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { Bars, CumulativeFlow } from '../features/flow/charts.tsx'
import { dateWords } from '../entities/card/model.ts'
import { t } from '../shared/i18n/index.ts'
import {
  PRESETS,
  PRIORITIES,
  STATES,
  addressQuery,
  downloadHref,
  parseReportFilter,
  periodReversed,
  presetPeriod,
  reportQuery,
} from './reportFilter.ts'
import type { Format, ReportFilter } from './reportFilter.ts'

type Option = { id: string; name: string }
type Count = { status: 'counting' } | { status: 'error'; error: string } | { status: 'ready'; total: number; limit: number }
type PersonRow = {
  userId: string | null
  name: string
  cards: number
  finished: number
  wip: number
  cycleTimeP50: number | null
  cycleTimeP85: number | null
  workTimeP50: number | null
}
type People = { candidates: Option[] }
type Percentiles = { p50: number; p85: number }
/** Сводка — та же, что первым листом книги: сервер считает её одним
 *  запросом для экрана и для файла. */
type Summary = {
  cards: number
  finished: number
  discarded: number
  wip: number
  cycleTime: Percentiles | null
  age: Percentiles | null
  throughput: { week: string; count: number }[]
  flowStep: 'day' | 'week'
  flow: { day: string; queued: number; inProgress: number; done: number }[]
  iterations: { name: string; board: string; startsOn: string; endsOn: string; closed: boolean; planned: number; done: number }[]
  teams: { team: string; cards: number; finished: number; wip: number; cycleTimeP50: number | null; cycleTimeP85: number | null }[]
  people: PersonRow[]
}
type SummaryState = { status: 'loading' } | { status: 'error'; error: string } | { status: 'ready'; data: Summary }

// Вызовы — здесь, а не в общем клиенте: экран грузится отдельным куском.
const countCards = (f: ReportFilter) =>
  request<{ total: number; limit: number }>('GET', '/api/reports/cards/count?' + reportQuery(f).toString())
// Люди под отбор (ROADMAP 37.1–37.2): из кого выбирать исполнителей —
// люди выбранных досок и подразделений.
const reportPeople = (f: ReportFilter) => request<People>('GET', '/api/reports/people?' + reportQuery(f).toString())
// Сводка под отбор (ROADMAP 42.3, срез 1): то, что раньше было только
// на первом листе файла, — на экране, с разрезом по людям внутри.
const reportSummary = (f: ReportFilter) => request<Summary>('GET', '/api/reports/summary?' + reportQuery(f).toString())

const FORMATS: Format[] = ['xlsx', 'csv', 'json']

/**
 * Выгрузка для руководства (этап 24): «пришлите табличку» без вечера
 * работы тимлида.
 *
 * Отбор живёт в адресе — ссылку «вот что закрыли за квартал» пересылают,
 * и открыться она должна тем же. Число карточек под отбор видно раньше
 * файла: кнопка заранее знает, что выгрузка не соберётся, и говорит,
 * что сузить.
 */
export function Reports() {
  const query = useQuery()
  const [now] = useState(() => new Date())
  const f = parseReportFilter(query, now)
  const s = t.reports

  const [boards, setBoards] = useState<Option[]>([])
  const [teams, setTeams] = useState<Option[]>([])
  const [people, setPeople] = useState<People | null>(null)
  const [labels, setLabels] = useState<Option[]>([])
  const [iterations, setIterations] = useState<Iteration[]>([])
  const [count, setCount] = useState<Count>({ status: 'counting' })
  const [summary, setSummary] = useState<SummaryState>({ status: 'loading' })

  // Списки для выбора — один раз: они нужны, чтобы назвать выбранное,
  // а не чтобы отбирать.
  useEffect(() => {
    api.listBoards().then((r) => setBoards(r.boards.map((b) => ({ id: b.id, name: b.name })))).catch(() => {})
    api.listTeams().then((r) => setTeams(r.teams.map((x) => ({ id: x.id, name: x.name })))).catch(() => {})
    api.listLabels().then((r) => setLabels(r.labels.map((l) => ({ id: l.id, name: l.name })))).catch(() => {})
  }, [])

  // Итерации — у одной доски: у разных досок они разные, и общий
  // список из «Недель 33» трёх досок читался бы одной итерацией.
  const oneBoard = f.boards.length === 1 ? f.boards[0] : null
  useEffect(() => {
    if (!oneBoard) {
      setIterations([])
      return
    }
    let current = true
    api
      .snapshot(oneBoard)
      .then((snap) => current && setIterations(snap.iterations))
      .catch(() => current && setIterations([]))
    return () => {
      current = false
    }
  }, [oneBoard])

  // Подсчёт — с задержкой: дату набирают по цифре, и спрашивать сервер
  // на каждую незачем.
  const key = reportQuery(f).toString()
  useEffect(() => {
    let current = true
    const filter = parseReportFilter(new URLSearchParams(key), now)
    if (periodReversed(filter)) return
    setCount({ status: 'counting' })
    setSummary({ status: 'loading' })
    const timer = setTimeout(() => {
      countCards(filter)
        .then((r) => current && setCount({ status: 'ready', ...r }))
        .catch((e) => current && setCount({ status: 'error', error: e instanceof Error ? e.message : s.countFailed }))
      // Люди — тем же отбором и в тот же момент: выбрали подразделение —
      // список исполнителей сузился до его людей.
      reportPeople(filter)
        .then((r) => current && setPeople(r))
        .catch(() => current && setPeople(null))
      // Сводка — тем же отбором: что покажет файл, видно до файла.
      reportSummary(filter)
        .then((r) => current && setSummary({ status: 'ready', data: r }))
        .catch((e) => current && setSummary({ status: 'error', error: e instanceof Error ? e.message : s.summaryFailed }))
    }, 300)
    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [key, now, s])

  const change = (patch: Partial<ReportFilter>) => {
    const next = { ...f, ...patch }
    // Итерация принадлежит доске: сменили доски — итерация ушла.
    if (patch.boards && !(next.boards.length === 1 && next.boards[0] === oneBoard)) next.iteration = null
    setQuery(addressQuery(next), { replace: true })
  }
  const toggle = <T extends string>(list: T[], value: T) =>
    list.includes(value) ? list.filter((v) => v !== value) : [...list, value]

  const reversed = periodReversed(f)
  const tooMany = count.status === 'ready' && count.total > count.limit
  const blocked = reversed || tooMany

  return (
    <div className="stack reports">
      <p className="muted">{s.intro}</p>
      <Slices query={addressQuery(f).toString()} onOpen={(q) => setQuery(new URLSearchParams(q), { replace: false })} />

      <fieldset className="stack reports-group">
        <legend>{s.period}</legend>
        <div className="row">
          <label className="row row--tight">
            <span className="sr-only">{s.from}</span>
            <input type="date" value={f.from} max={f.to}
              onChange={(e) => e.target.value && change({ from: e.target.value, to: f.to, period: null })} />
          </label>
          <span aria-hidden="true">—</span>
          <label className="row row--tight">
            <span className="sr-only">{s.to}</span>
            <input type="date" value={f.to} min={f.from}
              onChange={(e) => e.target.value && change({ from: f.from, to: e.target.value, period: null })} />
          </label>
        </div>
        <div className="row row--tight">
          {PRESETS.map((p) => (
            <Button key={p} kind="quiet" aria-pressed={f.period === p}
              onClick={() => change({ period: p, ...presetPeriod(p, now) })}>
              {s.presets[p]}
            </Button>
          ))}
        </div>
        <p className="muted small">{s.periodRule}</p>
      </fieldset>

      <fieldset className="stack reports-group">
        <legend className="sr-only">{s.title}</legend>
        <PickList label={s.boards} anyText={s.allBoards} addText={s.addBoard} options={boards}
          chosen={f.boards} removeLabel={s.remove} onChange={(v) => change({ boards: v })} />
        <PickList label={s.teams} anyText={s.allTeams} addText={s.addTeam} options={teams}
          chosen={f.teams} removeLabel={s.remove} onChange={(v) => change({ teams: v })} />
        <PickList label={s.assignees} anyText={s.allAssignees} addText={s.addAssignee}
          options={people?.candidates ?? []}
          chosen={f.assignees} removeLabel={s.remove} onChange={(v) => change({ assignees: v })} />
        {(f.boards.length > 0 || f.teams.length > 0) && (
          <p className="muted small">{s.assigneesNarrowed}</p>
        )}
        <PickList label={s.labels} anyText={s.allLabels} addText={s.addLabel} options={labels}
          chosen={f.labels} removeLabel={s.remove} onChange={(v) => change({ labels: v })} />

        <div className="row" role="group" aria-label={s.priority}>
          <span className="small report-name">{s.priority}</span>
          {PRIORITIES.map((p) => (
            <label key={p} className="row row--tight small">
              <input type="checkbox" checked={f.priorities.includes(p)}
                onChange={() => change({ priorities: toggle(f.priorities, p) })} />
              <span>{PRIORITY_NAMES[p]}</span>
            </label>
          ))}
        </div>
        <div className="row" role="group" aria-label={s.state}>
          <span className="small report-name">{s.state}</span>
          {STATES.map((st) => (
            <label key={st} className="row row--tight small">
              <input type="checkbox" checked={f.states.includes(st)}
                onChange={() => change({ states: toggle(f.states, st) })} />
              <span>{s.states[st]}</span>
            </label>
          ))}
        </div>
        <div className="row">
          <label className="row row--tight">
            <span className="small report-name">{s.iteration}</span>
            <select value={f.iteration ?? ''} disabled={!oneBoard}
              onChange={(e) => change({ iteration: e.target.value || null })}>
              <option value="">{s.anyIteration}</option>
              {iterations.map((it) => (
                <option key={it.id} value={it.id}>
                  {it.name}
                </option>
              ))}
            </select>
          </label>
          {!oneBoard && <span className="muted small">{s.iterationHint}</span>}
        </div>
        <label className="row row--tight small">
          <input type="checkbox" checked={f.archived} onChange={(e) => change({ archived: e.target.checked })} />
          <span>{s.archived}</span>
        </label>
        <div>
          <Button kind="quiet" onClick={() => setQuery(addressQuery({ ...parseReportFilter(new URLSearchParams(), now),
              period: f.period, from: f.from, to: f.to }), { replace: true })}>
            {s.reset}
          </Button>
        </div>
      </fieldset>

      {/* Число — живым регионом: оно меняется от каждого выбора, и
          диктор должен узнать, сколько теперь попадает. */}
      <p className={tooMany || reversed ? 'form-error' : 'small'} role="status">
        {reversed
          ? s.reversed
          : count.status === 'counting'
            ? s.counting
            : count.status === 'error'
              ? count.error
              : tooMany
                ? s.tooMany(count.total, count.limit)
                : count.total === 0
                  ? s.none
                  : s.count(count.total)}
      </p>

      {/* Сводка — только когда есть о чём: при нуле карточек строка
          выше уже сказала, что в файле будет одна шапка. Отказ —
          словами: пропавшая сводка выглядела бы как «её тут и не было». */}
      {!reversed && summary.status === 'error' && <p className="form-error">{summary.error}</p>}
      {!reversed && summary.status === 'ready' && summary.data.cards > 0 && (
        <>
          <SummaryView sum={summary.data} />
          {summary.data.people.length > 0 && (
            <ByPerson rows={summary.data.people} chosen={f.assignees} onPick={(id) => change({ assignees: [id] })} />
          )}
        </>
      )}

      <div className="row">
        <span className="small report-name">{s.download}</span>
        {FORMATS.map((format, i) =>
          // Ссылка, а не кнопка с запросом: файл скачивает браузер,
          // потоком. Недоступная ссылка — просто текст с объяснением
          // над ней: у `<a>` нет состояния disabled.
          blocked ? (
            <span key={format} className="btn btn--default" aria-disabled="true">
              {s.formats[format]}
            </span>
          ) : (
            <a key={format} className={`btn btn--${i === 0 ? 'primary' : 'default'}`} href={downloadHref(f, format)} download>
              {s.formats[format]}
            </a>
          ),
        )}
      </div>
      <p className="muted small">{s.formatsHint}</p>
    </div>
  )
}

/**
 * Сводка организации на экране (ROADMAP 42.3, срез 1). До неё лист
 * «Сводка» считался только в файл, и чтобы увидеть, сколько сделано
 * за квартал, тимлид скачивал книгу. Здесь — те же числа и те же две
 * диаграммы, что в книге, под тем же отбором; сервер считает их одним
 * запросом, и экран с файлом разойтись не могут.
 *
 * Диаграммы и слова — с «Потока»: руководство уже читает их там
 * по одной доске, здесь — по всей организации или по отбору.
 */
function SummaryView({ sum }: { sum: Summary }) {
  const s = t.reports
  const days = (v: number) => s.days(round(v))
  return (
    <>
      <section className="stack reports-group">
        <h3 className="section-title">{s.summary}</h3>
        <div className="row row--tight">
          <Figure label={s.finished} value={String(sum.finished)} />
          <Figure label={s.discarded} value={String(sum.discarded)} />
          <Figure label={s.wip} value={String(sum.wip)} />
        </div>
        <div className="row row--tight">
          <span className="small report-name">{s.cycle}</span>
          {sum.cycleTime ? (
            <>
              <Figure label={s.half} value={days(sum.cycleTime.p50)} />
              <Figure label={s.p85} value={days(sum.cycleTime.p85)} />
            </>
          ) : (
            <span className="muted small">{s.noCycle}</span>
          )}
        </div>
        {sum.age && (
          <div className="row row--tight">
            <span className="small report-name">{s.age}</span>
            <Figure label={s.half} value={days(sum.age.p50)} />
            <Figure label={s.p85} value={days(sum.age.p85)} />
          </div>
        )}
        <p className="muted small">{s.summaryHint}</p>
      </section>

      <section className="stack reports-group">
        <h3 className="section-title">{t.flow.throughput}</h3>
        {sum.throughput.some((w) => w.count > 0) ? (
          <>
            <Bars values={sum.throughput.map((w) => w.count)}
              labels={sum.throughput.map((w) => t.flow.week(dateWords(w.week)))} />
            <p className="muted small">{t.flow.byWeek}</p>
          </>
        ) : (
          <p className="muted small">{s.noThroughput}</p>
        )}
      </section>

      {/* Накопительная рисуется от двух точек: период в один день —
          одна точка, и диаграмма молча не показывается. */}
      {sum.flow.length > 1 && (
        <section className="stack reports-group">
          <h3 className="section-title">{t.flow.cfd}</h3>
          <CumulativeFlow flow={sum.flow} />
        </section>
      )}

      {sum.iterations.length > 0 && (
        <section className="stack reports-group">
          <h3 className="section-title">{s.iterations}</h3>
          <div className="table-wrap">
            <table className="figures reports-table" aria-label={s.iterations}>
              <thead>
                <tr>
                  <th scope="col">{s.iterationName}</th>
                  <th scope="col">{s.board}</th>
                  <th scope="col">{s.starts}</th>
                  <th scope="col">{s.ends}</th>
                  <th scope="col">{s.planned}</th>
                  <th scope="col">{s.finished}</th>
                  <th scope="col">{s.share}</th>
                </tr>
              </thead>
              <tbody>
                {sum.iterations.map((it) => (
                  <tr key={`${it.board}/${it.name}/${it.startsOn}`}>
                    <th scope="row">{it.name}</th>
                    <td>{it.board}</td>
                    <td>{dateWords(it.startsOn)}</td>
                    <td>{dateWords(it.endsOn)}</td>
                    <td>{it.planned}</td>
                    <td>{it.done}</td>
                    <td>{it.planned > 0 ? s.percent(Math.round((100 * it.done) / it.planned)) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="muted small">{s.iterationsHint}</p>
        </section>
      )}

      <section className="stack reports-group">
        <h3 className="section-title">{s.byTeam}</h3>
        <div className="table-wrap">
          <table className="figures reports-table" aria-label={s.byTeam}>
            <thead>
              <tr>
                <th scope="col">{s.team}</th>
                <th scope="col">{s.cards}</th>
                <th scope="col">{s.finished}</th>
                <th scope="col">{s.wip}</th>
                <th scope="col">{s.cycleMedian}</th>
                <th scope="col">{s.cycleP85}</th>
              </tr>
            </thead>
            <tbody>
              {sum.teams.map((r) => (
                <tr key={r.team}>
                  <th scope="row">{r.team || <span className="muted">{s.noTeam}</span>}</th>
                  <td>{r.cards}</td>
                  <td>{r.finished}</td>
                  <td>{r.wip}</td>
                  <td>{r.cycleTimeP50 === null ? '—' : s.days(r.cycleTimeP50)}</td>
                  <td>{r.cycleTimeP85 === null ? '—' : s.days(r.cycleTimeP85)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="muted small">{s.byTeamHint}</p>
      </section>
    </>
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

// Дни — до десятых, как на «Потоке»: «3,47 дн.» точнее, чем данные.
const round = (v: number) => Math.round(v * 10) / 10

/**
 * Разрез по исполнителям под нынешний отбор (ROADMAP 37.1): «что делал
 * Борис» прямо на экране, без файла и фильтра в Excel. Имя — кнопка:
 * она ставит отбор на этого человека, и выгрузка ниже становится его.
 * Карточка с двумя исполнителями считается у обоих.
 */
function ByPerson({ rows, chosen, onPick }: { rows: PersonRow[]; chosen: string[]; onPick: (id: string) => void }) {
  const s = t.reports
  const days = (v: number | null) => (v === null ? '—' : s.days(v))
  return (
    <section className="stack reports-group">
      <h3 className="section-title">{s.byPerson}</h3>
      <div className="table-wrap">
        <table className="figures reports-table" aria-label={s.byPerson}>
          <thead>
            <tr>
              <th scope="col">{s.person}</th>
              <th scope="col">{s.cards}</th>
              <th scope="col">{s.finished}</th>
              <th scope="col">{s.wip}</th>
              <th scope="col">{s.cycleMedian}</th>
              <th scope="col">{s.workMedian}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.userId ?? 'none'}>
                <th scope="row">
                  {r.userId === null ? (
                    <span className="muted">{s.noAssignee}</span>
                  ) : chosen.length === 1 && chosen[0] === r.userId ? (
                    <strong>{r.name}</strong>
                  ) : (
                    <button type="button" className="link" aria-label={s.onlyPerson(r.name)}
                      onClick={() => onPick(r.userId!)}>
                      {r.name}
                    </button>
                  )}
                </th>
                <td>{r.cards}</td>
                <td>{r.finished}</td>
                <td>{r.wip}</td>
                <td>{days(r.cycleTimeP50)}</td>
                <td>{days(r.workTimeP50)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="muted small">{s.byPersonHint}</p>
    </section>
  )
}
