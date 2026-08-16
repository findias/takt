import { useCallback, useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { Button } from '../../shared/ui/Button.tsx'
import { api } from '../../shared/api/index.ts'
import type { ArchivedCard } from '../../shared/api/index.ts'
import { timeText } from '../../entities/feed/model.ts'

/**
 * Архив карточек доски.
 *
 * До этого убранную карточку можно было вернуть только из всплывающего
 * уведомления сразу после архивации: исчезло оно — и карточка становилась
 * недостижимой, оставаясь при этом в базе, в выгрузке организации
 * и в счётчиках. Архив досок был, архива карточек не было.
 *
 * Список читается порциями по времени архивации, а не по номеру
 * страницы: архив дописывается, и смещение по номеру однажды покажет
 * одну карточку дважды.
 */
export function CardArchive({
  boardId,
  canDelete,
  reloadKey,
  onRestored,
  onDelete,
  onClose,
}: {
  boardId: string
  /** Удалять насовсем может только владелец организации. */
  canDelete: boolean
  /** Меняется, когда карточку удалили насовсем: список перечитывает себя.
   *  Своего состояния доски у архива нет, а удаление происходит снаружи —
   *  диалог один на всё. */
  reloadKey: number
  onRestored: () => void
  /** Спросить и удалить. Диалог живёт на доске: он один на всё, поэтому
   *  название передаётся туда — на доске этой карточки уже нет. */
  onDelete: (cardId: string, title: string) => void
  onClose: () => void
}) {
  const [cards, setCards] = useState<ArchivedCard[] | null>(null)
  const [next, setNext] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [mode, setMode] = usePanelMode()

  const load = useCallback(
    (before?: string) => {
      api
        .archivedCards(boardId, before)
        .then((r) => {
          setCards((prev) => (before && prev ? [...prev, ...r.cards] : r.cards))
          setNext(r.next)
        })
        .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось прочитать архив'))
    },
    [boardId],
  )

  useEffect(() => load(), [load, reloadKey])

  const restore = (card: ArchivedCard) => {
    setError(null)
    api
      .restoreCard(boardId, card.id)
      .then(() => {
        setCards((prev) => prev?.filter((c) => c.id !== card.id) ?? null)
        onRestored()
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось вернуть карточку'))
  }

  return (
    <Panel mode={mode} onMode={setMode} title="Архив" label="Архив карточек" onClose={onClose}>
      {error && <p className="error">{error}</p>}
      {cards === null && !error && <p className="muted small">Читаем…</p>}
      {cards?.length === 0 && (
        <p className="muted small">
          Архив пуст. Сюда попадают карточки, убранные с доски: они не удаляются, и вернуть их
          можно отсюда.
        </p>
      )}

      {cards && cards.length > 0 && (
        <ul className="member-list">
          {cards.map((c) => (
            <li key={c.id}>
              <div className="member-who">
                <span>
                  {c.number} · {c.title}
                </span>
                {/* Переносится, а не обрезается: имя того, кто убрал
                    карточку, оказывалось ровно за многоточием. */}
                <span className="muted small related-note">
                  {c.columnName} · убрана {timeText(c.archivedAt)}
                  {c.actor ? ` · ${c.actor}` : ''}
                  {c.outcome === 'done' ? ' · была доведена до конца' : ''}
                </span>
                {/* Сказано до нажатия, а не после отказа. */}
                {!c.restorable && (
                  <span className="muted small related-note">
                    Колонка «{c.columnName}» тоже в архиве — вернуть некуда, пока не вернут её.
                  </span>
                )}
              </div>
              <div className="row row--tight">
                {c.restorable && (
                  <button className="link" onClick={() => restore(c)}>
                    Вернуть
                  </button>
                )}
                {canDelete && (
                  <button
                    className="link link--danger"
                    onClick={() => onDelete(c.id, `${c.number} · ${c.title}`)}
                  >
                    Удалить навсегда
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {next && (
        <Button kind="quiet" onClick={() => load(next)}>
          Показать ещё
        </Button>
      )}
    </Panel>
  )
}
