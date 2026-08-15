/**
 * Разбор адреса.
 *
 * Своего маршрутизатора обычно не пишут, и правильно делают — но у нас
 * пять маршрутов без вложенности, а библиотека привозит своё
 * представление о загрузчиках данных, отложенных переходах и границах
 * ошибок, с которым потом приходится договариваться. Здесь всё, что
 * нужно, — разбор пути и его сборка обратно, и это чистые функции,
 * проверяемые без браузера.
 *
 * Адрес — это состояние: что открыто, у какой доски, какая карточка.
 * Правило простое: если после перезагрузки человек должен увидеть
 * то же самое, это живёт в адресе, а не в useState. До сих пор доска
 * не имела адреса вовсе — прислать на неё ссылку было нельзя,
 * а перезагрузка возвращала в список.
 */

export type Route =
  | { name: 'boards' }
  | { name: 'team' }
  | { name: 'structure' }
  | { name: 'board'; boardId: string; cardId: string | null }
  | { name: 'invite'; token: string }

/** Неизвестный адрес — это список досок, а не пустой экран. */
export function parseRoute(pathname: string): Route {
  const parts = pathname.split('/').filter(Boolean)

  if (parts[0] === 'invite' && parts[1]) return { name: 'invite', token: parts.slice(1).join('/') }
  if (parts[0] === 'team') return { name: 'team' }
  if (parts[0] === 'structure') return { name: 'structure' }
  if (parts[0] === 'board' && parts[1]) {
    const cardId = parts[2] === 'card' && parts[3] ? parts[3] : null
    return { name: 'board', boardId: parts[1], cardId }
  }
  return { name: 'boards' }
}

export function routePath(route: Route): string {
  switch (route.name) {
    case 'boards':
      return '/'
    case 'team':
      return '/team'
    case 'structure':
      return '/structure'
    case 'invite':
      return `/invite/${route.token}`
    case 'board':
      return route.cardId
        ? `/board/${route.boardId}/card/${route.cardId}`
        : `/board/${route.boardId}`
  }
}

export function boardPath(boardId: string, cardId: string | null = null): string {
  return routePath({ name: 'board', boardId, cardId })
}
