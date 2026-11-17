import { boardPath } from './model.ts'

/** Возврат разрешён только к экрану задач или карточке внутри приложения. */
export function parentReturn(value: string | null): string | null {
  if (!value || !/^\/(?:tasks(?:\?|$)|board\/[a-zA-Z0-9-]+\/card\/[a-zA-Z0-9-]+(?:\?|$))/.test(value)) return null
  if (value.includes('\\') || value.includes('#')) return null
  return value
}

export function parentPath(current: string, boardId: string, cardId: string, source: string): string {
  const [path, search = ''] = current.split('?')
  const previous = new URLSearchParams(search)
  const tasks = path === '/tasks'
  const query = tasks || path.startsWith(`/board/${boardId}/`) ? new URLSearchParams(previous) : new URLSearchParams()
  const back = parentReturn(previous.get('parentReturn')) ?? current
  query.set('parentReturn', back)
  query.set('parentOpen', `${boardId}:${cardId}`)
  query.set('parentFrom', source)
  if (tasks) query.set('open', `${boardId}:${cardId}`)
  return `${tasks ? path : boardPath(boardId, cardId)}?${query}`
}
