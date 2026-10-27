import { useEffect, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { BoardInfo, MovePreview } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { Field, FormError } from '../../shared/ui/Field.tsx'

/**
 * Перенос карточки на другую доску (ROADMAP 40).
 *
 * Спрашивает, хотя правило «обратимое не спрашивает»: перенос назад
 * не возвращает снятые метки, номер и место в итерации. Поэтому
 * подтверждение называет снимаемое поимённо — его присылает сервер
 * (`move-preview`), а не считает клиент: какие метки где действуют
 * и кто какую доску видит, решают политики базы, и копия этого правила
 * здесь разошлась бы с ними.
 *
 * Предлагаются только доски того же уровня: перенос с портфеля
 * на доску команды сервер всё равно откажет.
 *
 * Карточек может быть несколько — выделенные на доске. Тогда ответы
 * складываются: снимаемые метки и исполнители без доступа — по всем
 * карточкам сразу, чтобы подтверждение было одно на всю пачку.
 */
export function MoveToBoard({
  boardId,
  cardIds,
  title,
  boards,
  onMove,
  onCancel,
}: {
  boardId: string
  cardIds: string[]
  /** Название карточки или, если их несколько, «3 карточки». */
  title: string
  /** Куда можно: доски того же уровня, куда человек пишет. */
  boards: BoardInfo[]
  onMove: (cardIds: string[], toBoard: BoardInfo, columnId: string) => void
  onCancel: () => void
}) {
  const [target, setTarget] = useState(boards[0]?.id ?? '')
  // Ответ хранится вместе с доской, на которую спрашивали: пока идёт
  // новый запрос, старый ответ про другую доску показывать нельзя —
  // в нём чужие колонки и чужие метки.
  const [preview, setPreview] = useState<{ board: string; data: MovePreview } | null>(null)
  const [column, setColumn] = useState('')
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (!target) return
    let alive = true
    setFailed(false)
    Promise.all(cardIds.map((id) => api.movePreview(boardId, id, target))).then(
      (answers) => {
        if (!alive) return
        const data = merged(answers)
        setPreview({ board: target, data })
        setColumn(data.columns[0]?.id ?? '')
      },
      () => alive && setFailed(true),
    )
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- карточки сравниваются текстом: массив пересоздаётся при каждой отрисовке
  }, [boardId, cardIds.join(), target])

  const shown = preview?.board === target ? preview.data : null
  const chosen = boards.find((b) => b.id === target)
  const blocked = !shown || shown.unseen.length > 0 || shown.otherLevel || !column

  return (
    <ConfirmDialog
      open
      title={cardIds.length > 1 ? t.moveCard.titleMany(title) : t.moveCard.title(title)}
      confirmLabel={t.moveCard.confirm}
      busy={!!target && !shown && !failed}
      confirmDisabled={!chosen || blocked}
      onCancel={onCancel}
      onConfirm={() => chosen && onMove(cardIds, chosen, column)}
    >
      {boards.length === 0 ? (
        <p>{t.moveCard.noBoards}</p>
      ) : (
        <div className="stack">
          <Field label={t.moveCard.board}>
            {(bind) => (
              <select {...bind} value={target} onChange={(e) => setTarget(e.target.value)}>
                {boards.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {shown && shown.columns.length > 0 && (
            <Field label={t.moveCard.column}>
              {(bind) => (
                <select {...bind} value={column} onChange={(e) => setColumn(e.target.value)}>
                  {shown.columns.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              )}
            </Field>
          )}
          {!shown && !failed && <p className="small muted">{t.moveCard.loading}</p>}
          {shown && (
            <>
              <p className="small">{t.moveCard.number}</p>
              {shown.items > 0 && <p className="small">{t.moveCard.items(shown.items)}</p>}
              {shown.droppedLabels.length > 0 && (
                <p className="small">{t.moveCard.dropped(shown.droppedLabels.join(', '))}</p>
              )}
              {shown.iteration && <p className="small">{t.moveCard.iteration(shown.iteration)}</p>}
            </>
          )}
          {/* Почему перенести нельзя — словами и с тем, что сделать:
              погашенная кнопка без объяснения отправляет искать причину. */}
          <FormError>
            {failed
              ? t.moveCard.previewFailed
              : shown?.otherLevel
                ? t.moveCard.otherLevel
                : shown && shown.unseen.length > 0
                  ? t.moveCard.unseen(shown.unseen.join(', '))
                  : null}
          </FormError>
        </div>
      )}
    </ConfirmDialog>
  )
}

/** Ответы по нескольким карточкам — одним: колонки у доски назначения
 *  одни, остальное складывается без повторов. */
function merged(answers: MovePreview[]): MovePreview {
  const union = (pick: (a: MovePreview) => string[]) =>
    [...new Set(answers.flatMap(pick))].sort((a, b) => a.localeCompare(b))
  return {
    columns: answers[0]?.columns ?? [],
    droppedLabels: union((a) => a.droppedLabels),
    unseen: union((a) => a.unseen),
    iteration: union((a) => (a.iteration ? [a.iteration] : [])).join(', '),
    items: answers.reduce((sum, a) => sum + a.items, 0),
    otherLevel: answers.some((a) => a.otherLevel),
  }
}
