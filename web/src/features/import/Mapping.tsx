import { IMPORT_FIELDS } from '../../shared/api/index.ts'
import type { ImportField } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Сопоставление колонок файла полям карточки (ROADMAP 23.2).
 *
 * Сервер предлагает по заголовкам, человек правит. Рядом с заголовком
 * — значение из первой строки: «Статус» у одной системы — колонка
 * доски, у другой — «открыта/закрыта», и отличить их можно только
 * по данным.
 *
 * Нативный `select`, а не наше меню: это поле формы — выбор значения,
 * а не действие над объектом, и с клавиатуры, и на телефоне родной
 * список работает лучше любого своего.
 */
export function Mapping({
  headers,
  sample,
  mapping,
  onChange,
  disabled,
}: {
  headers: string[]
  sample: string[][]
  mapping: ImportField[]
  onChange: (next: ImportField[]) => void
  disabled: boolean
}) {
  return (
    <div className="table-wrap">
      <table className="import-mapping">
        <thead>
          <tr>
            <th scope="col">{t.imports.header}</th>
            <th scope="col">{t.imports.example}</th>
            <th scope="col">{t.imports.field}</th>
          </tr>
        </thead>
        <tbody>
          {headers.map((header, i) => (
            <tr key={i}>
              <th scope="row">{header || '—'}</th>
              <td className="import-example">{sample.find((row) => row[i]?.trim())?.[i] ?? '—'}</td>
              <td>
                <select
                  aria-label={t.imports.fieldOf(header)}
                  value={mapping[i] ?? ''}
                  disabled={disabled}
                  onChange={(e) => {
                    const next = [...mapping]
                    const field = e.target.value as ImportField
                    // Поле достаётся одной колонке: выбранное здесь
                    // снимается с той, где стояло, — иначе сервер
                    // откажет, а человек будет искать второе вхождение.
                    if (field !== '') {
                      const was = next.indexOf(field)
                      if (was !== -1) next[was] = ''
                    }
                    next[i] = field
                    onChange(next)
                  }}
                >
                  <option value="">{t.imports.fields['']}</option>
                  {IMPORT_FIELDS.map((f) => (
                    <option key={f} value={f}>
                      {t.imports.fields[f]}
                    </option>
                  ))}
                </select>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
