// Клиент API. Одна точка входа, чтобы обработка ошибок и таймаутов
// не расползалась по компонентам.

export type User = {
  id: string
  orgId: string
  email: string
  name: string
  role: 'owner' | 'member' | 'viewer'
}

export type BoardInfo = { id: string; name: string; version: number }
export type Column = { id: string; name: string; position: string; wipLimit: number | null }
export type Card = {
  id: string
  columnId: string
  position: string
  title: string
  description: string
  version: number
}

export type Snapshot = { board: BoardInfo; columns: Column[]; cards: Card[] }

export type Patch = {
  cards?: Card[]
  columns?: Column[]
  removedCardIds?: string[]
}
export type OperationResult = { version: number; patch: Patch }

export type Placement =
  | { place: 'start' }
  | { place: 'end' }
  | { place: 'after'; afterCardId: string }

/** Конфликт: операция опиралась на устаревшее представление доски. */
export type Conflict = {
  error: string
  columnId?: string
  currentOrder?: { id: string; position: string }[]
  version: number
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: any,
  ) {
    super(body?.error ?? `Ошибка ${status}`)
  }
  get isConflict() {
    return this.status === 409
  }
}

/** Сеть недоступна или сервер не ответил вовремя — повтор имеет смысл. */
export class NetworkError extends Error {
  constructor(message: string) {
    super(message)
  }
}

const TIMEOUT_MS = 10_000

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS)
  let response: Response
  try {
    response = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
      credentials: 'same-origin',
    })
  } catch (e) {
    throw new NetworkError(
      controller.signal.aborted ? 'Сервер не ответил за 10 секунд' : 'Нет связи с сервером',
    )
  } finally {
    clearTimeout(timer)
  }

  const text = await response.text()
  const parsed = text ? JSON.parse(text) : null
  if (!response.ok) throw new ApiError(response.status, parsed)
  return parsed as T
}

export const api = {
  me: () => request<User>('GET', '/api/me'),
  login: (email: string, password: string) =>
    request<User>('POST', '/api/auth/login', { email, password }),
  register: (org: string, name: string, email: string, password: string) =>
    request<User>('POST', '/api/auth/register', { org, name, email, password }),
  logout: () => request<void>('POST', '/api/auth/logout'),

  listBoards: () => request<{ boards: BoardInfo[] }>('GET', '/api/boards'),
  createBoard: (name: string) => request<BoardInfo>('POST', '/api/boards', { name }),
  snapshot: (boardId: string) => request<Snapshot>('GET', `/api/boards/${boardId}`),

  operation: (boardId: string, operationId: string, type: string, payload: unknown) =>
    request<OperationResult>('POST', `/api/boards/${boardId}/operations`, {
      operationId,
      type,
      payload,
    }),
}
