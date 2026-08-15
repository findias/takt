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
export function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel = 'Подтвердить',
  danger = false,
  busy = false,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  children: ReactNode
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
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
        <Button kind={danger ? 'danger' : 'primary'} busy={busy} onClick={onConfirm}>
          {confirmLabel}
        </Button>
        <Button kind="quiet" onClick={onCancel}>
          Отмена
        </Button>
      </div>
    </dialog>
  )
}
