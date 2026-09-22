import { request } from '../../shared/api/index.ts'
import type { ImportAnswer, ImportReport, ImportRequest } from '../../shared/api/index.ts'

/**
 * Клиент API переноса. Отдельно от общего `api`: экран переноса
 * открывают раз в жизни организации, а общий клиент едет в первую
 * загрузку каждого экрана — и порог её размера (perf.spec.ts) эти
 * пять вызовов уже переходили.
 */
const IMPORT_TIMEOUT_MS = 120_000

export const importApi = {
  /** Импорт из таблицы: предпросмотр (`apply: false`) и перенос —
   *  один и тот же запрос. Файл — base64; ждём дольше обычного:
   *  перенос тысяч строк идёт, пока человек смотрит на кнопку. */
  importTable: (body: ImportRequest) =>
    request<ImportAnswer>('POST', '/api/import/table', body, false, IMPORT_TIMEOUT_MS),
  /** YouGile (ROADMAP 23.3): пароль едет только в первые два запроса,
   *  дальше — ключ; ни то ни другое сервер не хранит. */
  yougileCompanies: (login: string, password: string) =>
    request<{ companies: { id: string; name: string }[] }>('POST', '/api/import/yougile/companies', {
      login,
      password,
    }),
  yougileKey: (login: string, password: string, companyId: string) =>
    request<{ key: string; created: boolean }>('POST', '/api/import/yougile/key', {
      login,
      password,
      companyId,
    }),
  yougileBoards: (key: string) =>
    request<{ boards: { id: string; title: string; project: string }[] }>(
      'POST',
      '/api/import/yougile/boards',
      { key },
      false,
      IMPORT_TIMEOUT_MS,
    ),
  yougileImport: (body: {
    key: string
    board: string
    boardId?: string
    newBoardName?: string
    columns?: Record<string, string>
    apply: boolean
  }) =>
    request<{ report: ImportReport }>('POST', '/api/import/yougile', body, false, IMPORT_TIMEOUT_MS),
}
