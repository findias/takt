import type { BoardInfo } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

export type Target = { kind: 'new'; name: string } | { kind: 'existing'; boardId: string }

/** Существующая доска цели; пусто — новая. */
export const boardIdOf = (target: Target) => (target.kind === 'existing' ? target.boardId : '')

/**
 * Куда переносим: новая доска или одна из тех, куда человек пишет.
 * Общий для всех источников: таблица и YouGile кладут карточки
 * одинаково, и спрашивать об этом разными словами незачем.
 *
 * Смена доски сообщается отдельно (`onBoardChange`): у другой доски
 * другие колонки, и выбор «значение → колонка» к ней не относится.
 */
export function TargetPicker({
  target,
  boards,
  fallbackName,
  disabled,
  onChange,
  onBoardChange,
}: {
  target: Target
  boards: BoardInfo[]
  fallbackName: string
  disabled: boolean
  onChange: (next: Target) => void
  onBoardChange: () => void
}) {
  return (
    <fieldset className="account-group" disabled={disabled}>
      <legend>{t.imports.where}</legend>
      <label>
        <input
          type="radio"
          name="import-target"
          checked={target.kind === 'new'}
          onChange={() => onChange({ kind: 'new', name: fallbackName })}
        />
        {t.imports.newBoard}
      </label>
      {target.kind === 'new' && (
        <input
          className="import-indent"
          aria-label={t.imports.newBoardName}
          value={target.name}
          required
          onChange={(e) => onChange({ kind: 'new', name: e.target.value })}
        />
      )}
      <label>
        <input
          type="radio"
          name="import-target"
          checked={target.kind === 'existing'}
          disabled={boards.length === 0}
          onChange={() => {
            onBoardChange()
            onChange({ kind: 'existing', boardId: boards[0]?.id ?? '' })
          }}
        />
        {t.imports.existingBoard}
        {boards.length === 0 && <span className="muted small"> — {t.imports.noWritableBoards}</span>}
      </label>
      {target.kind === 'existing' && (
        <select
          className="import-indent"
          aria-label={t.imports.board}
          value={target.boardId}
          onChange={(e) => {
            onBoardChange()
            onChange({ kind: 'existing', boardId: e.target.value })
          }}
        >
          {boards.map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </select>
      )}
    </fieldset>
  )
}
