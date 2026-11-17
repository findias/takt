import type { BaseState } from '../../entities/board/model.ts'
import type { Card, TreeNode } from '../../shared/api/index.ts'
import { locale, t } from '../../shared/i18n/index.ts'

export type WorkNode = {
  id: string
  title: string
  number: string | null
  own: Card | null
  /** Где карточка: колонка этой доски или чужая доска. */
  where: string
  /** Доска чужой карточки, если она видна: туда ведёт её название. */
  boardId: string | null
  priority?: Card['priority']
  done: boolean
  blocked: boolean
  /** Есть ли в ветке карточки других досок — тогда её досчитывает сервер. */
  foreign: boolean
  children: WorkNode[]
}

export function buildTree(base: BaseState, remote: Map<string, TreeNode>): { roots: WorkNode[]; alone: WorkNode[] } {
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

  const byNumber = (a: WorkNode, b: WorkNode) =>
    (a.number ?? '').localeCompare(b.number ?? '', locale(), { numeric: true }) ||
    a.title.localeCompare(b.title, locale())

  const node = (id: string, seen: Set<string>): WorkNode => {
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
      priority: own?.priority ?? foreign?.priority ?? far?.priority,
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

  const roots: WorkNode[] = []
  const alone: WorkNode[] = []
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
