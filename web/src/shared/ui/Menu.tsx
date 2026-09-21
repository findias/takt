import { useCallback, useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { CheckIcon } from './icons.tsx'
import { topLayer, useAnchored } from './anchored.ts'

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
  /** Выбор одного из нескольких (тема): диктор читает «переключатель,
   *  выбран», а не «флажок» — флажок обещает, что можно включить два. */
  radio?: boolean
  /** Тихое уточнение после подписи: откуда метка. Подпись остаётся
   *  короткой — по её первой букве ходит клавиатура. */
  hint?: string
  /** Ключ пункта, когда подписи могут совпасть: две «Срочно» из разных
   *  подразделений на одной карточке — редкость, но не невозможность. */
  id?: string
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
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const menuId = useId()
  const box = useAnchored(open, buttonRef, listRef, align, drop)

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
      // Список остаётся потомком обёртки и в верхнем слое — рисуется
      // он поверх всего, а в дереве стоит там же, где стоял.
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
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
              key={item.id ?? item.label}
              type="button"
              role={
                item.checked === undefined ? 'menuitem' : item.radio ? 'menuitemradio' : 'menuitemcheckbox'
              }
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
              {item.hint && <span className="menu-hint">{item.hint}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
