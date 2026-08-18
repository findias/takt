// Названия того, что показывается человеку.
//
// Отдельным модулем, а не в index.ts: разбор записей журнала читает
// их из проверок, которые идут без сборщика, а index.ts тянет за собой
// весь клиент. Список при этом остаётся один — index.ts его реэкспортирует.

import type { Role, Visibility } from './index.ts'

export const ROLE_NAMES: Record<Role, string> = {
  owner: 'Владелец',
  member: 'Участник',
  viewer: 'Наблюдатель',
}

export const VISIBILITY_NAMES: Record<Visibility, string> = {
  org: 'Всей организации',
  team: 'Своей команде',
  private: 'Только вписанным',
}
