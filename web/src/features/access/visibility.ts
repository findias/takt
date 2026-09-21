import { VISIBILITY_NAMES } from '../../shared/api/index.ts'
import type { BoardAccess as Access } from '../../shared/api/index.ts'

// Подпись видимости живёт отдельно от панели доступа: подпись нужна
// в шапке каждой доски сразу, а панель — только когда её открыли,
// и едет отдельным куском.

/** Короткая подпись для шапки: кто видит доску прямо сейчас. */
export function visibilityLabel(access: Access | null): string {
  if (!access) return 'Доступ'
  if (access.visibility === 'team') {
    return access.teamName ? `Видна: ${access.teamName}` : 'Видна: команде'
  }
  if (access.visibility === 'private') {
    return `Видна: ${access.members.length} поимённо`
  }
  return `Видна: ${VISIBILITY_NAMES.org.toLowerCase()}`
}
