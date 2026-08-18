// Чистая часть экрана структуры: дерево из плоского списка, правила
// переноса и подписи. Всё, что можно проверить без браузера и без сети,
// вынесено сюда — по той же причине, что и boardModel.

import { plural } from '../../shared/lib/plural.ts'
import type { Team } from '../../shared/api/index.ts'

/**
 * Предел вложенности подразделений. Держит его база; здесь значение нужно,
 * чтобы не предлагать заведомо невозможное.
 *
 * Живёт оно в модели, а не в клиенте API, по прозаической причине: модель
 * берёт из `api.ts` только типы, а они стираются при сборке. Значение
 * потянуло бы за собой весь модуль вместе с сетевым кодом — тестам он
 * не нужен, и раннер его даже не разберёт.
 */
export const MAX_TEAM_DEPTH = 5

export type TreeNode = Team & { children: TreeNode[] }

/**
 * Собирает дерево из плоского списка.
 *
 * Сервер отдаёт список, а не дерево, намеренно: так один и тот же ответ
 * годится и для дерева, и для выпадающего списка, и для подсчётов. Порядок
 * ветвей — по названию, чтобы список не перетасовывался при каждом ответе.
 *
 * Узел, чей родитель не пришёл, считается корневым. Так бывает не от ошибки:
 * родитель может быть не виден спрашивающему.
 */
export function buildTree(teams: Team[]): TreeNode[] {
  const byId = new Map<string, TreeNode>()
  for (const team of teams) byId.set(team.id, { ...team, children: [] })

  const roots: TreeNode[] = []
  for (const node of byId.values()) {
    const parent = node.parentId ? byId.get(node.parentId) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }

  const sort = (nodes: TreeNode[]) => {
    nodes.sort((a, b) => a.name.localeCompare(b.name, 'ru'))
    for (const node of nodes) sort(node.children)
  }
  sort(roots)
  return roots
}

/** Все узлы поддерева, включая корень: перенос запрещён внутрь себя. */
export function subtreeIds(node: TreeNode): string[] {
  return [node.id, ...node.children.flatMap(subtreeIds)]
}

/** Высота поддерева в уровнях: у листа единица. */
export function height(node: TreeNode): number {
  return 1 + Math.max(0, ...node.children.map(height))
}

/**
 * Куда можно перенести узел.
 *
 * Три запрета, и все три — не выдумка интерфейса, а правила базы:
 * внутрь себя нельзя (цикл), к прежнему родителю незачем, и глубже
 * предела нельзя — причём считать надо не по самому узлу, а по высоте
 * его поддерева: перенос тащит за собой всех потомков.
 */
export function allowedParents(tree: TreeNode[], node: TreeNode): TreeNode[] {
  const forbidden = new Set(subtreeIds(node))
  const tall = height(node)
  const out: TreeNode[] = []

  const walk = (nodes: TreeNode[]) => {
    for (const candidate of nodes) {
      if (!forbidden.has(candidate.id) && candidate.depth + tall <= MAX_TEAM_DEPTH) {
        out.push(candidate)
      }
      walk(candidate.children)
    }
  }
  walk(tree)
  return out
}

/** Можно ли завести подразделение внутри этого узла. */
export function canNestInside(node: TreeNode): boolean {
  return node.depth < MAX_TEAM_DEPTH
}

/** «3 команды», «1 доска» — короткие подписи под названием узла. */
export function counters(node: TreeNode): string {
  const parts: string[] = []
  if (node.members > 0) parts.push(`${node.members} ${plural(node.members, 'человек', 'человека', 'человек')}`)
  if (node.boards > 0) parts.push(`${node.boards} ${plural(node.boards, 'доска', 'доски', 'досок')}`)
  return parts.join(' · ')
}

