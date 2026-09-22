import type { ImportReport as Report } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { ScreenError } from '../../shared/ui/Field.tsx'
import { navigate, boardPath } from '../../shared/router/index.ts'
import { ColumnValues } from './ColumnValues.tsx'
import { ImportReport } from './ImportReport.tsx'

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

      <div className="stack stack--tight" aria-live="polite">
        <h3 className="section-title">{t.imports.preview}</h3>
        {checking && <p className="muted small">{t.imports.checking}</p>}
        <ScreenError>{problem}</ScreenError>
        {report && <ImportReport report={report} boardName={boardName} />}
      </div>

      <button className="primary" disabled={!canApply} aria-busy={applying || undefined} onClick={onApply}>
        {applying
          ? t.imports.applying
          : report && report.created > 0
            ? t.imports.apply(report.created)
            : t.imports.nothing}
      </button>
    </>
  )
}

/** Итог: что перенесено, и куда идти дальше. */
export function Done({ report, onAnother }: { report: Report; onAnother: () => void }) {
  return (
    <>
      <ImportReport report={report} boardName={report.boardName} />
      <div className="form-row">
        <button className="primary" onClick={() => report.boardId && navigate(boardPath(report.boardId))}>
          {t.imports.open}
        </button>
        <button onClick={onAnother}>{t.imports.another}</button>
      </div>
    </>
  )
}
