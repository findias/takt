import type { WorkNode } from './treeModel.ts'
import { locale } from '../../shared/i18n/index.ts'

export const TREE_COLUMNS = ['where', 'priority', 'progress'] as const
export type TreeColumn = (typeof TREE_COLUMNS)[number]

export function treeColumns(value: string | null): TreeColumn[] {
  if (value === null) return [...TREE_COLUMNS]
  return TREE_COLUMNS.filter((key) => value.split(',').includes(key))
}

/** Поиск оставляет путь к совпадению. Сортируются соседи: глобальная
 *  сортировка разорвала бы связь ребёнка с родителем. */
export function treeRows(roots: WorkNode[], closed: Set<string>, search: string, sort: string) {
  const needle = search.trim().toLocaleLowerCase(locale())
  const compare = (a: WorkNode, b: WorkNode) => {
    const key = sort === 'title' ? 'title' : 'number'
    return (a[key] ?? '').localeCompare(b[key] ?? '', locale(), { numeric: true }) || a.id.localeCompare(b.id)
  }
  const rows: { node: WorkNode; depth: number; context: boolean; open: boolean }[] = []
  const select = (nodes: WorkNode[]): WorkNode[] => nodes.flatMap((node) => {
    const children = select(node.children)
    const match = `${node.number ?? ''} ${node.title}`.toLocaleLowerCase(locale()).includes(needle)
    return match ? [node] : children.length ? [{ ...node, children }] : []
  })
  const visit = (nodes: WorkNode[], depth: number) => {
    for (const node of [...nodes].sort(compare)) {
      const open = needle !== '' || !closed.has(node.id)
      rows.push({ node, depth, open, context: needle !== '' && !`${node.number ?? ''} ${node.title}`.toLocaleLowerCase(locale()).includes(needle) })
      if (open) visit(node.children, depth + 1)
    }
  }
  visit(needle ? select(roots) : roots, 0)
  return rows
}

export function branchIds(roots: WorkNode[]): string[] {
  return roots.flatMap((node) => node.children.length ? [node.id, ...branchIds(node.children)] : [])
}
