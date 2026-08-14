import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, NetworkError, api } from './api'
import type { Conflict, Placement } from './api'
import {
  applyPatch,
  fromSnapshot,
  reconcileColumn,
  renderOrder,
} from './boardModel'
import type { BaseState, MoveCommand } from './boardModel'

export type Notice = {
  id: string
  text: string
  tone: 'info' | 'warning'
  /** Повтор идёт с тем же operationId, поэтому «дважды» ничего не произойдёт. */
  retry?: () => void
}

export function useBoard(boardId: string | null) {
  const [base, setBase] = useState<BaseState | null>(null)
  const [queue, setQueue] = useState<MoveCommand[]>([])
  const [notices, setNotices] = useState<Notice[]>([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const sending = useRef(false)

  const notify = useCallback((text: string, tone: Notice['tone'], retry?: () => void) => {
    const id = crypto.randomUUID()
    setNotices((list) => [...list, { id, text, tone, retry }])
    if (!retry) setTimeout(() => setNotices((list) => list.filter((n) => n.id !== id)), 4000)
  }, [])

  const dismiss = useCallback((id: string) => {
    setNotices((list) => list.filter((n) => n.id !== id))
  }, [])

  const reload = useCallback(async () => {
    if (!boardId) return
    setLoading(true)
    setLoadError(null)
    try {
      const snap = await api.snapshot(boardId)
      setBase(fromSnapshot(snap))
      setQueue([])
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : 'Не удалось загрузить доску')
    } finally {
      setLoading(false)
    }
  }, [boardId])

  useEffect(() => {
    void reload()
  }, [reload])

  // Очередь разбирается строго по одной команде: сервер сериализует
  // операции по доске, и отправлять их пачкой — значит гадать о порядке.
  useEffect(() => {
    if (!boardId || sending.current || queue.length === 0) return
    const command = queue[0]
    sending.current = true

    void (async () => {
      try {
        const result = await api.operation(boardId, command.operationId, 'MOVE_CARD', {
          cardId: command.cardId,
          toColumnId: command.toColumnId,
          ...command.placement,
        })
        setBase((current) => (current ? applyPatch(current, result) : current))
        setQueue((list) => list.filter((c) => c.operationId !== command.operationId))
      } catch (e) {
        setQueue((list) => list.filter((c) => c.operationId !== command.operationId))

        if (e instanceof ApiError && e.isConflict) {
          const conflict = e.body as Conflict
          setBase((current) => {
            if (!current) return current
            if (conflict.columnId && conflict.currentOrder) {
              const reconciled = reconcileColumn(current, conflict.columnId, conflict.currentOrder)
              if (reconciled) return reconciled
            }
            // сервер прислал карточки, которых мы не знаем: своими силами
            // не сойтись, нужен полный снимок
            void reload()
            return current
          })
          notify(conflict.error ?? 'Доска изменилась, пока вы перетаскивали карточку', 'warning')
          return
        }

        if (e instanceof NetworkError) {
          // Карточка уже вернулась на место — команда убрана из очереди.
          // Предлагаем повтор с тем же operationId: если операция всё-таки
          // дошла до сервера, второй раз она не выполнится.
          notify(`${e.message}. Карточка вернулась на место.`, 'warning', () =>
            setQueue((list) => [...list, command]),
          )
          return
        }

        notify(e instanceof Error ? e.message : 'Не удалось переместить карточку', 'warning')
      } finally {
        sending.current = false
      }
    })()
  }, [boardId, queue, notify, reload])

  const order = useMemo(() => (base ? renderOrder(base, queue) : {}), [base, queue])

  /** Перемещение применяется мгновенно, подтверждение приходит потом. */
  const moveCard = useCallback(
    (cardId: string, toColumnId: string, placement: Placement) => {
      setBase((current) => {
        if (!current) return current
        const card = current.cards[cardId]
        if (!card) return current
        setQueue((list) => [
          ...list,
          {
            operationId: crypto.randomUUID(),
            cardId,
            toColumnId,
            placement,
            fromColumnId: card.columnId,
          },
        ])
        return current
      })
    },
    [],
  )

  // Создание, переименование и удаление ждут ответа сервера: пользователь
  // в этот момент печатает или подтверждает, и 30 миллисекунд незаметны.
  // Мгновенным должно быть перетаскивание — его и делаем оптимистичным.
  const run = useCallback(
    async (type: string, payload: unknown, failureText: string) => {
      if (!boardId) return
      try {
        const result = await api.operation(boardId, crypto.randomUUID(), type, payload)
        setBase((current) => (current ? applyPatch(current, result) : current))
      } catch (e) {
        notify(e instanceof Error ? `${failureText}: ${e.message}` : failureText, 'warning')
      }
    },
    [boardId, notify],
  )

  const createCard = useCallback(
    (columnId: string, title: string) =>
      run('CREATE_CARD', { columnId, title, place: 'end' }, 'Не удалось создать карточку'),
    [run],
  )
  const renameCard = useCallback(
    (cardId: string, title: string) =>
      run('UPDATE_CARD', { cardId, title }, 'Не удалось переименовать карточку'),
    [run],
  )
  const archiveCard = useCallback(
    (cardId: string) => run('ARCHIVE_CARD', { cardId }, 'Не удалось удалить карточку'),
    [run],
  )
  const createColumn = useCallback(
    (name: string) => run('CREATE_COLUMN', { name }, 'Не удалось создать колонку'),
    [run],
  )
  const renameColumn = useCallback(
    (columnId: string, name: string) =>
      run('RENAME_COLUMN', { columnId, name }, 'Не удалось переименовать колонку'),
    [run],
  )

  return {
    base,
    order,
    pending: queue.length,
    loading,
    loadError,
    notices,
    dismiss,
    reload,
    moveCard,
    createCard,
    renameCard,
    archiveCard,
    createColumn,
    renameColumn,
  }
}
