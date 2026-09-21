import { useCallback, useEffect, useState } from 'react'
import { Button } from '../../shared/ui/Button.tsx'
import { api } from '../../shared/api/index.ts'
import type { BoardEvent, CardField } from '../../shared/api/index.ts'
import { actorText, eventText, timeText } from '../../entities/feed/model.ts'
import { ScreenError } from '../../shared/ui/Field'
import { t } from '../../shared/i18n/index.ts'

/**
 * Что происходило на доске.
 *
 * Третий вид на ту же доску. Доска показывает, как дела обстоят сейчас,
 * таблица — что где стоит, а этот вид отвечает на вопрос, которого
 * не задать ни той, ни другой: что изменилось, пока меня не было.
 * Ответ на него копился в `card_events` с самого начала и читался
 * только по одной карточке, из её истории.
 *
 * Отбор «только про меня» — события карточек, где я исполнитель,
 * и реплики, где меня упомянули. Свои действия из него не вычитаются:
 * вернувшийся из отпуска хочет видеть и то, что делал сам до отъезда,
 * а «кто» написано в каждой строке.
 */
export function Changes({
  boardId,
  fields,
  onOpenCard,
}: {
  boardId: string
  /** Поля доски: своё поле в ленте названо по имени, а не ссылкой. */
  fields: CardField[]
  onOpenCard: (id: string) => void
}) {
  const [events, setEvents] = useState<BoardEvent[] | null>(null)
  const [next, setNext] = useState<number | null>(null)
  const [mine, setMine] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    (before?: number) => {
      api
        .boardEvents(boardId, undefined, before, mine)
        .then((feed) => {
          setEvents((prev) => (before && prev ? [...prev, ...feed.events] : feed.events))
          setNext(feed.next)
        })
        .catch((e) => setError(e instanceof Error ? e.message : t.board.feedFailed))
    },
    [boardId, mine],
  )

  // Переключение отбора перечитывает ленту с начала: дописывать к чужому
  // списку отобранное значило бы показать смесь двух ответов.
  useEffect(() => {
    setEvents(null)
    load()
  }, [load])

  return (
    <div className="changes">
      {/* Подпись отдельной строкой: рядом с чекбоксом она читается
          как продолжение его названия. */}
      <div className="stack stack--tight">
        <label className="row row--tight">
          <input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} />
          <span>{t.board.onlyMine}</span>
        </label>
        <p className="muted small">
          {mine
            ? t.board.mineExplain
            : t.board.allExplain}
        </p>
      </div>

      <ScreenError>{error}</ScreenError>
      {events === null && !error && <p className="muted small">{t.common.reading}</p>}
      {events?.length === 0 && (
        <p className="muted small">
          {mine
            ? t.board.mineEmpty
            : t.board.allEmpty}
        </p>
      )}

      {events && events.length > 0 && (
        <ol className="feed">
          {events.map((event) => (
            <li key={event.id}>
              <span className="feed-dot" aria-hidden="true" />
              <div className="member-who">
                <span>
                  <button className="link" onClick={() => onOpenCard(event.cardId)}>
                    {event.cardTitle}
                  </button>{' '}
                  — {eventText(event, fields)}
                </span>
                <span className="muted small">
                  {actorText(event.actor)} · {timeText(event.at)}
                </span>
              </div>
            </li>
          ))}
        </ol>
      )}

      {next && (
        <Button kind="quiet" onClick={() => load(next)}>
          {t.common.showMore}
        </Button>
      )}
    </div>
  )
}
