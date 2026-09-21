import { useCallback, useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { api } from '../../shared/api/index.ts'
import type { BoardLabel, LabelPlace } from '../../shared/api/index.ts'
import { labelOrigin, pickerItems, placeWords } from '../../entities/label/model.ts'
import type { PickerItem } from '../../entities/label/model.ts'
import { CheckIcon } from '../../shared/ui/icons.tsx'
import { topLayer, useAnchored } from '../../shared/ui/anchored.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Выбор метки: поиск по существующим и заведение новой в том же поле.
 *
 * Прежде новую метку заводили только на экране «Команда»: карточка
 * открыта, разговор идёт, нужной метки нет — уйти с доски, завести,
 * вернуться, снова открыть карточку, повесить. Пять шагов и потеря
 * места. Теперь набранное название, которого нет, последним пунктом
 * предлагает «Завести»; порядок и есть защита от свалки — человек
 * видит «Срочно», прежде чем заведёт «срочно» (правила — в
 * `pickerItems`).
 *
 * Сперва метка заводится, потом вешается — двумя запросами, а не одной
 * операцией. Если второй не прошёл, в организации осталась метка,
 * которой ни на чём нет: это видно в «Команде» и убирается одной
 * кнопкой. Операция доски, заводящая сущность организации, смешала бы
 * два уровня ради одного редкого сбоя.
 *
 * Список — часть поля (`combobox` + `listbox`), как у палитры: диктор
 * объявляет текущий пункт и его место в списке, не требуя уходить
 * из поля ввода.
 */

/** Места заведения по доске. Спрашиваются один раз на доску и только
 *  когда выбор открыли: на доске триста карточек, и у каждой свой
 *  выбор метки — запрос на каждую был бы лишним трижды сто раз. */
const placesCache = new Map<string, Promise<LabelPlace[]>>()

/** `null` — ещё не известно: пока ответа нет, «заводить нельзя» было бы
 *  неправдой. */
function useLabelPlaces(boardId: string, enabled: boolean): LabelPlace[] | null {
  const [places, setPlaces] = useState<LabelPlace[] | null>(null)
  useEffect(() => {
    if (!enabled) return
    let alive = true
    let pending = placesCache.get(boardId)
    if (!pending) {
      pending = api.labelPlaces(boardId).then((r) => r.places)
      // Отказ не кэшируется: следующее открытие спросит снова.
      pending.catch(() => placesCache.delete(boardId))
      placesCache.set(boardId, pending)
    }
    // Отказ — то же, что «мест нет»: выбор существующих работает и так.
    pending.then((p) => alive && setPlaces(p)).catch(() => alive && setPlaces([]))
    return () => {
      alive = false
    }
  }, [boardId, enabled])
  return places
}

export type LabelChoiceProps = {
  boardId: string
  labels: BoardLabel[]
  /** Метки карточки; `null` — выбор для многих карточек сразу: пункты
   *  там не переключают, а вешают. */
  hung: string[] | null
  /** Можно ли заводить. Наблюдателю пункта «Завести» нет. */
  canEdit: boolean
  onToggle: (labelId: string, on: boolean) => void
}

export function LabelCombobox({
  boardId,
  labels,
  hung,
  canEdit,
  onToggle,
  onDone,
  onEscape,
  inputLabel = t.parts.findOrCreateLabel,
  quietWhenIdle = false,
}: LabelChoiceProps & {
  /** Действие выполнено — всплывающее закрывается, встроенное чистит поле. */
  onDone?: () => void
  /** Escape на пустом поле. Во всплывающем — закрыть его. */
  onEscape?: () => void
  inputLabel?: string
  /** Список — только когда о нём попросили: набрали текст или нажали
   *  стрелку вниз. Встроенному в панель выбору постоянно раскрытый
   *  список удлинял бы раздел меток на весь словарь.
   *
   *  Раскрытие по фокусу пробовали, и оно ломало нажатие рядом: щелчок
   *  по «Снять» сперва уводит фокус из поля, список сворачивается,
   *  панель укорачивается и прокрутка сдвигает содержимое — кнопка
   *  уезжает из-под указателя раньше, чем его отпустят. Поэтому уход
   *  из поля список не трогает; сворачивает его выбор или Escape. */
  quietWhenIdle?: boolean
}) {
  const [text, setText] = useState('')
  const [browsing, setBrowsing] = useState(false)
  const [active, setActive] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const places = useLabelPlaces(boardId, canEdit)
  const listId = useId()
  const errorId = useId()

  const listed = !quietWhenIdle || browsing || text !== ''
  const items = listed ? pickerItems(text, labels, hung, canEdit ? (places ?? []) : []) : []
  const current = Math.min(active, Math.max(items.length - 1, 0))

  const run = async (item: PickerItem) => {
    setError('')
    try {
      if (item.kind === 'toggle') {
        onToggle(item.label.id, !item.checked)
      } else {
        setBusy(true)
        let id: string
        if (item.kind === 'restore') {
          await api.restoreLabel(item.label.id)
          id = item.label.id
        } else {
          // Оттенок на бегу не спрашивается: сервер даст наименее
          // занятый. Поменять его можно в «Команде».
          id = (await api.createLabel(item.name, undefined, item.place)).id
        }
        onToggle(id, true)
      }
      setText('')
      setActive(0)
      setBrowsing(false)
      onDone?.()
    } catch (e) {
      // Отказ сервера называет, где метка уже есть или кто может её
      // завести, — и живёт у поля, где его и ждут.
      setError(e instanceof Error ? e.message : t.common.notDone)
    } finally {
      setBusy(false)
    }
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      // Первое нажатие раскрывает свёрнутый список, а не ходит по нему.
      if (!listed) setBrowsing(true)
      else setActive((i) => Math.min(i + 1, items.length - 1))
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      if (items[current] && !busy) void run(items[current])
    }
    if (e.key === 'Escape') {
      // Сперва стирается набранное, вторым нажатием — уход: так же
      // устроен поиск во всех браузерах.
      if (text || browsing) {
        e.stopPropagation()
        setText('')
        setError('')
        setBrowsing(false)
      } else if (onEscape) {
        e.stopPropagation()
        onEscape()
      }
    }
  }

  const optionId = (i: number) => `${listId}-${i}`

  return (
    <div className="label-combobox stack stack--tight">
      <input
        value={text}
        aria-label={inputLabel}
        placeholder={canEdit ? t.parts.labelFindOrCreate : t.parts.labelFind}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={items.length > 0}
        aria-controls={listId}
        aria-activedescendant={items[current] ? optionId(current) : undefined}
        aria-describedby={errorId}
        aria-invalid={error ? true : undefined}
        aria-busy={busy || undefined}
        onChange={(e) => {
          setText(e.target.value)
          setActive(0)
          setError('')
        }}
        onKeyDown={onKeyDown}
      />
      {/* Узел отказа стоит всегда: диктор объявляет изменение
          существующей области, а появление нового узла пропускает. */}
      <p className="form-error" id={errorId} aria-live="polite">
        {error}
      </p>
      <ul className="palette-list label-options" id={listId} role="listbox" aria-label={t.parts.labels}>
        {items.map((item, i) => (
          <li key={item.key} role="presentation">
            <button
              type="button"
              id={optionId(i)}
              role="option"
              tabIndex={-1}
              aria-selected={i === current}
              aria-checked={item.kind === 'toggle' && hung ? item.checked : undefined}
              aria-setsize={items.length}
              aria-posinset={i + 1}
              disabled={busy}
              className={i === current ? 'palette-item palette-item--active' : 'palette-item'}
              // Мышь ведёт выделение за собой: иначе клавиатура
              // и указатель показывают разное, и непонятно, что
              // случится по Enter.
              onMouseEnter={() => setActive(i)}
              // Фокус остаётся в поле: иначе после щелчка набор
              // продолжился бы в никуда.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => void run(item)}
            >
              <ItemText item={item} marked={hung !== null} />
            </button>
          </li>
        ))}
      </ul>
      {listed && items.length === 0 && (
        <p className="muted small">
          {text.trim()
            ? canEdit && places === null
              ? t.parts.checkingPlaces
              : canEdit
                ? t.parts.noSuchLabelCannotCreate
                : t.parts.noSuchLabel
            : canEdit
              ? t.parts.noLabelsTypeToCreate
              : t.parts.noLabels}
        </p>
      )}
    </div>
  )
}

function ItemText({ item, marked }: { item: PickerItem; marked: boolean }) {
  if (item.kind === 'toggle') {
    return (
      <>
        {/* Место под галочку занято всегда: иначе отмеченный пункт
            съезжал бы вправо, и список переставал бы читаться колонкой. */}
        {marked && (
          <span className="menu-check" aria-hidden="true">
            {item.checked ? <CheckIcon /> : null}
          </span>
        )}
        <span className={`label-dot label-dot--${item.label.tone}`} aria-hidden="true" />
        <span className="palette-title">{item.label.name}</span>
        <span className="menu-hint">{labelOrigin(item.label)}</span>
      </>
    )
  }
  if (item.kind === 'restore') {
    return (
      <>
        {marked && <span className="menu-check" aria-hidden="true" />}
        <span className="palette-title">{t.parts.restoreLabel(item.label.name)}</span>
        <span className="menu-hint">{labelOrigin(item.label)}</span>
      </>
    )
  }
  return (
    <>
      {marked && <span className="menu-check" aria-hidden="true" />}
      <span className="palette-title">{t.parts.createLabel(item.name)}</span>
      <span className="menu-hint">{placeWords(item.place)}</span>
    </>
  )
}

/**
 * Выбор метки во всплывающем окне у кнопки: на карточке доски и в полосе
 * действий над выделенными. Поведение — как у меню: открывается
 * нажатием, закрывается Escape и щелчком вне, фокус возвращается
 * на кнопку.
 */
export function LabelPickerButton({
  label,
  className,
  align = 'right',
  drop = 'down',
  children,
  ...choice
}: LabelChoiceProps & {
  /** Имя кнопки для диктора: «Метки: Срочно, Риск». */
  label: string
  className: string
  align?: 'left' | 'right'
  drop?: 'down' | 'up'
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const floatRef = useRef<HTMLDivElement>(null)
  const popupId = useId()
  const box = useAnchored(open, buttonRef, floatRef, align, drop)

  const close = useCallback((returnFocus = true) => {
    setOpen(false)
    if (returnFocus) buttonRef.current?.focus()
  }, [])

  // Фокус — в поле, когда окно уже стоит на месте: `autoFocus` сработал
  // бы до показа в верхнем слое, в узел, который ещё не виден.
  // `preventScroll` — поле лежит в верхнем слое, а в дереве оно внутри
  // прокручиваемой колонки, и браузер прокрутил бы её «к фокусу».
  const placed = box !== null
  useEffect(() => {
    if (placed) floatRef.current?.querySelector('input')?.focus({ preventScroll: true })
  }, [placed])

  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    return () => document.removeEventListener('pointerdown', onPointer)
  }, [open])

  return (
    <div className={open ? 'menu menu--open' : 'menu'} ref={rootRef}>
      <button
        ref={buttonRef}
        type="button"
        className={className}
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? popupId : undefined}
        onClick={() => setOpen((v) => !v)}
      >
        {children}
      </button>
      {open && (
        <div
          className={`menu-list label-picker${box?.up ? ' menu-list--up' : ''}`}
          popover={topLayer ? 'manual' : undefined}
          style={box ? { top: box.top, left: box.left } : { opacity: 0 }}
          id={popupId}
          role="dialog"
          aria-label={t.parts.labels}
          ref={floatRef}
          onKeyDown={(e) => {
            if (e.key === 'Tab') close(false)
          }}
        >
          <LabelCombobox
            {...choice}
            onDone={() => close()}
            onEscape={() => close()}
          />
        </div>
      )}
    </div>
  )
}
