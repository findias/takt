import { useCallback, useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'

/**
 * Меню действий.
 *
 * Раньше действия над карточкой стояли рядом тремя кнопками и не
 * помещались в ширину колонки — обрезалось до «Откры», «Переиме»,
 * «Удалит». Меню решает это правильно: одно нажатие открывает список,
 * список читается целиком.
 *
 * Поведение по WAI-ARIA APG, и отступать от него незачем — люди уже
 * умеют: открывается щелчком, `Enter` и `Space`; ходит стрелками,
 * `Home` и `End`; закрывается `Escape` и щелчком вне; фокус
 * возвращается на кнопку, которая меню открыла.
 *
 * Позиционирование — обычным absolute внутри относительной обёртки.
 * Нативный `popover` дал бы всплытие над всем и умел бы top-layer,
 * но требует anchor positioning для привязки, а он ещё не везде;
 * до тех пор простое решение честнее сложного.
 */

export type MenuItem = {
  label: string
  icon?: ReactNode
  onSelect: () => void
  /** Опасное действие показывается иначе и стоит последним. */
  danger?: boolean
  disabled?: boolean
}

export function Menu({
  label,
  items,
  children,
}: {
  /** Имя кнопки для скринридера: «Действия карточки «Смета»». */
  label: string
  items: MenuItem[]
  /** Содержимое кнопки — обычно иконка. */
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const menuId = useId()

  const close = useCallback(
    (returnFocus = true) => {
      setOpen(false)
      if (returnFocus) buttonRef.current?.focus()
    },
    [],
  )

  // Щелчок вне и потеря фокуса закрывают меню. Фокус проверяется
  // отдельно от щелчка: уход по Tab — это тоже уход.
  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    return () => document.removeEventListener('pointerdown', onPointer)
  }, [open])

  // Фокус переезжает на пункт: так стрелки работают без ручного
  // управления aria-activedescendant.
  useEffect(() => {
    if (!open) return
    const nodes = listRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]')
    nodes?.[active]?.focus()
  }, [open, active])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      close()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => (i + 1) % items.length)
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => (i - 1 + items.length) % items.length)
    }
    if (e.key === 'Home') {
      e.preventDefault()
      setActive(0)
    }
    if (e.key === 'End') {
      e.preventDefault()
      setActive(items.length - 1)
    }
    if (e.key === 'Tab') close(false)
  }

  return (
    <div className="menu" ref={rootRef} onKeyDown={onKeyDown}>
      <button
        ref={buttonRef}
        type="button"
        className="btn btn--icon btn--quiet"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => {
          setActive(0)
          setOpen((v) => !v)
        }}
      >
        {children}
      </button>

      {open && (
        <div className="menu-list" id={menuId} role="menu" aria-label={label} ref={listRef}>
          {items.map((item, i) => (
            <button
              key={item.label}
              type="button"
              role="menuitem"
              tabIndex={i === active ? 0 : -1}
              className={item.danger ? 'menu-item menu-item--danger' : 'menu-item'}
              disabled={item.disabled}
              onFocus={() => setActive(i)}
              onClick={() => {
                close()
                item.onSelect()
              }}
            >
              {item.icon}
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
