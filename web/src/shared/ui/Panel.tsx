import { useCallback, useEffect, useRef, useState } from 'react'
import { useEscape } from '../lib/useEscape.ts'

/**
 * Панель в трёх режимах.
 *
 * Так сошлись Notion и ClickUp независимо друг от друга: сбоку, по центру,
 * во весь экран. Режим выбирает не разработчик, а тот, кто смотрит: одна
 * и та же карточка нужна то краем глаза рядом с доской, то целиком.
 *
 * Режим — не ширина. Боковая панель оставляет доску рабочей: её можно
 * листать и перетаскивать в ней карточки, поэтому она `aside` и фокус
 * не запирает. Центральная и полноэкранная доску перекрывают — значит,
 * это диалог, и у него появляются обязанности: `aria-modal`, ловушка
 * фокуса и возврат фокуса туда, откуда пришли. Сделать вид, что разница
 * только в размерах, — обычный способ получить панель, из которой
 * не выбраться с клавиатуры.
 */
export type PanelMode = 'side' | 'center' | 'full'

const MODES: Record<PanelMode, string> = {
  side: 'Сбоку',
  center: 'По центру',
  full: 'Во весь экран',
}

/** Режим запоминается: переключать его каждый раз никто не станет. */
export function usePanelMode(): [PanelMode, (mode: PanelMode) => void] {
  const [mode, setMode] = useState<PanelMode>(
    () => (localStorage.getItem('panel-mode') as PanelMode) ?? 'side',
  )
  const change = useCallback((next: PanelMode) => {
    setMode(next)
    localStorage.setItem('panel-mode', next)
  }, [])
  return [mode, change]
}

export function Panel({
  mode,
  onMode,
  title,
  label,
  onClose,
  actions,
  children,
}: {
  mode: PanelMode
  onMode: (mode: PanelMode) => void
  title: string
  label: string
  onClose: () => void
  actions?: React.ReactNode
  children: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  const modal = mode !== 'side'

  useEscape(onClose)

  // Фокус запирается только в модальном режиме: в боковой панели доска
  // остаётся рабочей, и запирать его там значило бы отнять её.
  useEffect(() => {
    if (!modal) return
    const returnTo = document.activeElement as HTMLElement | null
    const panel = ref.current
    panel?.focus()

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || !panel) return
      const focusable = panel.querySelectorAll<HTMLElement>(
        'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])',
      )
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      // Возврат фокуса туда, откуда пришли: иначе после закрытия он
      // улетает в начало страницы, и человек теряет место.
      returnTo?.focus()
    }
  }, [modal])

  const body = (
    <div
      ref={ref}
      className={`panel-card panel-card--${mode}`}
      role={modal ? 'dialog' : undefined}
      aria-modal={modal || undefined}
      aria-label={label}
      tabIndex={modal ? -1 : undefined}
    >
      <header className="panel-head">
        {/* Название карточки — заголовок, а не подпись раздела. Раньше
            он шёл тем же мелким капслоком, что и «ПОДЗАДАЧИ», и читался
            как служебная метка. */}
        <h2 className="panel-title">{title}</h2>
        <div className="row row--tight">
          {actions}
          <select
            value={mode}
            aria-label="Как показывать панель"
            onChange={(e) => onMode(e.target.value as PanelMode)}
          >
            {(Object.keys(MODES) as PanelMode[]).map((m) => (
              <option key={m} value={m}>
                {MODES[m]}
              </option>
            ))}
          </select>
          <button className="link" onClick={onClose}>
            Закрыть
          </button>
        </div>
      </header>
      {children}
    </div>
  )

  if (!modal) return <aside className="panel-side">{body}</aside>

  return (
    <div className="panel-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      {body}
    </div>
  )
}
