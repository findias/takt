import { useEffect, useState } from 'react'
import { api, request } from '../shared/api/index.ts'
import type { Iteration, Principal } from '../shared/api/index.ts'
import { Board } from './Board.tsx'
import { PRIORITY_NAMES } from '../entities/card/model.ts'
import { setQuery, useQuery } from '../shared/router/index.ts'
import { PickList } from '../shared/ui/PickList.tsx'
import { Slices } from '../features/reports/Slices.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { Bars, CumulativeFlow } from '../features/flow/charts.tsx'
import { UNIT_NAMES, dateWords } from '../entities/card/model.ts'
import type { EstimateUnit } from '../shared/api/index.ts'
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
  due: { withDue: number; finished: number; onTime: number; overdueNow: number }
  blocks: { cards: number; days: number; reasons: { reason: string; cards: number; days: number }[] }
  throughput: { week: string; count: number; created: number }[]
  flowStep: 'day' | 'week'
  flow: { day: string; queued: number; inProgress: number; done: number }[]
  iterations: {
    name: string
    board: string
    startsOn: string
    endsOn: string
    closed: boolean
    planned: number
    added: number
    dropped: number
    done: number
    donePlanned: number
  }[]
  teams: { team: string; cards: number; finished: number; wip: number; cycleTimeP50: number | null; cycleTimeP85: number | null }[]
  effort: { finished: number; unit: string; mixedUnits: boolean; byLabel: ShareRow[]; byEpic: ShareRow[] }
  people: PersonRow[]
}
type ShareRow = { name: string; scope?: 'org' | 'team' | 'board'; scopeName?: string; finished: number; weight: number; unestimated: number }
type SummaryState = { status: 'loading' } | { status: 'error'; error: string } | { status: 'ready'; data: Summary }
/** «Требует внимания» (ROADMAP 42.3, срез 5): пять групп на сегодня. */
type AttentionCard = {
  id: string
  boardId: string
  board: string
  number: string
  title: string
  column: string
  days: number
  reason?: string
  dueOn?: string
}
type Attention = {
  blocked: AttentionCard[]
  overPromise: AttentionCard[]
  overdue: AttentionCard[]
  unassigned: AttentionCard[]
  stale: AttentionCard[]
  truncated: boolean
}

// Вызовы — здесь, а не в общем клиенте: экран грузится отдельным куском.
const countCards = (f: ReportFilter) =>
  request<{ total: number; limit: number }>('GET', '/api/reports/cards/count?' + reportQuery(f).toString())
// Люди под отбор (ROADMAP 37.1–37.2): из кого выбирать исполнителей —
// люди выбранных досок и подразделений.
const reportPeople = (f: ReportFilter) => request<People>('GET', '/api/reports/people?' + reportQuery(f).toString())
// Сводка под отбор (ROADMAP 42.3, срез 1): то, что раньше было только
// на первом листе файла, — на экране, с разрезом по людям внутри.
const reportSummary = (f: ReportFilter) => request<Summary>('GET', '/api/reports/summary?' + reportQuery(f).toString())
// Застрявшее на сегодня под тот же отбор; период сервер не применяет.
const reportAttention = (f: ReportFilter) => request<Attention>('GET', '/api/reports/attention?' + reportQuery(f).toString())

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
export function Reports({ principal }: { principal: Principal }) {
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
  const [attention, setAttention] = useState<Attention | null>(null)
  // Открытая карточка — в адресе, как на «Задачах»: «доска:карточка».
  // Доска панели остаётся подключённой после закрытия: правка перед
  // самым закрытием подтверждается уже после него.
  const [openBoard, openCard] = (query.get('open') ?? '').split(':')
  const [panelBoard, setPanelBoard] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => {
    if (openBoard) setPanelBoard(openBoard)
  }, [openBoard])

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
      // Застрявшее — тем же отбором, на сегодня; перечитывается и после
      // правки в панели: сняли блокировку — строка должна уйти.
      reportAttention(filter)
        .then((r) => current && setAttention(r))
        .catch(() => current && setAttention(null))
    }, 300)
    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [key, now, s, reloadKey])

  const change = (patch: Partial<ReportFilter>) => {
    const next = { ...f, ...patch }
    // Итерация принадлежит доске: сменили доски — итерация ушла.
    if (patch.boards && !(next.boards.length === 1 && next.boards[0] === oneBoard)) next.iteration = null
    setQuery(withOpen(addressQuery(next), query.get('open')), { replace: true })
  }
  // Открытая карточка живёт в адресе рядом с отбором: отбор её не теряет,
  // а она — отбор.
  const withOpen = (q: URLSearchParams, open: string | null) => {
    if (open) q.set('open', open)
    return q
  }
  const setOpen = (open: string | null) => setQuery(withOpen(addressQuery(f), open), { replace: true })
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

      {!reversed && attention && (
        <AttentionView list={attention} onOpen={(c) => setOpen(`${c.boardId}:${c.id}`)} />
      )}

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
      {panelBoard && (
        <Board
          key={panelBoard}
          boardId={panelBoard}
          cardId={openBoard === panelBoard && openCard ? openCard : null}
          onCard={(cardId) => setOpen(cardId ? `${panelBoard}:${cardId}` : null)}
          onChanged={() => setReloadKey((k) => k + 1)}
          unit={principal.estimateUnit}
          meId={principal.id}
          isOwner={principal.role === 'owner'}
          canEdit={principal.role !== 'viewer'}
          onBack={() => setOpen(null)}
          panelOnly
        />
      )}
    </div>
  )
}

/**
 * «Требует внимания сейчас» (ROADMAP 42.3, срез 5): пять вопросов
 * тимлида одной страницей — что заблокировано и давно ли, что идёт
 * дольше обещания доски, что просрочено, что в работе без исполнителя,
 * что стоит в колонке дольше «без движения» доски. Не новые данные,
 * а новый срез тех же: каждая строка ведёт в карточку, и беда чинится
 * тут же, сбоку. Пусто — сказано словами, а не пятью пустыми таблицами.
 */
function AttentionView({ list, onOpen }: { list: Attention; onOpen: (c: AttentionCard) => void }) {
  const s = t.reports
  const groups: { key: keyof Omit<Attention, 'truncated'>; title: string; what: string }[] = [
    { key: 'blocked', title: s.attnBlocked, what: s.attnBlockedDays },
    { key: 'overPromise', title: s.attnOverPromise, what: s.attnInWorkDays },
    { key: 'overdue', title: s.attnOverdue, what: s.attnOverdueDays },
    { key: 'unassigned', title: s.attnUnassigned, what: s.attnInWorkDays },
    { key: 'stale', title: s.attnStale, what: s.attnStaleDays },
  ]
  const total = groups.reduce((n, g) => n + list[g.key].length, 0)
  return (
    <section className="stack reports-group">
      <h3 className="section-title">{s.attention}</h3>
      {total === 0 ? (
        <p className="muted small">{s.attentionNone}</p>
      ) : (
        groups
          .filter((g) => list[g.key].length > 0)
          .map((g) => (
            <div key={g.key} className="stack stack--tight">
              <h4 className="small report-name">{g.title}: {list[g.key].length}</h4>
              <div className="table-wrap">
                <table className="figures reports-table attention-table" aria-label={g.title}>
                  <thead>
                    <tr>
                      <th scope="col">{s.attnCard}</th>
                      <th scope="col">{s.board}</th>
                      <th scope="col">{s.attnColumn}</th>
                      <th scope="col" className="attention-days">{g.what}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {list[g.key].map((c) => (
                      <tr key={c.id}>
                        <th scope="row">
                          <button type="button" className="link table-title" onClick={() => onOpen(c)}>
                            {c.number} {c.title}
                          </button>
                          {c.reason && <div className="muted small">{c.reason}</div>}
                          {c.dueOn && <div className="muted small">{s.attnDue(dateWords(c.dueOn))}</div>}
                        </th>
                        <td>{c.board}</td>
                        <td className="muted">{c.column}</td>
                        <td className="attention-days">{s.days(c.days)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ))
      )}
      {list.truncated && <p className="muted small">{s.attentionTruncated}</p>}
      <p className="muted small">{s.attentionHint}</p>
    </section>
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
        {/* Сроки (42.3, срез 8): доля в срок — от сделанных со сроком,
            просроченные — на сегодня. Без единого срока в отборе — слова,
            а не три нуля. */}
        <div className="row row--tight">
          <span className="small report-name">{s.due}</span>
          {sum.due.withDue > 0 ? (
            <>
              {sum.due.finished > 0 ? (
                <Figure label={s.onTime(sum.due.finished)}
                  value={s.percent(Math.round((100 * sum.due.onTime) / sum.due.finished))} />
              ) : (
                <Figure label={s.noneDueDone} value={String(sum.due.withDue)} />
              )}
              <Figure label={s.overdueNow} value={String(sum.due.overdueNow)} />
            </>
          ) : (
            <span className="muted small">{s.noDue}</span>
          )}
        </div>
        <p className="muted small">{s.summaryHint}</p>
      </section>

      <section className="stack reports-group">
        <h3 className="section-title">{t.flow.throughput}</h3>
        {sum.throughput.some((w) => w.count > 0 || w.created > 0) ? (
          <>
            <Bars values={sum.throughput.map((w) => w.count)} created={sum.throughput.map((w) => w.created)}
              labels={sum.throughput.map((w) => t.flow.week(dateWords(w.week)))} />
            <p className="muted small">{t.flow.byWeek} {t.flow.createdVsDone}</p>
          </>
        ) : (
          <p className="muted small">{s.noThroughput}</p>
        )}
      </section>

      {/* Блокировки за период (42.3, срез 6): сколько дней простояли
          и из-за чего. Не было — одной строкой, без пустой таблицы. */}
      <section className="stack reports-group">
        <h3 className="section-title">{s.blocks}</h3>
        {sum.blocks.cards === 0 ? (
          <p className="muted small">{s.noBlocks}</p>
        ) : (
          <>
            <div className="row row--tight">
              <Figure label={s.blockedCards} value={String(sum.blocks.cards)} />
              <Figure label={s.blockedDays} value={s.days(sum.blocks.days)} />
              <Figure label={s.blockedPerCard} value={s.days(round(sum.blocks.days / sum.blocks.cards))} />
            </div>
            <div className="table-wrap">
              <table className="figures reports-table" aria-label={s.blockReasons}>
                <thead>
                  <tr>
                    <th scope="col">{s.blockReason}</th>
                    <th scope="col">{s.cards}</th>
                    <th scope="col">{s.blockDays}</th>
                  </tr>
                </thead>
                <tbody>
                  {sum.blocks.reasons.map((r) => (
                    <tr key={r.reason}>
                      <th scope="row">{r.reason || <span className="muted">{s.noReason}</span>}</th>
                      <td>{r.cards}</td>
                      <td>{s.days(r.days)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="muted small">{s.blocksHint}</p>
          </>
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
                  <th scope="col">{s.added}</th>
                  <th scope="col">{s.dropped}</th>
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
                    <td>{it.added}</td>
                    <td>{it.dropped}</td>
                    <td>{it.done}</td>
                    {/* Доля — от обещанного: добавленное по ходу её не меняет. */}
                    <td>{it.planned > 0 ? s.percent(Math.round((100 * it.donePlanned) / it.planned)) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="muted small">{s.iterationsHint}</p>
        </section>
      )}

      {/* Куда ушли силы (42.3, срез 2): по меткам и по эпикам. Главный
          ответ на «мы вообще чем заняты». Доли считаются от всех
          сделанных; карточка с двумя метками — у обеих. */}
      {sum.effort.finished > 0 && (
        <section className="stack reports-group">
          <h3 className="section-title">{s.effort}</h3>
          <ShareTable rows={sum.effort.byLabel} total={sum.effort.finished} label={s.effortByLabel}
            nameHead={s.label} none={s.noLabel} unit={sum.effort.mixedUnits ? null : sum.effort.unit} withScope />
          <ShareTable rows={sum.effort.byEpic} total={sum.effort.finished} label={s.effortByEpic}
            nameHead={s.epic} none={s.noEpic} unit={sum.effort.mixedUnits ? null : sum.effort.unit} />
          <p className="muted small">
            {s.effortHint}
            {sum.effort.mixedUnits && ' ' + s.effortMixed}
          </p>
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

/** Таблица долей: имя, область (у меток), сделано, доля, вес. Вес
 *  спрятан, когда доски считают в разных единицах: складывать часы
 *  с очками нельзя, и подпись не спасёт. */
function ShareTable({ rows, total, label, nameHead, none, unit, withScope }: {
  rows: ShareRow[]
  total: number
  label: string
  nameHead: string
  none: string
  unit: string | null
  withScope?: boolean
}) {
  const s = t.reports
  const scope = (r: ShareRow) =>
    r.scope === 'team' ? s.scopeTeam(r.scopeName ?? '') : r.scope === 'board' ? s.scopeBoard(r.scopeName ?? '') : r.scope === 'org' ? s.scopeOrg : ''
  return (
    <div className="stack stack--tight">
      <h4 className="small report-name">{label}</h4>
      <div className="table-wrap">
        <table className="figures reports-table" aria-label={label}>
          <thead>
            <tr>
              <th scope="col">{nameHead}</th>
              {withScope && <th scope="col">{s.scope}</th>}
              <th scope="col">{s.finished}</th>
              <th scope="col">{s.effortShare}</th>
              {unit && <th scope="col">{s.weight(UNIT_NAMES[unit as EstimateUnit] ?? unit)}</th>}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.scope ?? ''}/${r.scopeName ?? ''}/${r.name}`}>
                <th scope="row">{r.name || <span className="muted">{none}</span>}</th>
                {withScope && <td className="muted">{scope(r)}</td>}
                <td>{r.finished}</td>
                <td>{s.percent(Math.round((100 * r.finished) / total))}</td>
                {unit && <td>{r.weight.toLocaleString()}{r.unestimated > 0 && <span className="muted"> {s.unestimated(r.unestimated)}</span>}</td>}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
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
