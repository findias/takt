import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { CloseIcon } from './icons.tsx'
import { IconButton } from './Button.tsx'

/**
 * Сообщения о том, что произошло.
 *
 * Два правила, из-за которых это не просто «всплывашка».
 *
 * Первое: отмена вместо подтверждения. Диалог «вы уверены?» закрывают
 * не читая — он стоит на пути и потому воспринимается как помеха.
 * Обратимое действие с предложением отменить дешевле для всех:
 * обычный случай проходит без единого лишнего нажатия, а редкая ошибка
 * исправляется одним. Поэтому у сообщения есть действие и время жизни
 * подлиннее.
 *
 * Второе: role="status", а не alert. Alert перебивает чтение того, что
 * человек читает прямо сейчас, и потому предназначен для настоящих
 * тревог. «Карточка перенесена» такой тревогой не является.
 */

export type ToastAction = { label: string; onAct: () => void }

export type Toast = {
  id: string
  text: string
  tone: 'info' | 'warning'
  action?: ToastAction
}

type Push = (toast: Omit<Toast, 'id'>) => void

const ToastContext = createContext<Push | null>(null)

/** Сколько живёт сообщение. С действием — дольше: его надо успеть
 *  прочитать и нажать. */
const LIFE = { plain: 5000, withAction: 10_000 }

/** Больше трёх разом — это уже стена, которую не читают. */
const MAX_VISIBLE = 3

export function ToastHost({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const timers = useRef(new Map<string, number>())

  const dismiss = useCallback((id: string) => {
    setToasts((list) => list.filter((t) => t.id !== id))
    const timer = timers.current.get(id)
    if (timer) {
      window.clearTimeout(timer)
      timers.current.delete(id)
    }
  }, [])

  const push = useCallback<Push>(
    (toast) => {
      const id = crypto.randomUUID()
      setToasts((list) => [...list, { ...toast, id }].slice(-MAX_VISIBLE))
      const life = toast.action ? LIFE.withAction : LIFE.plain
      timers.current.set(id, window.setTimeout(() => dismiss(id), life))
    },
    [dismiss],
  )

  // Таймеры переживают размонтирование, если их не убрать: сообщение
  // исчезнет, а таймер продолжит трогать состояние.
  useEffect(() => {
    const running = timers.current
    return () => {
      for (const timer of running.values()) window.clearTimeout(timer)
      running.clear()
    }
  }, [])

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {toasts.map((toast) => (
          <div key={toast.id} className={`toast toast--${toast.tone}`}>
            <span className="toast-text">{toast.text}</span>
            {toast.action && (
              <button
                type="button"
                className="btn btn--quiet"
                onClick={() => {
                  dismiss(toast.id)
                  toast.action?.onAct()
                }}
              >
                {toast.action.label}
              </button>
            )}
            <IconButton label="Закрыть сообщение" onClick={() => dismiss(toast.id)}>
              <CloseIcon />
            </IconButton>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

/**
 * Показать сообщение. Вне ToastHost возвращает пустышку, а не бросает
 * исключение: сообщение — не то, ради чего стоит ронять экран.
 */
/** Заглушка вне провайдера — одна на всех. Новая стрелка на каждый
 *  вызов делала бы обработчики, собранные вокруг неё, разными при
 *  каждой отрисовке, а через них перерисовывалась бы вся доска. */
const SILENT: Push = () => {}

export function useToast(): Push {
  return useContext(ToastContext) ?? SILENT
}
