/**
 * Метка среды (ROADMAP 36.3): «DEV», «ИФТ», «ПРОМ».
 *
 * Установок у заказчика несколько, и выпущенный образ на всех один:
 * различаются они адресом в строке браузера и ничем больше, а снимок
 * с одной легко принять за другую. Метку задаёт тот, кто ставит
 * (`STAND_LABEL`), сервер отдаёт её вместе со способами входа — она
 * нужна и на экране входа, до того как кто-то вошёл.
 *
 * Хранится здесь, а не в контексте React: её читает заголовок
 * вкладки, которому контекст недоступен, и меняется она один раз
 * за жизнь страницы.
 */
let label = ''

const prefixOf = (l: string) => `[${l}] `

export function environmentLabel(): string {
  return label
}

/**
 * Запомнить метку и поправить заголовок вкладки: он мог быть выставлен
 * до ответа сервера — без метки или, в проверках, с прежней.
 */
export function setEnvironmentLabel(next: string): void {
  const was = label
  label = next.trim()
  if (typeof document === 'undefined') return
  let title = document.title
  if (was && title.startsWith(prefixOf(was))) title = title.slice(prefixOf(was).length)
  document.title = withEnvironment(title)
}

/**
 * Заголовок вкладки с меткой впереди: «[DEV] ПОСТ-14 · Доска». Метка —
 * первым словом нарочно: вкладка показывает начало заголовка, а вопрос
 * «какой это стенд» задают, глядя на ряд одинаковых вкладок.
 */
export function withEnvironment(title: string): string {
  if (!label) return title
  return title.startsWith(prefixOf(label)) ? title : prefixOf(label) + title
}
