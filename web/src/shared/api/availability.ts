import { request } from './index.ts'

export type Vacation = { id: string; userId: string; startsOn: string; endsOn: string; note: string; cancelled: boolean; version: number }
export type Coverage = {
  id: string; vacationId: string; vacationVersion: number; userId: string
  cardId: string; cardNumber: string; cardTitle: string; boardId: string
  substituteId: string; startsOn: string; endsOn: string
  status: 'proposed' | 'accepted' | 'declined' | 'cancelled'
  version: number; valid: boolean; canManage: boolean
}
export type Availability = { today: string; timeZone: string; vacations: Vacation[]; coverages: Coverage[]; eligible: string[] }
export const availabilityAPI = {
  load: (board?: string) => request<Availability>('GET', '/api/availability' + (board ? `?board=${encodeURIComponent(board)}` : '')),
  save: (v: Vacation) => request<Vacation>('PUT', `/api/vacations/${v.id}`, v),
  propose: (c: Pick<Coverage, 'id' | 'vacationId' | 'vacationVersion' | 'cardId' | 'substituteId' | 'startsOn' | 'endsOn'>) => request<Coverage>('PUT', `/api/coverages/${c.id}`, c),
  respond: (c: Coverage, status: Coverage['status']) => request<Coverage>('PATCH', `/api/coverages/${c.id}`, { status, version: c.version }),
  zone: (timeZone: string) => request<void>('PUT', '/api/availability/timezone', { timeZone }),
}
