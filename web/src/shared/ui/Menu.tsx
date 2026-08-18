import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { CheckIcon } from './icons.tsx'

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
 * Позиционирование — верхний слой (`popover`) и координаты, считанные
 * от кнопки. Обычный absolute внутри обёртки не годится: колонка
 * карточек прокручивается сама, и у нижней карточки меню не просто
 * обрезалось — в точке пунктов «Убрать в архив» и «Удалить навсегда»
 * лежала колонка, и они не нажимались вовсе. Верхний слой не обрезает
 * ничто; привязку, которую дал бы anchor positioning (он ещё не везде),
 * делает `place` на открытие, прокрутку и изменение размера.
 *
 * `popover="manual"` — потому что закрытием управляем мы: у `auto`
 * своё закрытие мимо состояния, и кнопка осталась бы с `aria-expanded`
 * «открыто» при закрытом списке.
 */

/** Верхний слой есть не везде — в разборе разметки для проверок его нет.
 *  Там признак не ставится вовсе: иначе стилевое правило «закрытое
 *  всплывающее не показывать» спрячет список, а показать его нечем.
 *  Без верхнего слоя остаётся `position: fixed` — оно тоже уходит
 *  из прокрутки, только его обрезает содержимое с `contain`. */
const topLayer =
  typeof HTMLElement !== 'undefined' && typeof HTMLElement.prototype.showPopover === 'function'

export type MenuItem = {
  label: string
  icon?: ReactNode
  onSelect: () => void
  /** Опасное действие показывается иначе и стоит последним. */
  danger?: boolean
  disabled?: boolean
  /** Пункт-переключатель: назначен исполнитель, висит метка. Задан —
   *  значит пункт не действие, а состояние, и роль у него другая:
   *  `menuitemcheckbox` читается вслух вместе с «включено», а галочка
   *  без роли осталась бы значком, о котором скринридер молчит. */
  checked?: boolean
}

export function Menu({
  label,
  items,
  className = 'btn btn--icon btn--quiet',
  align = 'right',
  drop = 'down',
  children,
}: {
  /** Имя кнопки для скринридера: «Действия карточки «Смета»». */
  label: string
  items: MenuItem[]
  /** С какой стороны раскрывается список — пожелание, а не приказ:
   *  если с этой стороны места нет, `place` развернёт список к другой.
   *  По умолчанию от правого края: меню действий прижато к правому краю
   *  карточки. Поле, которое правят нажатием по нему самому, стоит
   *  слева, и список от правого края уезжал бы за край окна. */
  align?: 'left' | 'right'
  /** Вниз или вверх — тоже пожелание. Вверх просят кнопки у нижнего
   *  края экрана: список вниз оттуда открывается за пределы окна. */
  drop?: 'down' | 'up'
  /** Чем открывается меню. Умолчание — тихая кнопка-иконка; правка
   *  по самому полю передаёт сюда своё, потому что там открывашка —
   *  и есть значение: стопка исполнителей, ряд меток, оценка. */
  className?: string
  /** Содержимое кнопки — обычно иконка. */
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  // Пока координаты не сосчитаны, показывать нечего: список стоял бы
  // не на месте ровно один кадр, и это видно глазом. Прячется он
  // прозрачностью, а не `visibility`: невидимое по `visibility` нельзя
  // сфокусировать, и первый пункт молча оставался бы без фокуса —
  // меню открывалось бы, а клавиатура в него не попадала.
  const [box, setBox] = useState<{ top: number; left: number; up: boolean } | null>(null)
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const menuId = useId()

  const close = useCallback(
    (returnFocus = true) => {
      setOpen(false)
      setBox(null)
      if (returnFocus) buttonRef.current?.focus()
    },
    [],
  )

  /** Координаты списка от кнопки. Отступ между ними задан в разметке
   *  (`margin`), чтобы слушаться плотности; здесь — только край окна,
   *  от которого нельзя уехать: за ним список не достать ничем. */
  const place = useCallback(() => {
    const button = buttonRef.current
    const list = listRef.current
    if (!button || !list) return
    const anchor = button.getBoundingClientRect()
    const { width, height } = list.getBoundingClientRect()
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
  }, [align, drop])

  // Верхний слой и координаты — до отрисовки: `useLayoutEffect`
  // успевает пересчитать состояние прежде, чем кадр покажут.
  useLayoutEffect(() => {
    if (!open) return
    const list = listRef.current
    if (!list) return
    if (topLayer && !list.matches(':popover-open')) list.showPopover()
    place()
    const again = () => place()
    // `capture` — прокрутка колонки карточек не всплывает до окна.
    window.addEventListener('scroll', again, true)
    window.addEventListener('resize', again)
    return () => {
      window.removeEventListener('scroll', again, true)
      window.removeEventListener('resize', again)
    }
  }, [open, place])

  // Щелчок вне и потеря фокуса закрывают меню. Фокус проверяется
  // отдельно от щелчка: уход по Tab — это тоже уход.
  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      // Список остаётся потомком обёртки и в верхнем слое — рисуется
      // он поверх всего, а в дереве стоит там же, где стоял.
      if (!rootRef.current?.contains(e.target as Node)) {
        setOpen(false)
        setBox(null)
      }
    }
    document.addEventListener('pointerdown', onPointer)
    return () => document.removeEventListener('pointerdown', onPointer)
  }, [open])

  // Фокус переезжает на пункт: так стрелки работают без ручного
  // управления aria-activedescendant.
  //
  // `preventScroll` — потому что пункт лежит в верхнем слое и виден
  // и так, а в дереве он всё ещё внутри прокручиваемой колонки:
  // браузер прокрутил бы её «к фокусу», и доска уехала бы под открытым
  // меню без всякой причины.
  useEffect(() => {
    if (!open) return
    const nodes = listRef.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]')
    nodes?.[active]?.focus({ preventScroll: true })
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
    <div className={open ? 'menu menu--open' : 'menu'} ref={rootRef} onKeyDown={onKeyDown}>
      <button
        ref={buttonRef}
        type="button"
        className={className}
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
        <div
          className={`menu-list${box?.up ? ' menu-list--up' : ''}`}
          popover={topLayer ? 'manual' : undefined}
          style={box ? { top: box.top, left: box.left } : { opacity: 0 }}
          id={menuId}
          role="menu"
          aria-label={label}
          ref={listRef}
        >
          {items.map((item, i) => (
            <button
              key={item.label}
              type="button"
              role={item.checked === undefined ? 'menuitem' : 'menuitemcheckbox'}
              aria-checked={item.checked}
              tabIndex={i === active ? 0 : -1}
              className={item.danger ? 'menu-item menu-item--danger' : 'menu-item'}
              disabled={item.disabled}
              onFocus={() => setActive(i)}
              onClick={() => {
                close()
                item.onSelect()
              }}
            >
              {/* Место под галочку занято всегда: иначе включённый пункт
                  съезжал бы вправо относительно соседей, и список
                  переставал бы читаться колонкой. */}
              {item.checked === undefined ? (
                item.icon
              ) : (
                <span className="menu-check" aria-hidden="true">
                  {item.checked ? <CheckIcon /> : null}
                </span>
              )}
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
