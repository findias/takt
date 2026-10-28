import { useCallback, useEffect, useId, useRef, useState } from 'react'
import type { EstimateUnit } from '../../shared/api/index.ts'
import { UNIT_SHORT } from '../../entities/card/model.ts'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { CheckIcon, ChevronDownIcon } from '../../shared/ui/icons.tsx'
import { topLayer, useAnchored } from '../../shared/ui/anchored.ts'
import { t } from '../../shared/i18n/index.ts'
import { SORTS, matchAssignee, sortAssignees } from './assigneeFilter.ts'
import type { AssigneeOption, AssigneeSort } from './assigneeFilter.ts'
import { UNASSIGNED } from './filters.ts'

/**
 * Отбор доски по исполнителю.
 *
 * Свой список, а не `select`: в нативном нельзя ни переставить людей
 * по загрузке, ни показать её рядом с именем, ни найти человека
 * набором, когда людей много. Поведение — как у выбора метки: кнопка,
 * окно у неё, поле поиска со списком (`combobox` + `listbox`), Escape
 * и щелчок вне закрывают, фокус возвращается на кнопку.
 *
 * Порядок — состояние сессии, а не адреса: это привычка смотрящего,
 * а не часть того, что он показывает по ссылке.
 */

const SORT_KEY = 'takt.assigneeSort'

function storedSort(): AssigneeSort {
  try {
    const v = localStorage.getItem(SORT_KEY)
    return (SORTS as string[]).includes(v ?? '') ? (v as AssigneeSort) : 'name'
  } catch {
    // Хранилище закрыто (приватное окно, запрет сайта) — порядок
    // по умолчанию, выбор просто не запомнится.
    return 'name'
  }
}

function storeSort(sort: AssigneeSort) {
  try {
    localStorage.setItem(SORT_KEY, sort)
  } catch {
    // См. storedSort: не запомнили — не беда.
  }
}

type Choice = { value: string | null; label: string; option?: AssigneeOption }

export function AssigneeFilter({
  value,
  options,
  unit,
  onChange,
}: {
  /** Идентификатор человека, `UNASSIGNED` или `null` — все. */
  value: string | null
  options: AssigneeOption[]
  unit: EstimateUnit
  onChange: (next: string | null) => void
}) {
  const [open, setOpen] = useState(false)
  const [sort, setSort] = useState<AssigneeSort>(storedSort)
  const [text, setText] = useState('')
  const [active, setActive] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const floatRef = useRef<HTMLDivElement>(null)
  const popupId = useId()
  const listId = useId()
  const box = useAnchored(open, buttonRef, floatRef, 'left', 'down')

  // Сортировать по оценке нечем, пока на доске не оценена ни одна
  // незавершённая карточка: порядок совпал бы с порядком по карточкам.
  const weighed = options.some((o) => o.weight > 0)
  const sorts = weighed ? SORTS : SORTS.filter((s) => s !== 'weight')
  const order = sorts.includes(sort) ? sort : 'name'

  const fixed: Choice[] = [
    { value: null, label: t.filters.allAssignees },
    { value: UNASSIGNED, label: t.filters.unassigned },
  ]
  const people: Choice[] = sortAssignees(options, order)
    .filter((o) => matchAssignee(o, text))
    .map((o) => ({ value: o.userId, label: o.name, option: o }))
  // Поиск ищет людей: «Все» и «Ни на ком» на время набора уходят,
  // иначе первым пунктом под Enter стояли бы они, а не найденный.
  const choices = text.trim() ? people : [...fixed, ...people]
  const current = Math.min(active, Math.max(choices.length - 1, 0))

  const shown =
    value === null
      ? t.filters.allAssignees
      : value === UNASSIGNED
        ? t.filters.unassigned
        : (options.find((o) => o.userId === value)?.name ?? t.common.someone)

  const close = useCallback((returnFocus = true) => {
    setOpen(false)
    setText('')
    if (returnFocus) buttonRef.current?.focus()
  }, [])

  const pick = (choice: Choice) => {
    onChange(choice.value)
    close()
  }

  // Фокус — в поле, когда окно уже стоит на месте (как у выбора метки).
  const placed = box !== null
  useEffect(() => {
    if (placed) floatRef.current?.querySelector('input')?.focus({ preventScroll: true })
  }, [placed])

  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) close(false)
    }
    document.addEventListener('pointerdown', onPointer)
    return () => document.removeEventListener('pointerdown', onPointer)
  }, [open, close])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive(Math.min(current + 1, choices.length - 1))
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive(Math.max(current - 1, 0))
    }
    if (e.key === 'Home' && !text) {
      e.preventDefault()
      setActive(0)
    }
    if (e.key === 'End' && !text) {
      e.preventDefault()
      setActive(choices.length - 1)
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      if (choices[current]) pick(choices[current])
    }
    if (e.key === 'Escape') {
      e.stopPropagation()
      // Сперва стирается набранное, вторым нажатием — уход.
      if (text) {
        setText('')
        setActive(0)
      } else close()
    }
    if (e.key === 'Tab') close(false)
  }

  const optionId = (i: number) => `${listId}-${i}`

  return (
    <div className={open ? 'menu menu--open' : 'menu'} ref={rootRef}>
      <button
        ref={buttonRef}
        type="button"
        className="select-button"
        aria-label={t.filters.assigneeIs(shown)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? popupId : undefined}
        onClick={() => {
          if (open) return close()
          setActive(0)
          setOpen(true)
        }}
      >
        <span className="select-button-text">{shown}</span>
        <ChevronDownIcon />
      </button>
      {open && (
        <div
          className={`menu-list assignee-picker${box?.up ? ' menu-list--up' : ''}`}
          popover={topLayer ? 'manual' : undefined}
          style={box ? { top: box.top, left: box.left } : { opacity: 0 }}
          id={popupId}
          role="dialog"
          aria-label={t.filters.assignee}
          ref={floatRef}
          onKeyDown={onKeyDown}
        >
          <div className="segment" role="group" aria-label={t.filters.assigneeOrder}>
            {sorts.map((s) => (
              <button
                key={s}
                type="button"
                className={s === order ? 'segment-item segment-item--on' : 'segment-item'}
                aria-pressed={s === order}
                onClick={() => {
                  setSort(s)
                  storeSort(s)
                  setActive(0)
                  floatRef.current?.querySelector('input')?.focus({ preventScroll: true })
                }}
              >
                {t.filters.assigneeSorts[s]}
              </button>
            ))}
          </div>
          <input
            value={text}
            aria-label={t.filters.findPerson}
            placeholder={t.filters.findPerson}
            role="combobox"
            aria-autocomplete="list"
            aria-expanded="true"
            aria-controls={listId}
            aria-activedescendant={choices[current] ? optionId(current) : undefined}
            onChange={(e) => {
              setText(e.target.value)
              setActive(0)
            }}
          />
          <ul className="palette-list" id={listId} role="listbox" aria-label={t.filters.assignee}>
            {choices.map((choice, i) => (
              <li key={choice.value ?? 'all'} role="presentation">
                <button
                  type="button"
                  id={optionId(i)}
                  role="option"
                  tabIndex={-1}
                  aria-selected={i === current}
                  aria-checked={choice.value === value}
                  className={i === current ? 'palette-item palette-item--active' : 'palette-item'}
                  onMouseEnter={() => setActive(i)}
                  // Фокус остаётся в поле: набор продолжается без щелчка
                  // обратно.
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(choice)}
                >
                  <span className="menu-check" aria-hidden="true">
                    {choice.value === value ? <CheckIcon /> : null}
                  </span>
                  {choice.option && <Avatar name={choice.option.name} />}
                  <span className="palette-title">{choice.label}</span>
                  {choice.option && <span className="menu-hint">{load(choice.option, unit)}</span>}
                </button>
              </li>
            ))}
          </ul>
          {choices.length === 0 && <p className="muted small">{t.filters.noSuchPerson}</p>}
        </div>
      )}
    </div>
  )
}

/** Загрузка рядом с именем: сколько в работе и сколько это в оценке.
 *  Нет доступа — сказать прямо: иначе непонятно, почему человек в списке,
 *  а назначить его на карточку нельзя. */
function load(o: AssigneeOption, unit: EstimateUnit): string {
  const parts: string[] = []
  if (o.noAccess) parts.push(t.filters.noAccess)
  if (o.cards > 0) {
    parts.push(
      o.weight > 0
        ? t.filters.inWorkWeight(o.cards, `${number(o.weight)}${o.unestimated > 0 ? '+' : ''} ${UNIT_SHORT[unit]}`)
        : t.filters.inWork(o.cards),
    )
  }
  return parts.join(' · ')
}

function number(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(1)))
}
