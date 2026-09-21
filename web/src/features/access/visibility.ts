import { VISIBILITY_NAMES } from '../../shared/api/index.ts'
import type { BoardAccess as Access } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

// Подпись видимости живёт отдельно от панели доступа: подпись нужна
// в шапке каждой доски сразу, а панель — только когда её открыли,
// и едет отдельным куском.

/** Короткая подпись для шапки: кто видит доску прямо сейчас. */
export function visibilityLabel(access: Access | null): string {
  if (!access) return t.boards.access
  if (access.visibility === 'team') {
    return access.teamName ? t.boards.visibleTeam(access.teamName) : t.boards.visibleOwnTeam
  }
  if (access.visibility === 'private') {
    return t.boards.visibleListed(access.members.length)
  }
  return t.boards.visible(VISIBILITY_NAMES.org.toLowerCase())
}
