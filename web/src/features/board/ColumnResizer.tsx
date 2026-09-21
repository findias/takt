import { useRef } from 'react'
import type { RefObject } from 'react'
import {
  COLUMN_WIDTH_DEFAULT,
  COLUMN_WIDTH_MAX,
  COLUMN_WIDTH_MIN,
  COLUMN_WIDTH_STEP,
  clampColumnWidth,
} from './columnWidth.ts'

/**
 * Ручка ширины колонки.
 *
 * Орган управления, а не декоративная полоска: `separator` с именем
 * колонки, ширина и пределы — в его значениях, стрелки двигают шагом,
 * двойной щелчок возвращает исходную ширину. Видимая полоса тонкая,
 * зона нажатия — во всю цель нажатия: четырёхпиксельную полосу пальцем
 * не берут.
 *
 * Во время перетаскивания ширина едет CSS-переменной прямо на элементе
 * колонки, а сохраняется один раз — на отпускании. Через состояние
 * React на каждый `pointermove` доска перерисовывалась бы десятки раз
 * в секунду, и порог «правка одной карточки не перерисовывает соседние»
 * упал бы в первый же заход.
 */
export function ColumnResizer({
  name,
  width,
  columnRef,
  onCommit,
}: {
  name: string
  /** Своя ширина колонки в rem; `null` — не трогали. */
  width: number | null
  columnRef: RefObject<HTMLElement | null>
  onCommit: (rem: number | null) => void
}) {
  const current = width ?? COLUMN_WIDTH_DEFAULT
  const drag = useRef<{ x: number; from: number; now: number; px: number } | null>(null)

  const show = (rem: number) =>
    columnRef.current?.style.setProperty('--column-width', `${rem}rem`)

  const step = (delta: number) => {
    const next = clampColumnWidth(current + delta)
    if (next !== current) onCommit(next)
  }

  return (
    <div
      className="column-resizer"
      role="separator"
      aria-orientation="vertical"
      aria-label={`Ширина колонки «${name}»`}
      aria-valuenow={current}
      aria-valuemin={COLUMN_WIDTH_MIN}
      aria-valuemax={COLUMN_WIDTH_MAX}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'ArrowRight') {
          e.preventDefault()
          step(COLUMN_WIDTH_STEP)
        }
        if (e.key === 'ArrowLeft') {
          e.preventDefault()
          step(-COLUMN_WIDTH_STEP)
        }
      }}
      onDoubleClick={() => {
        columnRef.current?.style.removeProperty('--column-width')
        onCommit(null)
      }}
      onPointerDown={(e) => {
        if (e.button !== 0) return
        // Перенос карточки и выделение текста с ручки не начинаются.
        e.preventDefault()
        e.stopPropagation()
        // Пиксели в rem — по кеглю корня: ширину хранят в rem, чтобы
        // она росла вместе с текстом.
        const px = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
        drag.current = { x: e.clientX, from: current, now: current, px }
        e.currentTarget.setPointerCapture?.(e.pointerId)
      }}
      onPointerMove={(e) => {
        const d = drag.current
        if (!d) return
        // Четверть rem — четыре пикселя при обычном кегле: мельче глаз
        // не различает, а диктору «19,8125» ни о чём не говорит.
        d.now = clampColumnWidth(Math.round((d.from + (e.clientX - d.x) / d.px) * 4) / 4)
        show(d.now)
      }}
      onPointerUp={(e) => {
        const d = drag.current
        if (!d) return
        drag.current = null
        e.currentTarget.releasePointerCapture?.(e.pointerId)
        if (d.now !== d.from) onCommit(d.now)
      }}
      onPointerCancel={() => {
        // Отменённое перетаскивание возвращает то, что было.
        const d = drag.current
        drag.current = null
        if (d) show(d.from)
      }}
    />
  )
}
