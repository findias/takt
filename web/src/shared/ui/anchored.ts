import { useCallback, useLayoutEffect, useState } from 'react'
import type { RefObject } from 'react'

/**
 * Всплывающее, привязанное к кнопке: меню действий и выбор метки.
 *
 * Вынесено из меню, когда появился второй такой список: привязку,
 * которую дал бы anchor positioning (он ещё не везде), делает пересчёт
 * координат на открытие, прокрутку и изменение размера, и держать два
 * списка этого пересчёта значило бы ждать, когда они разойдутся.
 *
 * Позиционирование — верхний слой (`popover`) и координаты, считанные
 * от кнопки. Обычный absolute внутри обёртки не годится: колонка
 * карточек прокручивается сама, и у нижней карточки список не просто
 * обрезался — в точке пунктов лежала колонка, и они не нажимались вовсе.
 */

/** Верхний слой есть не везде — в разборе разметки для проверок его нет.
 *  Там признак не ставится вовсе: иначе стилевое правило «закрытое
 *  всплывающее не показывать» спрячет список, а показать его нечем.
 *  Без верхнего слоя остаётся `position: fixed` — оно тоже уходит
 *  из прокрутки, только его обрезает содержимое с `contain`. */
export const topLayer =
  typeof HTMLElement !== 'undefined' && typeof HTMLElement.prototype.showPopover === 'function'

export type AnchorBox = { top: number; left: number; up: boolean }

/**
 * Координаты всплывающего от кнопки.
 *
 * Пока координаты не сосчитаны, возвращается `null`, и показывать
 * нечего: список стоял бы не на месте ровно один кадр, и это видно
 * глазом. Прячут его прозрачностью, а не `visibility`: невидимое
 * по `visibility` нельзя сфокусировать, и первый пункт молча оставался
 * бы без фокуса.
 *
 * `align` и `drop` — пожелания, а не приказы: если с той стороны места
 * нет, список развернётся к другой.
 */
export function useAnchored(
  open: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  floatRef: RefObject<HTMLElement | null>,
  align: 'left' | 'right',
  drop: 'down' | 'up',
): AnchorBox | null {
  const [box, setBox] = useState<AnchorBox | null>(null)

  /** Отступ между кнопкой и списком задан в разметке, чтобы слушаться
   *  плотности; здесь — только край окна, от которого нельзя уехать:
   *  за ним список не достать ничем. */
  const place = useCallback(() => {
    const anchorNode = anchorRef.current
    const float = floatRef.current
    if (!anchorNode || !float) return
    const anchor = anchorNode.getBoundingClientRect()
    const { width, height } = float.getBoundingClientRect()
    const edge = 8

    const below = window.innerHeight - anchor.bottom
    const above = anchor.top
    // Пожелание слушается, пока с той стороны есть место; когда места
    // нет ни с той, ни с другой — выбирается сторона побольше.
    const up = drop === 'up' ? above >= height || above > below : below < height && above > below
    const top = up
      ? Math.max(edge, anchor.top - height)
      : Math.min(anchor.bottom, window.innerHeight - height - edge)

    const wanted = align === 'left' ? anchor.left : anchor.right - width
    const left = Math.max(edge, Math.min(wanted, window.innerWidth - width - edge))

    setBox({ top, left, up })
  }, [align, drop, anchorRef, floatRef])

  // Верхний слой и координаты — до отрисовки: `useLayoutEffect`
  // успевает пересчитать состояние прежде, чем кадр покажут.
  useLayoutEffect(() => {
    if (!open) {
      setBox(null)
      return
    }
    const float = floatRef.current
    if (!float) return
    if (topLayer && !float.matches(':popover-open')) float.showPopover()
    place()
    const again = () => place()
    // `capture` — прокрутка колонки карточек не всплывает до окна.
    window.addEventListener('scroll', again, true)
    window.addEventListener('resize', again)
    // Содержимое меняет высоту — выбор метки сужается по набранному, —
    // и список у нижнего края обязан переехать вместе с ней.
    const resized = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(again)
    resized?.observe(float)
    return () => {
      window.removeEventListener('scroll', again, true)
      window.removeEventListener('resize', again)
      resized?.disconnect()
    }
  }, [open, place, floatRef])

  return box
}
