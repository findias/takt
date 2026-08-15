import { useSyncExternalStore } from 'react'
import { parseRoute } from './model.ts'
import type { Route } from './model.ts'

export { parseRoute, routePath, boardPath } from './model.ts'
export type { Route } from './model.ts'

/**
 * Подписка на адрес.
 *
 * useSyncExternalStore, а не useState с эффектом: адрес — внешнее
 * состояние браузера, и React про него знать не обязан. Эффект,
 * читающий location и кладущий его в состояние, даёт лишний кадр
 * со старым значением и расходится при быстрых переходах.
 */
const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  window.addEventListener('popstate', listener)
  return () => {
    listeners.delete(listener)
    window.removeEventListener('popstate', listener)
  }
}

// Снимок — строка, а не объект: объект пересоздавался бы на каждом
// чтении, и useSyncExternalStore ушёл бы в бесконечный цикл.
function snapshot(): string {
  return window.location.pathname + window.location.search
}

export function useRoute(): Route {
  const path = useSyncExternalStore(subscribe, snapshot, () => '/')
  return parseRoute(path.split('?')[0])
}

/** Переход. replace — для случаев, когда возвращаться некуда: например,
 *  приглашение, которое уже принято. */
export function navigate(path: string, options: { replace?: boolean } = {}): void {
  if (path === snapshot()) return
  if (options.replace) window.history.replaceState(null, '', path)
  else window.history.pushState(null, '', path)
  // pushState не будит popstate — будим сами, иначе переход увидит
  // только тот, кто его совершил.
  for (const listener of [...listeners]) listener()
}

/** Параметры запроса: фильтры и группировка будут жить здесь. */
export function useQuery(): URLSearchParams {
  const path = useSyncExternalStore(subscribe, snapshot, () => '/')
  return new URLSearchParams(path.split('?')[1] ?? '')
}

export function setQuery(next: URLSearchParams, options: { replace?: boolean } = {}): void {
  const query = next.toString()
  navigate(window.location.pathname + (query ? `?${query}` : ''), options)
}
