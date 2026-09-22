import { useEffect, useId, useRef, useState } from 'react'
import type { Iteration, BoardLabel, Person } from '../../shared/api/index.ts'
import { groupByOrigin, labelTitle, chipClass } from '../../entities/label/model.ts'
import { Button, IconButton } from '../../shared/ui/Button.tsx'
import { CloseIcon, FilterIcon, SearchIcon } from '../../shared/ui/icons.tsx'
import { EMPTY, NO_ITERATION, UNASSIGNED, activeCount, isEmpty } from './filters.ts'
import type { Filters } from './filters.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Полоса отбора — основная панель над доской (решение владельца
 * 22.09.2026: «Отбор сделать основной панелью»).
 *
 * На широком экране всегда на виду: спрятанный фильтр забывают,
 * а забытый показывает доску не целиком и об этом не сообщает. Кнопка
 * «Отбор» там спрятана стилями. На узком полоса съела бы треть экрана,
 * поэтому там она за кнопкой с числом включённых — забыть о них
 * по-прежнему нельзя. Рядом — счётчик отсеянного: человек обязан
 * видеть, что часть карточек скрыта им самим.
 *
 * Поиск — не здесь, а своей строкой ниже (CardSearch).
 */
export function FilterBar({
  filters,
  people,
  labels,
  iterations,
  hidden,
  hasBlockDeadlines,
  onChange,
}: {
  filters: Filters
  people: Person[]
  labels: BoardLabel[]
  /** Открытые итерации доски: закрытые не отбирают — их смотрят
   *  отчётом, а не доской. */
  iterations: Iteration[]
  /** Сколько карточек скрыто фильтром — иначе доска выглядит опустевшей. */
  hidden: number
  /** Есть ли на доске блокировки со сроком. Нет — отбор «истекает»
   *  не показывается: вопрос, который здесь не задать, только
   *  растягивает строку. */
  hasBlockDeadlines: boolean
  onChange: (next: Filters) => void
}) {
  const [open, setOpen] = useState(false)
  const restId = useId()
  const toggleRef = useRef<HTMLButtonElement>(null)

  // Раскрытое закрывается Escape — как меню, выбор метки и панель
  // карточки на этом же экране. Слушают кнопка и сама панель, а не
  // вся полоса: в поиске Escape стирает набранное, и отнимать это у
  // поля нельзя. Нажим истрачен здесь — иначе он закрыл бы заодно
  // и панель карточки, открытую рядом.
  const closeOnEscape = (e: React.KeyboardEvent) => {
    if (e.key !== 'Escape' || !open) return
    e.stopPropagation()
    setOpen(false)
    toggleRef.current?.focus()
  }
  const active = activeCount(filters)

  return (
    <div className="filters row">
      {/* Кнопка нужна только узкому экрану: на широком отбор открыт
          всегда, и кнопку прячут стили (.filters-toggle). */}
      <Button
        ref={toggleRef}
        kind="quiet"
        className="filters-toggle"
        onKeyDown={closeOnEscape}
        aria-expanded={open}
        aria-controls={restId}
        aria-label={active > 0 ? t.filters.filterOn(active) : t.filters.filter}
        onClick={() => setOpen((v) => !v)}
      >
        <FilterIcon />
        <span className="tool-label">{t.filters.filter}</span>
        {active > 0 && <span className="filter-count">{active}</span>}
      </Button>
      {!isEmpty(filters) && (
        <>
          <span className="muted small">
            {hidden > 0 ? t.filters.hidden(hidden) : t.filters.nothingHidden}
          </span>
          <Button kind="quiet" onClick={() => onChange(EMPTY)}>
            {t.filters.showAll}
          </Button>
        </>
      )}

      {/* Не атрибут hidden, а класс: `[hidden]` прячет безусловно
          (правило с !important в стилях), а на широком экране отбор
          открыт всегда — свёрнутым он бывает только на узком. */}
      <div
        className={`filters-rest row${open ? '' : ' filters-rest--closed'}`}
        id={restId}
        onKeyDown={closeOnEscape}
      >
        <select
          value={filters.assignee ?? ''}
          aria-label={t.filters.assignee}
          onChange={(e) => onChange({ ...filters, assignee: e.target.value || null })}
        >
          <option value="">{t.filters.allAssignees}</option>
          <option value={UNASSIGNED}>{t.filters.unassigned}</option>
          {people.map((person) => (
            <option key={person.userId} value={person.userId}>
              {person.name}
            </option>
          ))}
        </select>

        {/* Итерация — ответ на «к чему это привязано снаружи». Без
            отбора спринт на доске не увидеть: он живёт только
            в отчёте, а работают на доске. */}
        {iterations.length > 0 && (
          <select
            value={filters.iteration ?? ''}
            aria-label={t.filters.iteration}
            onChange={(e) => onChange({ ...filters, iteration: e.target.value || null })}
          >
            <option value="">{t.filters.allIterations}</option>
            <option value={NO_ITERATION}>{t.filters.notInIteration}</option>
            {iterations.map((iteration) => (
              <option key={iteration.id} value={iteration.id}>
                {iteration.name}
              </option>
            ))}
          </select>
        )}

        {labels.length > 0 && (
          <select
            value=""
            aria-label={t.filters.addLabel}
            onChange={(e) => {
              const id = e.target.value
              if (id && !filters.labels.includes(id)) {
                onChange({ ...filters, labels: [...filters.labels, id] })
              }
            }}
          >
            <option value="">{t.filters.labelPick}</option>
            {/* Отбирать можно и по убранной метке, и по чужой: они
                висят на карточках, и найти такие карточки — первый
                вопрос, который задают, наткнувшись на них. Группы —
                по происхождению: две одноимённые метки из разных мест
                иначе стояли бы в списке неразличимыми строками. */}
            {groupByOrigin(labels.filter((label) => !filters.labels.includes(label.id))).map(
              (group) => (
                <optgroup key={group.key} label={group.title}>
                  {group.labels.map((label) => (
                    <option key={label.id} value={label.id}>
                      {label.archived ? t.filters.archivedLabel(label.name) : label.name}
                    </option>
                  ))}
                </optgroup>
              ),
            )}
          </select>
        )}

        {filters.labels.map((id) => {
          const label = labels.find((l) => l.id === id)
          return (
            <button
              key={id}
              className={`${label ? chipClass(label) : 'chip chip--slate'} chip--removable`}
              title={label ? labelTitle(label) : undefined}
              aria-label={t.filters.removeLabel(label?.name ?? id)}
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
            уровня: «покажи низкие» не спрашивает никто. Горит — это
            верх шкалы, высокий и наивысший вместе. */}
        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.urgent}
            onChange={(e) => onChange({ ...filters, urgent: e.target.checked })}
          />
          <span>{t.filters.urgent}</span>
        </label>

        {/* «Срок подходит» — про обещанное наружу, а «Дольше
            обещанного» — про обещание доски. Это разные вопросы,
            и складывать их в один отбор нельзя. */}
        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.due}
            onChange={(e) => onChange({ ...filters, due: e.target.checked })}
          />
          <span>{t.filters.dueSoon}</span>
        </label>

        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.blocked}
            onChange={(e) => onChange({ ...filters, blocked: e.target.checked })}
          />
          <span>{t.filters.blocked}</span>
        </label>

        {/* Рядом с «Заблокированными»: это те из них, что снимутся сами
            в ближайшие сутки. Уведомлений продукт не шлёт — этот отбор
            и отвечает на «что вот-вот пойдёт». */}
        {(hasBlockDeadlines || filters.expiring) && (
        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.expiring}
            onChange={(e) => onChange({ ...filters, expiring: e.target.checked })}
          />
          <span>{t.filters.expiring}</span>
        </label>
        )}

        <label className="row row--tight small">
          <input
            type="checkbox"
            checked={filters.aging}
            onChange={(e) => onChange({ ...filters, aging: e.target.checked })}
          />
          <span>{t.filters.aging}</span>
        </label>

      </div>
    </div>
  )
}

/**
 * Поиск по карточкам — своей строкой под панелью отбора.
 *
 * С задержкой в четверть секунды: перерисовывать доску на каждую букву
 * при трёхстах карточках — заметная работа, а разницы между «сразу»
 * и «через 250 мс» на печати никто не чувствует.
 */
export function CardSearch({
  filters,
  onChange,
}: {
  filters: Filters
  onChange: (next: Filters) => void
}) {
  const [text, setText] = useState(filters.text)
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
    <label className="filters-search">
      <SearchIcon />
      <input
        type="search"
        value={text}
        placeholder={t.filters.findCard}
        aria-label={t.filters.findCard}
        onChange={(e) => setText(e.target.value)}
      />
    </label>
  )
}

/** Кнопка сброса для узких мест, где полосе не хватает ширины. */
export function ClearFilters({ onClear }: { onClear: () => void }) {
  return (
    <IconButton label={t.filters.reset} onClick={onClear}>
      <CloseIcon />
    </IconButton>
  )
}
