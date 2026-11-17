import { useEffect, useMemo, useState } from 'react'
import type { BaseState } from '../../entities/board/model.ts'
import { request } from '../../shared/api/index.ts'
import type { EstimateUnit, TreeNode } from '../../shared/api/index.ts'
import { completion, priorityLabel, progressLabel } from '../../entities/card/model.ts'
import { t } from '../../shared/i18n/index.ts'
import { boardPath, setQuery, useQuery } from '../../shared/router/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { ScreenError } from '../../shared/ui/Field.tsx'
import { useRenderWindow } from '../../shared/lib/useRenderWindow.ts'
import { buildTree } from './treeModel.ts'
import { branchIds, TREE_COLUMNS, treeColumns, treeRows } from './treeRows.ts'

const loadTree = (cardId: string) => request<{ nodes: TreeNode[] }>('GET', `/api/cards/${cardId}/tree`)

/** Иерархия остаётся представлением настоящих связей. Настройки таблицы
 *  живут в адресе и сохраняются обычным механизмом видов доски. */
export function TreeView({ base, unit, onOpenCard }: {
  base: BaseState
  unit?: EstimateUnit
  onOpenCard: (cardId: string) => void
}) {
  const s = t.tree
  const query = useQuery()
  const [remote, setRemote] = useState<Map<string, TreeNode>>(() => new Map())
  const [failed, setFailed] = useState(false)
  const [retry, setRetry] = useState(0)
  const { roots, alone } = useMemo(() => buildTree(base, remote), [base, remote])
  const wanted = roots.filter((r) => r.foreign).map((r) => r.id).join(' ')
  useEffect(() => {
    if (!wanted) return
    let current = true
    void Promise.allSettled(wanted.split(' ').map(loadTree)).then((answers) => {
      if (!current) return
      const next = new Map<string, TreeNode>()
      for (const answer of answers) if (answer.status === 'fulfilled') {
        for (const node of answer.value.nodes) next.set(node.id, node)
      }
      setRemote(next)
      setFailed(answers.some((answer) => answer.status === 'rejected'))
    })
    return () => { current = false }
  }, [wanted, base.links, retry])

  const set = (key: string, value: string) => {
    const next = new URLSearchParams(query)
    if (value) next.set(key, value)
    else next.delete(key)
    setQuery(next, { replace: true })
  }
  const closed = new Set((query.get('treeClosed') ?? '').split(',').filter(Boolean))
  const search = query.get('treeSearch') ?? ''
  const sort = query.get('treeOrder') === 'title' ? 'title' : 'number'
  const columns = treeColumns(query.get('treeColumns'))
  const includeAlone = query.get('treeAlone') === '1'
  const all = includeAlone ? [...roots, ...alone] : roots
  const rows = treeRows(all, closed, search, sort)
  const window_ = useRenderWindow<HTMLTableRowElement>(rows.length)
  const toggle = (id: string) => {
    if (closed.has(id)) closed.delete(id)
    else closed.add(id)
    set('treeClosed', [...closed].join(','))
  }

  return (
    <section className="tree-view stack" aria-label={s.label}>
      <div className="row tree-tools">
        <label className="tree-search">
          <span className="sr-only">{s.search}</span>
          <input type="search" value={search} placeholder={s.search} onChange={(e) => set('treeSearch', e.target.value)} />
        </label>
        <label className="row row--tight small">{s.sort}
          <select value={sort} onChange={(e) => set('treeOrder', e.target.value === 'number' ? '' : e.target.value)}>
            <option value="number">{s.byNumber}</option>
            <option value="title">{s.byTitle}</option>
          </select>
        </label>
        <Button kind="quiet" onClick={() => set('treeClosed', '')}>{s.expandAll}</Button>
        <Button kind="quiet" disabled={Boolean(search.trim())} onClick={() => set('treeClosed', branchIds(all).join(','))}>{s.collapseAll}</Button>
        <details className="tree-column-picker">
          <summary>{s.columns}</summary>
          <div className="row">
            {TREE_COLUMNS.map((key) => <label key={key} className="row row--tight small">
              <input type="checkbox" checked={columns.includes(key)} onChange={(e) => {
                const selected = TREE_COLUMNS.filter((c) => c === key ? e.target.checked : columns.includes(c))
                // Пустая строка в URL означает отсутствие параметра, поэтому
                // «ничего» кодируется явно, отдельно от набора по умолчанию.
                set('treeColumns', selected.join(',') || 'none')
              }} />{s.columnNames[key]}
            </label>)}
          </div>
        </details>
      </div>
      <details className="tree-about small"><summary>{s.about}</summary><p className="muted">{s.intro}</p></details>
      <div className="row tree-options">
        <label className="row row--tight small"><input type="checkbox" checked={includeAlone}
          onChange={(e) => set('treeAlone', e.target.checked ? '1' : '')} />{s.alone(alone.length)}</label>
        <span className="muted small" role="status">{s.rows(rows.length)}</span>
      </div>
      <ScreenError>{failed ? s.incomplete : ''}</ScreenError>
      {failed && <Button kind="quiet" onClick={() => setRetry((n) => n + 1)}>{s.retry}</Button>}
      {rows.length === 0 ? <p className="muted">{search.trim() ? s.noMatches : s.empty}</p> : (
        <div className="table-wrap tree-table-wrap">
          {/* Обычная таблица с кнопками раскрытия: роль treegrid потребовала
              бы отдельной модели навигации по ячейкам, которой здесь нет. */}
          <table className="board-table tree-table">
            <caption className="sr-only">{s.label}</caption>
            <thead><tr>
              <th scope="col">{s.task}</th>
              {columns.map((key) => <th scope="col" key={key}>{s.columnNames[key]}</th>)}
            </tr></thead>
            <tbody>
              {rows.slice(0, window_.limit).map(({ node, depth, open, context }) => {
                const percent = node.own ? completion(node.own)?.percent : node.done ? 100 : undefined
                const progress = node.own ? progressLabel(node.own, unit) : null
                return <tr key={node.id}>
                  <td>
                    <div className="tree-cell" style={{ paddingInlineStart: `calc(${Math.min(depth, 5)} * var(--space-3))` }}>
                      {node.children.length ? <button type="button" className="tree-toggle" aria-expanded={open}
                        aria-label={s.toggle(node.title, open)} disabled={Boolean(search.trim())} onClick={() => toggle(node.id)}>
                        {open ? '▾' : '▸'}
                      </button> : <span className="tree-toggle" aria-hidden="true" />}
                      <div className="tree-cell-content">
                        <span className="mono muted small">{node.number}</span>
                        {node.own ? <button type="button" className="link tree-title" onClick={() => onOpenCard(node.id)}>{node.title}</button>
                          : node.boardId ? <a className="link tree-title" href={boardPath(node.boardId, node.id)}>{node.title}</a>
                            : <span className="tree-title">{node.title}</span>}
                        {context && <span className="muted small">{s.context}</span>}
                        {depth > 0 && <span className="sr-only">{s.level(depth + 1)}</span>}
                        {node.blocked && <span className="mark mark--alarm">{s.blocked}</span>}
                        {node.done && <span className="mark">{s.done}</span>}
                      </div>
                    </div>
                  </td>
                  {columns.includes('where') && <td className="muted small">{node.where}</td>}
                  {columns.includes('priority') && <td className="muted small">{node.priority ? priorityLabel(node.priority) : '—'}</td>}
                  {columns.includes('progress') && <td className="tree-progress">
                    {percent === undefined ? '—' : <><progress max={100} value={percent} aria-label={s.progress(node.title)} /><span className="num">{percent}%</span></>}
                    {progress && <span className="muted small">{progress}</span>}
                  </td>}
                </tr>
              })}
              {window_.rest > 0 && <tr ref={window_.tail}><td colSpan={columns.length + 1}>
                <Button kind="quiet" onClick={window_.more}>{t.talk.moreRows(window_.rest)}</Button>
              </td></tr>}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
