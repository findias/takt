import type { ButtonHTMLAttributes, ReactNode } from 'react'

/**
 * Кнопка.
 *
 * Четыре вида, и больше не нужно: главное действие на экране одно,
 * обычных — сколько угодно, тихая живёт в плотных рядах, опасная —
 * там, где отменить нельзя.
 *
 * Занятость отделена от недоступности намеренно. Недоступная кнопка
 * говорит «сейчас нельзя», занятая — «уже делаю»; показывать их
 * одинаково значит на каждый медленный ответ отвечать «нельзя».
 */
export type ButtonKind = 'primary' | 'default' | 'quiet' | 'danger'

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  kind?: ButtonKind
  /** Действие идёт: кнопка не нажимается, но объясняет почему. */
  busy?: boolean
  icon?: ReactNode
}

export function Button({
  kind = 'default',
  busy = false,
  icon,
  children,
  className,
  disabled,
  ...rest
}: Props) {
  return (
    <button
      {...rest}
      className={['btn', `btn--${kind}`, className].filter(Boolean).join(' ')}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      {icon}
      {children}
    </button>
  )
}

/**
 * Кнопка без подписи.
 *
 * Подпись обязательна всё равно — она уходит в `aria-label` и в
 * подсказку. Иконка без имени для скринридера — это кнопка «кнопка».
 */
export function IconButton({
  label,
  kind = 'quiet',
  busy = false,
  children,
  className,
  disabled,
  ...rest
}: Omit<Props, 'icon'> & { label: string }) {
  return (
    <button
      {...rest}
      className={['btn', 'btn--icon', `btn--${kind}`, className].filter(Boolean).join(' ')}
      aria-label={label}
      title={label}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      {children}
    </button>
  )
}
