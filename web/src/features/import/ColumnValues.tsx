import type { ImportReport } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Куда ложатся значения колонки файла на существующей доске.
 *
 * По названию сервер находит сам; незнакомое значение по умолчанию
 * заводит новую колонку. Но «In Review» из Jira и наша «В работе» — одно
 * и то же, а угадать это по названию нельзя: решает человек, и новая
 * колонка заводится, только если он её хочет.
 *
 * Ключ — значение в нижнем регистре: так же его сравнивает сервер.
 */
export function ColumnValues({
  report,
  choice,
  onChange,
  disabled,
}: {
  report: ImportReport
  choice: Record<string, string>
  onChange: (next: Record<string, string>) => void
  disabled: boolean
}) {
  const s = t.imports
  const named = new Set(report.boardColumns.map((c) => c.name.toLowerCase()))
  return (
    <div className="table-wrap">
      <table className="import-mapping">
        <thead>
          <tr>
            <th scope="col">{s.value}</th>
            <th scope="col" className="import-count">
              {s.cardsCol}
            </th>
            <th scope="col">{s.boardColumn}</th>
          </tr>
        </thead>
        <tbody>
          {report.columnValues.map((v) => {
            const key = v.value.trim().toLowerCase()
            return (
              <tr key={key}>
                <th scope="row">{v.value}</th>
                <td className="import-count">{v.cards}</td>
                <td>
                  <select
                    aria-label={s.columnFor(v.value)}
                    value={choice[key] ?? v.columnId ?? ''}
                    disabled={disabled}
                    onChange={(e) => onChange({ ...choice, [key]: e.target.value })}
                  >
                    {/* «Новая колонка» — только если одноимённой нет:
                        одноимённую сервер и так возьмёт, и второй
                        с тем же названием на доске не нужно. */}
                    {!named.has(key) && <option value="">{s.newColumn(v.value)}</option>}
                    {report.boardColumns.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name}
                      </option>
                    ))}
                  </select>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
