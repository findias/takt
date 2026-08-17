import { useEffect, useState } from 'react'
import type { Label, Person } from '../../shared/api/index.ts'
import { Button, IconButton } from '../../shared/ui/Button.tsx'
import { NARROW, useMedia } from '../../shared/lib/useMedia.ts'
import { CloseIcon, FilterIcon, SearchIcon } from '../../shared/ui/icons.tsx'
import { EMPTY, UNASSIGNED, activeCount, isEmpty } from './filters.ts'
import type { Filters } from './filters.ts'

/**
 * Полоса фильтров.
 *
 * Всегда на виду, а не за кнопкой «фильтры»: спрятанный фильтр —
 * это фильтр, о котором забывают, а забытый фильтр показывает доску
 * не целиком и об этом не сообщает. Отсюда же счётчик отсеянного:
 * человек обязан видеть, что часть карточек скрыта им самим.
 *
 * На телефоне полоса съедала треть экрана — половину того, что осталось
 * от доски, — поэтому там на виду остаётся только поиск, а остальное
 * уходит под кнопку. Правило при этом не нарушено: число действующих
 * отборов стоит на самой кнопке, так что забыть о них по-прежнему
 * нельзя.
 *
 * Поиск с задержкой в четверть секунды: перерисовывать доску на каждую
 * букву при трёхстах карточках — заметная работа, а разницы между
 * «сразу» и «через 250 мс» на печати никто не чувствует.
 */
export function FilterBar({
  filters,
  people,
  labels,
  hidden,
  onChange,
}: {
  filters: Filters
  people: Person[]
  labels: Label[]
  /** Сколько карточек скрыто фильтром — иначе доска выглядит опустевшей. */
  hidden: number
  onChange: (next: Filters) => void
}) {
  const [text, setText] = useState(filters.text)
  const narrow = useMedia(NARROW)
  const [open, setOpen] = useState(false)
  const active = activeCount(filters)

  // Строка поиска — своё состояние: она печатается, а адрес меняется
  // следом. Обратная синхронизация нужна для перехода по ссылке
  // и кнопки «назад».
  useEffect(() => setText(filters.text), [filters.text])

  useEffect(() => {
    if (text === filters.text) return
    const timer = window.setTimeout(() => onChange({ ...filters, text }), 250)
    return () => window.clearTimeout(timer)
  }, [text, filters, onChange])

  return (
    <div className="filters row">
      <label className="filters-search">
        <SearchIcon />
        <input
          type="search"
          value={text}
          placeholder="Найти карточку"
          aria-label="Найти карточку"
          onChange={(e) => setText(e.target.value)}
        />
      </label>

      {narrow && (
        <Button kind="quiet" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          <FilterIcon />
          Отбор{active > 0 ? ` · ${active}` : ''}
        </Button>
      )}

      <div className="filters-rest row" hidden={narrow && !open}>
        <select
          value={filters.assignee ?? ''}
          aria-label="Исполнитель"
          onChange={(e) => onChange({ ...filters, assignee: e.target.value || null })}
        >
          <option value="">Все исполнители</option>
          <option value={UNASSIGNED}>Ни на ком</option>
          {people.map((person) => (
            <option key={person.userId} value={person.userId}>
              {person.name}
            </option>
          ))}
        </select>

        {labels.length > 0 && (
          <select
            value=""
            aria-label="Добавить метку в фильтр"
            onChange={(e) => {
              const id = e.target.value
              if (id && !filters.labels.includes(id)) {
                onChange({ ...filters, labels: [...filters.labels, id] })
              }
            }}
          >
            <option value="">Метка…</option>
            {labels
              .filter((label) => !filters.labels.includes(label.id))
              .map((label) => (
                <option key={label.id} value={label.id}>
                  {label.name}
                </option>
              ))}
          </select>
        )}

        {filters.labels.map((id) => {
          const label = labels.find((l) => l.id === id)
          return (
            <button
              key={id}
              className={`chip chip--${label?.tone ?? 'slate'} chip--removable`}
              aria-label={`Убрать из фильтра метку «${label?.name ?? id}»`}
              onClick={() =>
                onChange({
                  ...filters,
                  labels: filters.labels.filter((other) => other !== id),
                })
              }
            >
              {label?.name ?? id} ×
            </button>
          )
        })}

        {/* «Что у нас горит» — вопрос к доске, который задают чаще
            остальных отборов, поэтому он стоит отбором, а не выбором
            из трёх классов: «покажи фоновое» не спрашивает никто. */}
        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.expedite}
            onChange={(e) => onChange({ ...filters, expedite: e.target.checked })}
          />
          <span>Срочные</span>
        </label>

        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.blocked}
            onChange={(e) => onChange({ ...filters, blocked: e.target.checked })}
          />
          <span>Заблокированные</span>
        </label>

        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.aging}
            onChange={(e) => onChange({ ...filters, aging: e.target.checked })}
          />
          <span>Дольше обещанного</span>
        </label>

        {!isEmpty(filters) && (
          <>
            <span className="muted small">
              {hidden > 0 ? `скрыто ${hidden}` : 'ничего не скрыто'}
            </span>
            <Button kind="quiet" onClick={() => onChange(EMPTY)}>
              Показать все
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

/** Кнопка сброса для узких мест, где полосе не хватает ширины. */
export function ClearFilters({ onClear }: { onClear: () => void }) {
  return (
    <IconButton label="Сбросить фильтры" onClick={onClear}>
      <CloseIcon />
    </IconButton>
  )
}
