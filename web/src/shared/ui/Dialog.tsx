import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import { Button } from './Button.tsx'

/**
 * Диалог подтверждения.
 *
 * На нативном `<dialog>` с `showModal()`: браузер сам делает ловушку
 * фокуса, верхний слой, затемнение через `::backdrop` и закрытие
 * по Escape. Своя реализация всего этого — привычный способ получить
 * окно, из которого не выйти с клавиатуры; у нас такая уже есть
 * в панели, и повторять её незачем.
 *
 * Диалог оставлен только там, где отменить нельзя: смена видимости
 * доски, отзыв ключа, исключение из организации. Всё обратимое
 * спрашивать не должно — оно предлагает отмену после.
 */
/** Виден ли элемент. `checkVisibility` знает про `display: none`
 *  у предков и про `content-visibility`; где его нет — считаем
 *  по геометрии. */
function visible(element: HTMLElement): boolean {
  return typeof element.checkVisibility === 'function'
    ? element.checkVisibility()
    : element.offsetParent !== null
}

export function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel = 'Подтвердить',
  danger = false,
  busy = false,
  confirmDisabled = false,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  children: ReactNode
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
  /** Подтверждение недоступно, пока условие не выполнено: так у диалога,
   *  который просит набрать название удаляемого. */
  confirmDisabled?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  }, [open])

  // Куда вернуть фокус, когда возвращать некуда.
  //
  // Обычно это делает браузер: закрытый диалог отдаёт фокус туда,
  // откуда его открыли. Но открывают диалог часто из меню карточки,
  // а оно к этому моменту уже закрылось и спряталось вместе со своей
  // кнопкой; иногда фокус так и остаётся на кнопке самого диалога,
  // которую только что убрали с экрана. И в том, и в другом случае
  // он оказывается на `body` — клавиатуре идти дальше неоткуда.
  //
  // Поэтому проверяется не «на чём фокус», а «жив ли тот, на ком он»:
  // элемент, которого не видно, фокус не держит. Если не жив — фокус
  // получает сам экран: не то место, где были, но место, из которого
  // можно двигаться.
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    const onClose = () => {
      // Через кадр: сначала браузер делает своё, и перебивать его
      // нельзя — решение принимается, только если он не справился.
      requestAnimationFrame(() => {
        const active = document.activeElement as HTMLElement | null
        if (active && active !== document.body && active.isConnected && visible(active)) return
        document.querySelector<HTMLElement>('.board-screen, main')?.focus()
      })
    }
    dialog.addEventListener('close', onClose)
    return () => dialog.removeEventListener('close', onClose)
  }, [])

  // Escape закрывает диалог силами браузера — но нам нужно узнать
  // об этом, иначе состояние снаружи останется «открыт».
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    const onClose = () => onCancel()
    dialog.addEventListener('close', onClose)
    return () => dialog.removeEventListener('close', onClose)
  }, [onCancel])

  return (
    <dialog className="dialog" ref={ref} aria-label={title}>
      <h2 className="dialog-title">{title}</h2>
      <div className="dialog-body">{children}</div>
      <div className="row row--tight dialog-actions">
        <Button
          kind={danger ? 'danger' : 'primary'}
          busy={busy}
          disabled={confirmDisabled}
          onClick={onConfirm}
        >
          {confirmLabel}
        </Button>
        <Button kind="quiet" onClick={onCancel}>
          Отмена
        </Button>
      </div>
    </dialog>
  )
}
