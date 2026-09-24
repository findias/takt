import { useMemo, useState } from 'react'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card, EstimateUnit } from '../../shared/api/index.ts'
import { progressLabel, subtreeLabel } from '../../entities/card/model.ts'
import { locale, t } from '../../shared/i18n/index.ts'

/**
 * Вид «Дерево» (этап 32.6): эпики, фичи и задачи доски — иерархией,
 * рядом с «Доской» и «Таблицей», а не поверх них. Кому нужна иерархия,
 * смотрит здесь; доска остаётся доской потока.
 *
 * Строится из снимка доски: карточки доски и связи «подзадача» с обеих
 * сторон. Карточка с другой доски стоит со своей подписью, и под ней
 * собраны её части с этой доски — так «эпик › релиз соседей › наша
 * задача» остаётся одной веткой. Корнем чужая карточка становится,
 * только если её родитель снимку неизвестен. Части чужой карточки,
 * лежащие на третьих досках, не видны: заглядывать в чужие доски снимок
 * не умеет, и это честная граница вида.
 *
 * Отбор доски здесь не действует: дерево без половины веток не
 * отвечает на вопрос «далеко ли до эпика».
 */

type Node = {
  id: string
  title: string
  number: string | null
  own: Card | null
  /** Где карточка: колонка этой доски или чужая доска. */
  where: string
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
  const { roots, alone } = useMemo(() => build(base), [base])
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
    const progress = node.own
      ? [progressLabel(node.own, unit), subtreeLabel(node.own, unit)].filter(Boolean).join(' · ')
      : ''
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
          ) : (
            <span className="tree-title">{node.title}</span>
          )}
          {node.own?.blocked && <span className="mark mark--alarm">{s.blocked}</span>}
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

function build(base: BaseState): { roots: Node[]; alone: Node[] } {
  const kids = new Map<string, string[]>()
  const parentOf = new Map<string, string>()
  for (const link of base.links) {
    if (link.kind !== 'subtask') continue
    parentOf.set(link.toCard, link.fromCard)
    kids.set(link.fromCard, [...(kids.get(link.fromCard) ?? []), link.toCard])
  }
  const byNumber = (a: Node, b: Node) =>
    (a.number ?? '').localeCompare(b.number ?? '', locale(), { numeric: true }) ||
    a.title.localeCompare(b.title, locale())

  const node = (id: string, seen: Set<string>): Node => {
    const own = base.cards[id] ?? null
    const foreign = own ? null : base.linked[id]
    // Цикл в связях не должен уводить отрисовку в бесконечность:
    // карточка, уже встреченная на пути, дальше не раскрывается.
    const next = new Set(seen).add(id)
    // Части чужой карточки тоже известны — те, что на этой доске:
    // связь с ними есть в снимке. Так «эпик › релиз соседей › наша задача»
    // собирается одной веткой, а не распадается на два корня.
    const children = !seen.has(id) ? (kids.get(id) ?? []).map((c) => node(c, next)).sort(byNumber) : []
    return {
      id,
      title: own?.title ?? foreign?.title ?? t.card.unavailable,
      number: own?.number ?? null,
      own,
      where: own
        ? (base.columns[own.columnId]?.name ?? '')
        : foreign
          ? t.card.onBoard(foreign.boardName)
          : t.card.hiddenTeam,
      children,
    }
  }

  const roots: Node[] = []
  const alone: Node[] = []
  // Корни — карточки доски без родителя и родители с других досок,
  // под которыми здесь лежат части. Чужой родитель — корень, только если над ним никого не известно:
  // иначе он уже стоит веткой под своим родителем.
  const foreignParents = new Set<string>()
  for (const card of Object.values(base.cards)) {
    const parent = parentOf.get(card.id)
    if (parent && !base.cards[parent] && !parentOf.has(parent)) foreignParents.add(parent)
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
