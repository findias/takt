/**
 * Инициалы для подписи карточки.
 *
 * Чистая функция и живёт отдельно от компонента: разметке она не нужна,
 * а проверять её удобнее без браузера. Пустое имя даёт вопрос, а не
 * пустоту — подпись без букв читается как сбой отрисовки.
 */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}
