import { useEffect, useState } from 'react'
import { api, request } from '../shared/api/index.ts'
import type { Iteration } from '../shared/api/index.ts'
import { PRIORITY_NAMES } from '../entities/card/model.ts'
import { setQuery, useQuery } from '../shared/router/index.ts'
import { PickList } from '../shared/ui/PickList.tsx'
import { Slices } from '../features/reports/Slices.tsx'
import { Button } from '../shared/ui/Button.tsx'
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

// Вызов — здесь, а не в общем клиенте: экран грузится отдельным куском.
const countCards = (f: ReportFilter) =>
  request<{ total: number; limit: number }>('GET', '/api/reports/cards/count?' + reportQuery(f).toString())

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
  const [people, setPeople] = useState<Option[]>([])
  const [labels, setLabels] = useState<Option[]>([])
  const [iterations, setIterations] = useState<Iteration[]>([])
  const [count, setCount] = useState<Count>({ status: 'counting' })

  // Списки для выбора — один раз: они нужны, чтобы назвать выбранное,
  // а не чтобы отбирать.
  useEffect(() => {
    api.listBoards().then((r) => setBoards(r.boards.map((b) => ({ id: b.id, name: b.name })))).catch(() => {})
    api.listTeams().then((r) => setTeams(r.teams.map((x) => ({ id: x.id, name: x.name })))).catch(() => {})
    api
      .team()
      .then((r) =>
        setPeople(r.members.filter((m) => m.kind === 'person').map((m) => ({ id: m.userId, name: m.name }))),
      )
      .catch(() => {})
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
    const timer = setTimeout(() => {
      countCards(filter)
        .then((r) => current && setCount({ status: 'ready', ...r }))
        .catch((e) => current && setCount({ status: 'error', error: e instanceof Error ? e.message : s.countFailed }))
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
        <PickList label={s.assignees} anyText={s.allAssignees} addText={s.addAssignee} options={people}
          chosen={f.assignees} removeLabel={s.remove} onChange={(v) => change({ assignees: v })} />
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
