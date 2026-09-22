import type { PersonChoice, ImportReport as Report } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { ScreenError } from '../../shared/ui/Field.tsx'
import { navigate, boardPath } from '../../shared/router/index.ts'
import { ColumnValues } from './ColumnValues.tsx'
import { ImportReport } from './ImportReport.tsx'
import { CreatedPeople, People } from './People.tsx'

/**
 * Есть ли что делать переносом. Не только новые карточки: повтор той же
 * доски дописывает исполнителей, заведённых после прошлого раза, снимает
 * метки нашедшихся и заводит людей — и кнопка, погашенная на «новых
 * карточек нет», не давала сделать ни того, ни другого.
 */
export function hasWork(r: Report): boolean {
  return (
    r.created > 0 || r.assignedLater > 0 || (r.unlabeled ?? 0) > 0 || (r.createdPeople?.length ?? 0) > 0
  )
}

/**
 * Хвост экрана переноса, общий для всех источников: куда ложатся
 * значения колонки, что получится, и одна кнопка, которая переносит.
 */
export function Preview({
  report,
  boardName,
  checking,
  applying,
  problem,
  canApply,
  columnMap,
  onColumnMap,
  people,
  onPeople,
  onApply,
}: {
  report: Report | null
  boardName: string
  checking: boolean
  applying: boolean
  /** Почему переносить нельзя: сопоставление, лист, вход. */
  problem?: string | null
  canApply: boolean
  columnMap: Record<string, string>
  onColumnMap: (next: Record<string, string>) => void
  /** Выбор по людям источника (ROADMAP 23.6). */
  people: Record<string, PersonChoice>
  onPeople: (next: Record<string, PersonChoice>) => void
  onApply: () => void
}) {
  return (
    <>
      {report && report.boardColumns.length > 0 && report.columnValues.length > 0 && (
        <div className="stack stack--tight">
          <h3 className="section-title">{t.imports.values}</h3>
          <p className="muted small">{t.imports.valuesHint}</p>
          <ColumnValues report={report} choice={columnMap} onChange={onColumnMap} disabled={applying} />
        </div>
      )}

      {report && (report.people?.length ?? 0) > 0 && (
        <People report={report} choices={people} onChange={onPeople} disabled={applying} />
      )}

      <div className="stack stack--tight" aria-live="polite">
        <h3 className="section-title">{t.imports.preview}</h3>
        {checking && <p className="muted small">{t.imports.checking}</p>}
        <ScreenError>{problem}</ScreenError>
        {/* Кого не нашли, уже сказано в «Людях» — с выбором, что делать. */}
        {report && (
          <ImportReport report={report} boardName={boardName} withMissing={!(report.people?.length ?? 0)} />
        )}
      </div>

      <button className="primary" disabled={!canApply} aria-busy={applying || undefined} onClick={onApply}>
        {applying
          ? t.imports.applying
          : report && report.created > 0
            ? t.imports.apply(report.created)
            : report && hasWork(report)
              ? t.imports.update
              : t.imports.nothing}
      </button>
    </>
  )
}

/** Итог: что перенесено, и куда идти дальше. */
export function Done({
  report,
  onAnother,
  anotherLabel = t.imports.another,
}: {
  report: Report
  onAnother: () => void
  /** У пакета «ещё» — следующая доска того же файла, а не новый файл. */
  anotherLabel?: string
}) {
  return (
    <>
      <ImportReport report={report} boardName={report.boardName} />
      <CreatedPeople report={report} />
      <div className="form-row">
        <button className="primary" onClick={() => report.boardId && navigate(boardPath(report.boardId))}>
          {t.imports.open}
        </button>
        <button onClick={onAnother}>{anotherLabel}</button>
      </div>
    </>
  )
}
