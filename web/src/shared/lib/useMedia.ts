import { useSyncExternalStore } from 'react'

/**
 * Совпадает ли медиазапрос прямо сейчас.
 *
 * useSyncExternalStore, а не useState с эффектом: ширина окна — внешнее
 * состояние, и эффект, читающий его после отрисовки, даёт лишний кадр
 * с прежней раскладкой. На узком экране это заметно — доска успевает
 * мигнуть широкой.
 *
 * Медиазапрос в JS нужен там, где меняется не оформление, а состав
 * разметки: на телефоне мы рисуем одну колонку вместо всех, и CSS
 * такого не умеет.
 */
export function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (listener) => {
      const media = window.matchMedia(query)
      media.addEventListener('change', listener)
      return () => media.removeEventListener('change', listener)
    },
    () => window.matchMedia(query).matches,
    // На сервере таких запросов нет, и «широкий экран» — честное
    // предположение по умолчанию: так раскладка не прыгает у тех,
    // у кого он и правда широкий.
    () => false,
  )
}

/** Телефон и узкое окно. Порог тот же, что у CSS-правил раскладки. */
export const NARROW = '(max-width: 40rem)'
