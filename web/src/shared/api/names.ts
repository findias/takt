// Названия того, что показывается человеку.
//
// Отдельным модулем, а не в index.ts: разбор записей журнала читает
// их из проверок, которые идут без сборщика, а index.ts тянет за собой
// весь клиент. Список при этом остаётся один — index.ts его реэкспортирует.
// Сами слова живут в каталоге языка (`shared/i18n`, раздел `names`).

import type { Role, Visibility } from './index.ts'
import { live, t } from '../i18n/index.ts'

export const ROLE_NAMES = live(() => ({
  owner: t.names.owner,
  member: t.names.member,
  viewer: t.names.viewer,
})) as Record<Role, string>

export const VISIBILITY_NAMES = live(() => ({
  org: t.names.org,
  team: t.names.team,
  private: t.names.private,
})) as Record<Visibility, string>
