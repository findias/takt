// Клиент API. Одна точка входа, чтобы обработка ошибок и таймаутов
// не расползалась по компонентам.

export type Role = 'owner' | 'member' | 'viewer'

/** Кто выполняет запрос и от имени какой организации. */
export type Principal = {
  id: string
  email: string
  name: string
  orgId: string
  orgName: string
  orgSlug: string
  role: Role
}

export type Membership = {
  orgId: string
  orgName: string
  orgSlug: string
  role: Role
}

export type Member = {
  userId: string
  name: string
  email: string
  role: Role
  joinedAt: string
}

export type Invite = {
  id: string
  email: string
  role: Role
  expiresAt: string
  createdAt: string
  /** Приходит только в ответе на создание: в базе лежит лишь хеш токена. */
  link?: string
}

export type InviteInfo = {
  orgName: string
  email: string
  role: Role
  needsAccount: boolean
}

export const ROLE_NAMES: Record<Role, string> = {
  owner: 'Владелец',
  member: 'Участник',
  viewer: 'Наблюдатель',
}

/** Подразделение. Дерево строится по parentId; depth приходит с сервера,
 *  чтобы показать, что глубже вкладывать уже нельзя. */
export type Team = {
  id: string
  name: string
  parentId: string | null
  depth: number
  members: number
  boards: number
}

export type TeamMember = {
  userId: string
  name: string
  email: string
  role: TeamRole
  addedAt: string
}

export type TeamRole = 'lead' | 'member'

export const TEAM_ROLE_NAMES: Record<TeamRole, string> = {
  lead: 'Ведущий',
  member: 'Участник',
}

/** Наблюдение за поддеревом; без команды — за всей организацией. */
export type Observer = {
  id: string
  userId: string
  name: string
  email: string
  teamId: string | null
  teamName: string | null
}

export type Visibility = 'org' | 'team' | 'private'

export const VISIBILITY_NAMES: Record<Visibility, string> = {
  org: 'Всей организации',
  team: 'Своей команде',
  private: 'Только вписанным',
}

export type BoardAccess = {
  visibility: Visibility
  teamId: string | null
  teamName: string | null
  members: { userId: string; name: string; email: string }[]
}

export type BoardInfo = { id: string; name: string; version: number }
/** Вид колонки. Границы потока задаются отдельно: очередей и стадий работы
 *  бывает много, а началом и концом объявляются конкретные колонки. */
export type ColumnKind = 'queue' | 'in_progress' | 'done'

export type Column = {
  id: string
  name: string
  position: string
  kind: ColumnKind
  isStartedPoint: boolean
  isFinishedPoint: boolean
  policy: string
  wipLimit: number | null
  /** Мягкий лимит только подсвечивается, жёсткий отвечает конфликтом. */
  wipLimitHard: boolean
}

export type Card = {
  id: string
  columnId: string
  position: string
  title: string
  description: string
  version: number
  /** Момент входа в текущую колонку — из него считается старение карточки. */
  columnEnteredAt: string
  startedAt: string | null
  finishedAt: string | null
  /** Исход работы: done или discarded. Пока работа идёт — пусто.
   *  Отказ и завершение — разные факты: иначе пропускная способность
   *  считает выброшенное за сделанное. */
  outcome: 'done' | 'discarded' | null
  /** Прогресс по подзадачам; пусто, если подзадач нет. */
  progress?: { done: number; total: number }
  /** Открытая блокировка, если есть. */
  blocked?: { id: string; reason: string; blockedAt: string }
}

export type LinkKind = 'subtask' | 'blocks' | 'relates'

export const LINK_KIND_NAMES: Record<LinkKind, string> = {
  subtask: 'Подзадача',
  blocks: 'Блокирует',
  relates: 'Связана с',
}

export type Link = { fromCard: string; toCard: string; kind: LinkKind }

/** Карточка с другой доски: столько, сколько нужно, чтобы её узнать
 *  и понять, чья это команда. Недоступной доски здесь не будет. */
export type LinkedCard = {
  id: string
  title: string
  boardId: string
  boardName: string
  teamName: string | null
  outcome: 'done' | 'discarded' | null
  blocked: boolean
}

export type Snapshot = {
  board: BoardInfo
  columns: Column[]
  cards: Card[]
  links: Link[]
  linked: LinkedCard[]
}

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
  me: () => request<Principal>('GET', '/api/me'),
  login: (email: string, password: string) =>
    request<Principal>('POST', '/api/auth/login', { email, password }),
  register: (org: string, name: string, email: string, password: string) =>
    request<Principal>('POST', '/api/auth/register', { org, name, email, password }),
  logout: () => request<void>('POST', '/api/auth/logout'),

  listOrgs: () => request<{ orgs: Membership[]; activeOrgId: string }>('GET', '/api/orgs'),
  createOrg: (name: string) => request<Membership>('POST', '/api/orgs', { name }),
  switchOrg: (orgId: string) => request<Principal>('POST', '/api/session/org', { orgId }),

  team: () => request<{ members: Member[]; invites: Invite[] }>('GET', '/api/team'),
  invite: (email: string, role: Role) => request<Invite>('POST', '/api/invites', { email, role }),
  revokeInvite: (id: string) => request<void>('DELETE', `/api/invites/${id}`),
  setRole: (userId: string, role: Role) =>
    request<void>('PUT', `/api/members/${userId}/role`, { role }),
  removeMember: (userId: string) => request<void>('DELETE', `/api/members/${userId}`),

  inviteInfo: (token: string) => request<InviteInfo>('GET', `/api/invites/${token}/info`),
  acceptInvite: (token: string, account?: { name: string; password: string }) =>
    request<Principal>('POST', `/api/invites/${token}/accept`, account ?? {}),

  listTeams: () => request<{ teams: Team[] }>('GET', '/api/teams'),
  createTeam: (name: string, parentId: string | null) =>
    request<Team>('POST', '/api/teams', { name, parentId }),
  renameTeam: (id: string, name: string) => request<void>('PATCH', `/api/teams/${id}`, { name }),
  /** Перенос: родитель или явный корень — «оставить как есть» выражается
   *  тем, что запрос вообще не отправляется. */
  moveTeam: (id: string, parentId: string | null) =>
    request<void>('PATCH', `/api/teams/${id}`, parentId ? { parentId } : { root: true }),
  archiveTeam: (id: string) => request<void>('DELETE', `/api/teams/${id}`),

  teamMembers: (id: string) => request<{ members: TeamMember[] }>('GET', `/api/teams/${id}/members`),
  addTeamMember: (id: string, userId: string, role: TeamRole) =>
    request<void>('PUT', `/api/teams/${id}/members/${userId}`, { role }),
  removeTeamMember: (id: string, userId: string) =>
    request<void>('DELETE', `/api/teams/${id}/members/${userId}`),

  listObservers: () => request<{ observers: Observer[] }>('GET', '/api/observers'),
  grantObservation: (userId: string, teamId: string | null) =>
    request<Observer>('POST', '/api/observers', { userId, teamId }),
  revokeObservation: (id: string) => request<void>('DELETE', `/api/observers/${id}`),

  boardAccess: (boardId: string) => request<BoardAccess>('GET', `/api/boards/${boardId}/access`),
  setBoardAccess: (boardId: string, visibility: Visibility, teamId: string | null) =>
    request<void>('PUT', `/api/boards/${boardId}/access`, { visibility, teamId }),
  addBoardMember: (boardId: string, userId: string) =>
    request<void>('PUT', `/api/boards/${boardId}/members/${userId}`),
  removeBoardMember: (boardId: string, userId: string) =>
    request<void>('DELETE', `/api/boards/${boardId}/members/${userId}`),

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
