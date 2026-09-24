import { useEffect, useMemo, useState } from 'react'
import type { BaseState } from '../../entities/board/model.ts'
import { request } from '../../shared/api/index.ts'
import type { Card, EstimateUnit, TreeNode } from '../../shared/api/index.ts'
import { progressLabel } from '../../entities/card/model.ts'
import { locale, t } from '../../shared/i18n/index.ts'
import { boardPath, navigate } from '../../shared/router/index.ts'

/**
 * Вид «Дерево» (этап 32.6): эпики, фичи и задачи доски — иерархией,
 * рядом с «Доской» и «Таблицей», а не поверх них. Кому нужна иерархия,
 * смотрит здесь; доска остаётся доской потока.
 *
 * Строится из снимка доски, а ветки, уходящие на другие доски,
 * досчитывает сервер (этап 33.5): снимок знает только свои карточки
 * и их прямых соседей, а эпик портфеля лежит тремя уровнями на досках
 * команд. Спрашиваются только такие ветки — дерево доски, где всё своё,
 * запросов не делает. Свои карточки берутся из снимка: они живые,
 * а ответ сервера — на момент запроса.
 *
 * Отбор доски здесь не действует: дерево без половины веток не
 * отвечает на вопрос «далеко ли до эпика».
 */

const loadTree = (cardId: string) => request<{ nodes: TreeNode[] }>('GET', `/api/cards/${cardId}/tree`)

type Node = {
  id: string
  title: string
  number: string | null
  own: Card | null
  /** Где карточка: колонка этой доски или чужая доска. */
  where: string
  /** Доска чужой карточки, если она видна: туда ведёт её название. */
  boardId: string | null
  done: boolean
  blocked: boolean
  /** Есть ли в ветке карточки других досок — тогда её досчитывает сервер. */
  foreign: boolean
  children: Node[]
}

export function TreeView({
  base,
  unit,
  onOpenCard,
}: {
  base: BaseState
  unit?: EstimateUnit
  onOpenCard: (cardId: string) => void
}) {
  const s = t.tree
  const [remote, setRemote] = useState<Map<string, TreeNode>>(() => new Map())
  const { roots, alone } = useMemo(() => build(base, remote), [base, remote])
  // Какие ветки спросить у сервера. Ключ строкой, чтобы пересборка
  // дерева от ответа сервера не запрашивала его снова.
  const wanted = roots
    .filter((r) => r.foreign)
    .map((r) => r.id)
    .join(' ')
  useEffect(() => {
    if (!wanted) return
    let current = true
    // Молча: без ответа дерево остаётся тем, что знает снимок, — честно
    // неполным, а не сломанным.
    void Promise.all(wanted.split(' ').map((id) => loadTree(id).catch(() => ({ nodes: [] as TreeNode[] })))).then(
      (answers) => {
        if (!current) return
        const next = new Map<string, TreeNode>()
        for (const a of answers) for (const n of a.nodes) next.set(n.id, n)
        setRemote(next)
      },
    )
    return () => {
      current = false
    }
    // Связи — тоже повод спросить заново: часть привязали или отвязали.
  }, [wanted, base.links])
  // Свёрнутое — локально: вид раскрыт целиком по умолчанию, дерево
  // смотрят, чтобы увидеть всё сразу.
  const [folded, setFolded] = useState<Set<string>>(() => new Set())
  const toggle = (id: string) =>
    setFolded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const row = (node: Node, depth: number) => {
    const open = !folded.has(node.id)
    const progress = node.own ? (progressLabel(node.own, unit) ?? '') : ''
    return (
      <li key={node.id}>
        <div className="tree-row" style={{ paddingInlineStart: `calc(${depth} * var(--space-5))` }}>
          {node.children.length > 0 ? (
            <button
              type="button"
              className="tree-toggle"
              aria-expanded={open}
              aria-label={s.toggle(node.title, open)}
              onClick={() => toggle(node.id)}
            >
              {open ? '▾' : '▸'}
            </button>
          ) : (
            <span className="tree-toggle" aria-hidden="true" />
          )}
          {node.number && <span className="card-number">{node.number}</span>}
          {node.own ? (
            <button type="button" className="link tree-title" onClick={() => onOpenCard(node.id)}>
              {node.title}
            </button>
          ) : node.boardId ? (
            // Карточка другой доски открывается на своей доске: здесь
            // её панели нет. Ссылкой — чтобы открывалась и в новой
            // вкладке, как звено пути до корня.
            <a
              className="link tree-title"
              href={boardPath(node.boardId, node.id)}
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
                e.preventDefault()
                navigate(boardPath(node.boardId ?? '', node.id))
              }}
            >
              {node.title}
            </a>
          ) : (
            <span className="tree-title">{node.title}</span>
          )}
          {node.blocked && <span className="mark mark--alarm">{s.blocked}</span>}
          {node.done && <span className="mark">{s.done}</span>}
          <span className="muted small">{node.where}</span>
          {progress && <span className="muted small">{progress}</span>}
        </div>
        {open && node.children.length > 0 && <ul>{node.children.map((c) => row(c, depth + 1))}</ul>}
      </li>
    )
  }

  return (
    <div className="tree-view stack">
      <p className="muted small">{s.intro}</p>
      {roots.length === 0 ? (
        <p className="muted">{s.empty}</p>
      ) : (
        <ul className="tree" aria-label={s.label}>
          {roots.map((r) => row(r, 0))}
        </ul>
      )}
      {alone.length > 0 && (
        <details className="tree-alone">
          <summary className="small">{s.alone(alone.length)}</summary>
          <ul className="tree">{alone.map((r) => row(r, 0))}</ul>
        </details>
      )}
    </div>
  )
}

function build(base: BaseState, remote: Map<string, TreeNode>): { roots: Node[]; alone: Node[] } {
  const kids = new Map<string, string[]>()
  const parentOf = new Map<string, string>()
  const link = (parent: string, child: string) => {
    if (parentOf.has(child)) return
    parentOf.set(child, parent)
    kids.set(parent, [...(kids.get(parent) ?? []), child])
  }
  for (const l of base.links) if (l.kind === 'subtask') link(l.fromCard, l.toCard)
  // Сервер добавляет только части чужих карточек: части своих снимок
  // знает целиком, и знает свежее.
  for (const n of remote.values()) if (n.parentId && !base.cards[n.parentId]) link(n.parentId, n.id)

  const byNumber = (a: Node, b: Node) =>
    (a.number ?? '').localeCompare(b.number ?? '', locale(), { numeric: true }) ||
    a.title.localeCompare(b.title, locale())

  const node = (id: string, seen: Set<string>): Node => {
    const own = base.cards[id] ?? null
    const far = own ? undefined : remote.get(id)
    const foreign = own ? null : base.linked[id]
    // Цикл в связях не должен уводить отрисовку в бесконечность:
    // карточка, уже встреченная на пути, дальше не раскрывается.
    const next = new Set(seen).add(id)
    const children = !seen.has(id) ? (kids.get(id) ?? []).map((c) => node(c, next)).sort(byNumber) : []
    const visible = own !== null || foreign !== undefined || far?.visible === true
    const boardName = foreign?.boardName ?? far?.boardName
    const boardId = own ? null : visible ? (foreign?.boardId ?? far?.boardId ?? null) : null
    return {
      id,
      title: own?.title ?? foreign?.title ?? far?.title ?? t.card.unavailable,
      number: own?.number ?? far?.number ?? null,
      own,
      where: own
        ? (base.columns[own.columnId]?.name ?? '')
        : visible && boardName
          ? [t.card.onBoard(boardName), far?.columnName].filter(Boolean).join(' · ')
          : t.card.hiddenTeam,
      boardId,
      done: own ? own.outcome === 'done' || own.doneAt !== null : far?.done === true,
      blocked: own ? Boolean(own.blocked) : far?.blocked === true,
      foreign: !own || children.some((c) => c.foreign),
      children,
    }
  }

  const roots: Node[] = []
  const alone: Node[] = []
  // Корни — карточки доски без родителя и чужие карточки на вершине
  // ветки, в которой лежат карточки доски. Чужой родитель, над которым
  // кто-то известен, — не корень: он уже стоит веткой под своим родителем.
  const foreignParents = new Set<string>()
  for (const card of Object.values(base.cards)) {
    const path = new Set([card.id])
    let top = card.id
    for (let up = parentOf.get(top); up !== undefined && !path.has(up); up = parentOf.get(top)) {
      path.add(up)
      top = up
    }
    if (!base.cards[top]) foreignParents.add(top)
  }
  for (const id of foreignParents) roots.push(node(id, new Set()))
  for (const card of Object.values(base.cards)) {
    if (parentOf.has(card.id)) continue
    const n = node(card.id, new Set())
    // Карточка без родителя и без частей — не дерево: она уходит
    // в свёрнутый список, иначе иерархию не разглядеть за одиночками.
    if (n.children.length === 0) alone.push(n)
    else roots.push(n)
  }
  return { roots: roots.sort(byNumber), alone: alone.sort(byNumber) }
}
