import { useEffect, useState } from 'react'
import { blockUntilLabel } from '../../entities/card/model.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Срок блокировки в панели карточки (ROADMAP 28.1).
 *
 * Момент в зоне смотрящего: поле `datetime-local` показывает и принимает
 * местное время, а на сервер уходит ISO — сервер сам в местное время
 * не переводит, и «до пятницы, 18:00» так и остаётся 18:00 у того, кто
 * ставил.
 */

/** ISO → значение поля `datetime-local` в местном времени. */
export function toLocalInput(iso: string): string {
  const d = new Date(iso)
  const two = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}T${two(d.getHours())}:${two(d.getMinutes())}`
}

/** Значение поля → ISO; пустое поле — `null`. */
export function fromLocalInput(value: string): string | null {
  if (!value) return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d.toISOString()
}

/**
 * Срок открытой блокировки: видно, когда снимется, и править можно
 * во время блокировки — поставку перенесли. Бессрочная — кнопкой,
 * а не стиранием поля: стёртое поле читается как «не дописал», а не
 * как решение.
 */
export function BlockUntilEditor({
  until,
  canEdit,
  onChange,
}: {
  until: string | undefined
  canEdit: boolean
  onChange: (until: string | null) => void
}) {
  const [draft, setDraft] = useState(until ? toLocalInput(until) : '')
  useEffect(() => setDraft(until ? toLocalInput(until) : ''), [until])
  const label = until ? blockUntilLabel(until) : null
  const commit = () => {
    const next = fromLocalInput(draft)
    if (next && next !== until) onChange(next)
  }

  return (
    <div className="stack stack--tight">
      {label ? (
        <span className={`small${label.soon ? ' block-ending' : ''}`}>
          {label.expired ? label.text : t.board.liftsItself(label.text)}
        </span>
      ) : (
        <span className="muted small">{t.board.openEnded}</span>
      )}
      {canEdit && (
        // Без формы: кнопка отправки в форме у проекта всегда главная,
        // а главное действие на панели уже есть, и срок — не оно.
        <div className="row row--tight">
          <input
            type="datetime-local"
            aria-label={t.board.blockDeadline}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                commit()
              }
            }}
          />
          <Button type="button" disabled={!fromLocalInput(draft)} onClick={commit}>
            {until ? t.board.moveDeadline : t.board.setDeadline}
          </Button>
          {until && (
            <Button kind="quiet" type="button" onClick={() => onChange(null)}>
              {t.board.makeOpenEnded}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
