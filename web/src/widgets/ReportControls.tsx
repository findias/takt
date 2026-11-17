import type { Iteration } from '../shared/api/index.ts'
import { PRIORITY_NAMES } from '../entities/card/model.ts'
import { PickList } from '../shared/ui/PickList.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { t } from '../shared/i18n/index.ts'
import { PRESETS, PRIORITIES, STATES, presetPeriod } from './reportFilter.ts'
import type { ReportFilter } from './reportFilter.ts'

type Option = { id: string; name: string }
type Props = {
  f: ReportFilter
  now: Date
  boards: Option[]
  teams: Option[]
  labels: Option[]
  people: { candidates: Option[] } | null
  iterations: Iteration[]
  change: (patch: Partial<ReportFilter>) => void
  onReset: () => void
}

/** Сложный отбор раскрывается отдельно; выбранные условия остаются
 *  видимыми, иначе закрытая форма скрывала бы смысл результатов. */
export function ReportControls({ f, now, boards, teams, labels, people, iterations, change, onReset }: Props) {
  const s = t.reports
  const oneBoard = f.boards.length === 1 ? f.boards[0] : null
  const toggle = <T extends string>(list: T[], value: T) =>
    list.includes(value) ? list.filter((v) => v !== value) : [...list, value]
  const groups = [
    { key: 'boards', label: s.boards, options: boards },
    { key: 'teams', label: s.teams, options: teams },
    { key: 'assignees', label: s.assignees, options: people?.candidates ?? [] },
    { key: 'labels', label: s.labels, options: labels },
  ] as const
  const conditions = groups.flatMap(({ key, label, options }) => f[key].map((id) => ({
    key: `${key}:${id}`, text: `${label}: ${options.find((o) => o.id === id)?.name ?? '…'}`,
    remove: () => change({ [key]: f[key].filter((v) => v !== id) }),
  })))
  for (const p of f.priorities) conditions.push({ key: `priority:${p}`, text: `${s.priority}: ${PRIORITY_NAMES[p]}`, remove: () => change({ priorities: toggle(f.priorities, p) }) })
  for (const st of f.states) conditions.push({ key: `state:${st}`, text: `${s.state}: ${s.states[st]}`, remove: () => change({ states: toggle(f.states, st) }) })
  if (f.iteration) conditions.push({ key: 'iteration', text: `${s.iteration}: ${iterations.find((it) => it.id === f.iteration)?.name ?? '…'}`, remove: () => change({ iteration: null }) })
  if (f.archived) conditions.push({ key: 'archived', text: s.archived, remove: () => change({ archived: false }) })

  return (
    <div className="reports-controls stack">
      <fieldset className="reports-period">
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
        <div className="row row--tight reports-presets">
          {PRESETS.map((p) => (
            <Button key={p} kind="quiet" aria-pressed={f.period === p}
              onClick={() => change({ period: p, ...presetPeriod(p, now) })}>
              {s.presets[p]}
            </Button>
          ))}
        </div>
        <label className="reports-preset-compact">
          <span className="sr-only">{s.quickPeriod}</span>
          <select value={f.period ?? ''} onChange={(e) => {
            const preset = PRESETS.find((p) => p === e.target.value)
            change(preset ? { period: preset, ...presetPeriod(preset, now) } : { period: null })
          }}>
            <option value="">{s.customPeriod}</option>
            {PRESETS.map((p) => <option value={p} key={p}>{s.presets[p]}</option>)}
          </select>
        </label>
      </fieldset>

      <details className="reports-filter-panel">
        <summary>{s.filter}{conditions.length > 0 && <span className="badge">{conditions.length}</span>}</summary>
        <fieldset className="stack reports-group">
          <legend className="sr-only">{s.title}</legend>
          <details className="reports-period-help small"><summary>{s.periodHelp}</summary><p className="muted">{s.periodRule}</p></details>
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
        </fieldset>
      </details>
      <div className="row reports-selection-line">
        {conditions.length > 0 ? <ul className="reports-selection" aria-label={s.activeFilters}>
          {conditions.map((condition) => <li key={condition.key}>
            <button type="button" className="chip chip--slate chip--removable" aria-label={s.remove(condition.text)} onClick={condition.remove}>
              {condition.text}<span aria-hidden="true"> ×</span>
            </button>
          </li>)}
        </ul> : <span className="muted small">{s.noExtraFilters}</span>}
        {conditions.length > 0 && <Button kind="quiet" onClick={onReset}>{s.reset}</Button>}
      </div>
    </div>
  )
}
