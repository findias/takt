import { useLayoutEffect } from 'react'
import type { RefObject } from 'react'

/**
 * Высота текстового поля — по написанному (ROADMAP 37.3).
 *
 * Обычно это делает CSS: `field-sizing: content` у всех textarea
 * (styles.css). Но свойство молодое — Chrome с 123, остальные к лету
 * 2026, — а на рабочих местах заказчика браузер бывает старше, и там
 * поле стояло высотой в три строки с прокруткой внутри: длинную причину
 * блокировки было не прочитать целиком, пока набираешь. Где свойства
 * нет, высоту подгоняет этот хук; где есть — не делает ничего.
 *
 * Пересчёт — по значению, а не по событию ввода: поле очищают и
 * программно (заведённая карточка), и высота обязана вернуться.
 */
const native = typeof CSS !== 'undefined' && typeof CSS.supports === 'function' && CSS.supports('field-sizing', 'content')

export function useAutoGrow(ref: RefObject<HTMLTextAreaElement | null>, value: string): void {
  useLayoutEffect(() => {
    const el = ref.current
    if (native || !el) return
    el.style.height = 'auto'
    // Граница входит в высоту при box-sizing: border-box, а scrollHeight
    // её не считает — без поправки последняя строка срезается на пиксель.
    const border = el.offsetHeight - el.clientHeight
    el.style.height = `${el.scrollHeight + Math.max(border, 0)}px`
  }, [ref, value])
}
