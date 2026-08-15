import { useEffect } from 'react'

/**
 * Закрытие по Escape.
 *
 * Боковая панель не запирает фокус намеренно — доска за ней остаётся
 * рабочей, как side peek у Notion. Но выйти из неё клавишей человек
 * обязан, иначе панель превращается в ловушку для того, кто пришёл
 * без мыши.
 */
export function useEscape(onEscape: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onEscape()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onEscape])
}
