import { useCallback, useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { Button } from '../../shared/ui/Button.tsx'
import { api } from '../../shared/api/index.ts'
import type { ArchivedCard } from '../../shared/api/index.ts'
import { timeText } from '../../entities/feed/model.ts'
import { ScreenError } from '../../shared/ui/Field'
import { t } from '../../shared/i18n/index.ts'

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
  // Что ищут в архиве. Архив на сотнях карточек открывают, чтобы найти
  // одну, а не листать подряд: без поиска «Показать ещё» приходилось
  // жать десяток раз, вглядываясь в каждую строку.
  const [query, setQuery] = useState('')

  const load = useCallback(
    (before?: string, search = query) => {
      api
        .archivedCards(boardId, before, search)
        .then((r) => {
          setCards((prev) => (before && prev ? [...prev, ...r.cards] : r.cards))
          setNext(r.next)
        })
        .catch((e) => setError(e instanceof Error ? e.message : t.parts.archiveReadFailed))
    },
    [boardId, query],
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
      .catch((e) => setError(e instanceof Error ? e.message : t.parts.restoreCardFailed))
  }

  return (
    <Panel mode={mode} onMode={setMode} title={t.parts.archive} label={t.parts.cardArchive} onClose={onClose}>
      <ScreenError>{error}</ScreenError>
      {/* Поиск стоит всегда, а не появляется от числа карточек: строка
          поиска, возникающая на сто первой карточке, читается как сбой. */}
      <input
        type="search"
        value={query}
        aria-label={t.parts.findInArchive}
        placeholder={t.parts.findInArchive}
        onChange={(e) => setQuery(e.target.value)}
      />
      {cards === null && !error && <p className="muted small">{t.common.reading}</p>}
      {cards?.length === 0 && query.trim() !== '' && (
        <p className="muted small">{t.parts.archiveNothingFound(query.trim())}</p>
      )}
      {cards?.length === 0 && query.trim() === '' && (
        <p className="muted small">{t.parts.archiveEmpty}</p>
      )}

      {cards && cards.length > 0 && (
        <ul className="member-list">
          {cards.map((c) => (
            <li key={c.id}>
              <div className="member-who">
                {/* Название переносится, а не обрезается. Сосед снизу —
                    имя того, кто убрал карточку, — это правило уже
                    получил, а само название осталось на общем: замер
                    23.08.2026 нашёл в архиве два названия из двух
                    обрезанными, потерянного 71 и 57 пикселей.
                    В архиве название — единственный способ узнать
                    карточку: ключ есть, но по ключу не вспомнишь. */}
                <span className="wrap-title">
                  {c.number} · {c.title}
                </span>
                {/* Переносится, а не обрезается: имя того, кто убрал
                    карточку, оказывалось ровно за многоточием. */}
                <span className="muted small related-note">
                  {c.columnName} · {t.parts.archivedAt(timeText(c.archivedAt))}
                  {c.actor ? ` · ${c.actor}` : ''}
                  {c.outcome === 'done' ? t.parts.wasFinished : ''}
                </span>
                {/* Сказано до нажатия, а не после отказа. */}
                {!c.restorable && (
                  <span className="muted small related-note">
                    {t.parts.columnArchived(c.columnName)}
                  </span>
                )}
              </div>
              <div className="row row--tight">
                {c.restorable && (
                  <button className="link" onClick={() => restore(c)}>
                    {t.parts.restore}
                  </button>
                )}
                {canDelete && (
                  <button
                    className="link link--danger"
                    onClick={() => onDelete(c.id, `${c.number} · ${c.title}`)}
                  >
                    {t.parts.deleteForever}
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {next && (
        <Button kind="quiet" onClick={() => load(next)}>
          {t.common.showMore}
        </Button>
      )}
    </Panel>
  )
}
