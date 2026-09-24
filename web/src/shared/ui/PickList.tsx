/**
 * Выбор нескольких значений: список «добавить» и выбранное фишками
 * с крестиком — так же, как метки в полосе отбора доски. Нативный
 * `select multiple` здесь не годится: зажатый Ctrl никто не угадывает,
 * а выбранное прокручивается из виду.
 *
 * Пустой выбор значит «любые» — это слово и стоит в списке первым,
 * пока ничего не выбрано.
 */
export function PickList({
  label,
  anyText,
  addText,
  options,
  chosen,
  removeLabel,
  onChange,
}: {
  label: string
  /** Что значит пустой выбор: «Все доски». */
  anyText: string
  /** Первая строка списка, когда что-то уже выбрано: «Добавить доску». */
  addText: string
  options: { id: string; name: string }[]
  chosen: string[]
  removeLabel: (name: string) => string
  onChange: (next: string[]) => void
}) {
  const nameOf = (id: string) => options.find((o) => o.id === id)?.name
  const rest = options.filter((o) => !chosen.includes(o.id))
  return (
    <div className="row row--tight pick-list">
      <label className="row row--tight">
        <span className="small pick-list-name">{label}</span>
        <select
          value=""
          disabled={rest.length === 0}
          onChange={(e) => {
            if (e.target.value) onChange([...chosen, e.target.value])
          }}
        >
          <option value="">{chosen.length === 0 ? anyText : addText}</option>
          {rest.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
            </option>
          ))}
        </select>
      </label>
      {chosen.map((id) => {
        // Выбранное, которого нет в списке, — из присланной ссылки на то,
        // что человеку не видно или уже убрано. Фишка остаётся, чтобы
        // её можно было снять: молча выброшенный отбор меняет отчёт.
        const name = nameOf(id) ?? '…'
        return (
          <button
            key={id}
            type="button"
            className="chip chip--slate chip--removable"
            aria-label={removeLabel(name)}
            onClick={() => onChange(chosen.filter((other) => other !== id))}
          >
            {name} ×
          </button>
        )
      })}
    </div>
  )
}
