import { useRef } from 'react'
import {
  PANEL_WIDTH_DEFAULT,
  PANEL_WIDTH_MAX,
  PANEL_WIDTH_MIN,
  PANEL_WIDTH_STEP,
  clampPanelWidth,
} from './panelWidth.ts'
import { t } from '../i18n/index.ts'

/** Ширина едет переменной на корне: от неё зависят сама панель, место,
 *  которое ей уступает доска, и сдвиг сообщений. */
export function showPanelWidth(rem: number | null) {
  const root = document.documentElement.style
  if (rem === null) root.removeProperty('--panel-side-size')
  else root.setProperty('--panel-side-size', `${rem}rem`)
}

/**
 * Ручка ширины боковой панели — на её левой кромке.
 *
 * Устроена как ручка колонки (`ColumnResizer.tsx`), только зеркально:
 * кромка слева, и панель шире, когда ручку ведут влево. Стрелки — в ту же
 * сторону, что и мышь. Во время перетаскивания ширина едет переменной,
 * сохраняется один раз на отпускании; двойной щелчок возвращает исходную.
 */
export function PanelResizer({
  width,
  onCommit,
}: {
  width: number | null
  onCommit: (rem: number | null) => void
}) {
  const current = width ?? PANEL_WIDTH_DEFAULT
  const drag = useRef<{ x: number; from: number; now: number; px: number } | null>(null)

  const step = (delta: number) => {
    const next = clampPanelWidth(current + delta)
    if (next !== current) onCommit(next)
  }

  return (
    <div
      className="panel-resizer"
      role="separator"
      aria-orientation="vertical"
      aria-label={t.ui.panelWidth}
      aria-valuenow={current}
      aria-valuemin={PANEL_WIDTH_MIN}
      aria-valuemax={PANEL_WIDTH_MAX}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft') {
          e.preventDefault()
          step(PANEL_WIDTH_STEP)
        }
        if (e.key === 'ArrowRight') {
          e.preventDefault()
          step(-PANEL_WIDTH_STEP)
        }
      }}
      onDoubleClick={() => onCommit(null)}
      onPointerDown={(e) => {
        if (e.button !== 0) return
        e.preventDefault()
        const px = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
        drag.current = { x: e.clientX, from: current, now: current, px }
        e.currentTarget.setPointerCapture?.(e.pointerId)
      }}
      onPointerMove={(e) => {
        const d = drag.current
        if (!d) return
        d.now = clampPanelWidth(Math.round((d.from + (d.x - e.clientX) / d.px) * 4) / 4)
        showPanelWidth(d.now)
      }}
      onPointerUp={(e) => {
        const d = drag.current
        if (!d) return
        drag.current = null
        e.currentTarget.releasePointerCapture?.(e.pointerId)
        if (d.now !== d.from) onCommit(d.now)
      }}
      onPointerCancel={() => {
        const d = drag.current
        drag.current = null
        if (d) showPanelWidth(width)
      }}
    />
  )
}
