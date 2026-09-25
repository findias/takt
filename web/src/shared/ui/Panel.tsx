import { useCallback, useEffect, useRef, useState } from 'react'
import { useEscape } from '../lib/useEscape.ts'
import { t } from '../i18n/index.ts'

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

const MODES: PanelMode[] = ['side', 'center', 'full']
const modeName = (m: PanelMode) =>
  m === 'side' ? t.ui.panelSide : m === 'center' ? t.ui.panelCenter : t.ui.panelFull

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
  heading,
  eyebrow,
  label,
  onClose,
  actions,
  children,
}: {
  mode: PanelMode
  onMode: (mode: PanelMode) => void
  title: string
  /** Правимый заголовок вместо текста; пусто — просто `title`. */
  heading?: React.ReactNode
  /** Строка над заголовком: чем эта панель открыта — номер задачи,
   *  например. Тише заголовка и не спорит с ним за место. */
  eyebrow?: React.ReactNode
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
        {/* «Как показывать» и «Закрыть» — строкой над названием
            (владелец 25.09.2026): в узкой боковой панели они
            переносились под название и терялись среди полей
            карточки. Первыми и в разметке — Tab приходит к закрытию
            раньше, чем к содержимому. */}
        <div className="row row--tight panel-controls">
          {actions}
          <select
            value={mode}
            aria-label={t.ui.panelMode}
            onChange={(e) => onMode(e.target.value as PanelMode)}
          >
            {MODES.map((m) => (
              <option key={m} value={m}>
                {modeName(m)}
              </option>
            ))}
          </select>
          <button className="link" onClick={onClose}>
            {t.common.close}
          </button>
        </div>
        {/* Название карточки — заголовок, а не подпись раздела. Раньше
            он шёл тем же мелким капслоком, что и «ПОДЗАДАЧИ», и читался
            как служебная метка. */}
        <div className="panel-heading">
          {eyebrow}
          {/* Заголовок можно отдать правимым: название карточки правят
              прямо здесь, а не только из меню карточки на доске. Кнопка
              правки — рядом с заголовком, а не им самим: заголовок-кнопка
              звучал бы с диктора «кнопка «Фича»» и спорил бы с кнопками
              подзадач того же имени. */}
          {heading ?? <h2 className="panel-title">{title}</h2>}
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
