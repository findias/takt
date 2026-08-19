import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, NetworkError, api } from '../../shared/api/index.ts'
import type {
  Card,
  ColumnKind,
  Conflict,
  LinkKind,
  Placement,
  Priority,
} from '../../shared/api/index.ts'

/** Подмножество свойств колонки, которое меняет одна операция. */
export type ColumnPatch = {
  kind?: ColumnKind
  isStartedPoint?: boolean
  isFinishedPoint?: boolean
  policy?: string
  wipLimitHard?: boolean
}
import {
  applyPatch,
  fromSnapshot,
  reconcileColumn,
  renderOrder,
} from '../../entities/board/model.ts'
import { PRIORITY_NAMES, cardsLabel } from '../../entities/card/model.ts'
import type { BaseState, MoveCommand } from '../../entities/board/model.ts'

/**
 * Как хук сообщает о том, что пошло не так.
 *
 * Раньше он держал собственный список уведомлений и сам заводил таймеры
 * их исчезновения. Это работа показа, а не работа с данными: теперь хук
 * зовёт функцию, а очередь, время жизни и разметку держит ToastHost.
 * Повтор идёт с тем же operationId, поэтому «дважды» ничего не произойдёт.
 */
export type Notify = (message: {
  text: string
  tone: 'info' | 'warning'
  action?: { label: string; onAct: () => void }
}) => void

export function useBoard(boardId: string | null, notify: Notify) {
  const [base, setBase] = useState<BaseState | null>(null)
  const [queue, setQueue] = useState<MoveCommand[]>([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  // Отказ отказу рознь: 503 значит «сервер перезапускают, попробуйте
  // ещё», а 404 — «этой доски у вас нет», и повторять его до скончания
  // века незачем. Код держим отдельно, потому что различать по тексту
  // значит сломать экран при первой правке формулировки.
  const [loadStatus, setLoadStatus] = useState<number | null>(null)
  // Убранная в архив доска — не ошибка, а положение дел, и лечится оно
  // одной кнопкой. Отличаем по коду ответа, а не по тексту сообщения:
  // разбирать текст значит сломать экран при первой правке формулировки.
  const [archived, setArchived] = useState(false)
  const sending = useRef(false)
  // Зеркало версии доски. Нужно затем, чтобы догон не зависел от самого
  // снимка: иначе функция пересоздаётся на каждое изменение, а вместе
  // с ней переподписывается поток — и мы теряем события ровно тогда,
  // когда их больше всего.
  const latest = useRef<number | null>(null)
  const titles = useRef(new Map<string, string>())
  // Зеркало снимка. Нужно, чтобы прочитать прежнее значение до изменения:
  // читать его внутри обновления состояния нельзя — обновление
  // выполняется позже, и при быстром отказе откатывать оказывается нечего.
  const shown = useRef<BaseState | null>(null)
  // Сколько правок было у каждой карточки: по номеру видно, устарел ли
  // ответ, пришедший из сети.
  const edits = useRef(new Map<string, number>())

  const reload = useCallback(async () => {
    if (!boardId) return
    setLoading(true)
    setLoadError(null)
    setArchived(false)
    try {
      const snap = await api.snapshot(boardId)
      // Снимок не той формы — это не «пустая доска», а расхождение
      // клиента с сервером: браузер держит старую сборку, а сервер
      // уехал вперёд (или наоборот). Раньше отсюда вылетало
      // «a.columns is not iterable» — сообщение чужой библиотеки,
      // по которому человек не может ни понять, что случилось,
      // ни догадаться, что помогает обновление страницы.
      if (!Array.isArray(snap.columns) || !Array.isArray(snap.cards)) {
        throw new Error(
          'Ответ сервера не похож на снимок доски. Похоже, страница открыта давно ' +
            'и устарела: обновите её (Ctrl+R или Cmd+R).',
        )
      }
      setLoadStatus(null)
      setBase(fromSnapshot(snap))
      setQueue([])
    } catch (e) {
      if (e instanceof ApiError && e.body?.code === 'board_archived') setArchived(true)
      setLoadStatus(e instanceof ApiError ? e.status : null)
      setLoadError(e instanceof Error ? e.message : 'Не удалось загрузить доску')
    } finally {
      setLoading(false)
    }
  }, [boardId])

  useEffect(() => {
    void reload()
  }, [reload])

  // Не загрузилось — пробуем ещё, сами.
  //
  // «Повторить» на экране остаётся, но нажимать её ради того, чтобы
  // пережить перезапуск сервера, человек не должен: доска обязана
  // сойтись сама, как сходится после обрыва потока. Проход
  // по интерфейсу поймал ровно это — вторая вкладка застряла
  // на «Ошибка 503» и после подъёма сервера так и стояла.
  //
  // Архивная доска сюда не попадает: это не сбой, а положение дел,
  // и повторять запрос за ним незачем.
  useEffect(() => {
    if (!loadError || archived) return
    // Повторяем только то, что может пройти со второй попытки: обрыв
    // связи (кода нет вовсе) и отказ самого сервера. «Не найдено»
    // и «нельзя» повтором не лечатся — там нужен другой ход, и он
    // назван на экране.
    const стоит = loadStatus === null || loadStatus >= 500
    if (!стоит) return
    const again = window.setTimeout(() => void reload(), 5000)
    return () => window.clearTimeout(again)
  }, [loadError, loadStatus, archived, reload])

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

        // Кончившаяся сессия уводит на вход целым экраном, и тост
        // об этом же висел бы поверх формы: одно событие, два разных
        // сообщения — человек ищет две поломки вместо одной.
        if (e instanceof ApiError && e.status === 401) return

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
          notify({
            text: conflict.error ?? 'Доска изменилась, пока вы перетаскивали карточку',
            tone: 'warning',
          })
          return
        }

        if (e instanceof NetworkError) {
          // Карточка уже вернулась на место — команда убрана из очереди.
          // Предлагаем повтор с тем же operationId: если операция всё-таки
          // дошла до сервера, второй раз она не выполнится.
          notify({
            text: `${e.message}. Карточка вернулась на место.`,
            tone: 'warning',
            action: { label: 'Повторить', onAct: () => setQueue((list) => [...list, command]) },
          })
          return
        }

        notify({
          text: e instanceof Error ? e.message : 'Не удалось переместить карточку',
          tone: 'warning',
        })
      } finally {
        sending.current = false
      }
    })()
  }, [boardId, queue, notify, reload])

  /**
   * Догнать пропущенное.
   *
   * Раньше на каждое чужое изменение перечитывался весь снимок доски:
   * на десяти карточках незаметно, на трёхстах заметно, и заметно тем
   * сильнее, чем больше людей работает — каждый из них перечитывает
   * доску целиком на каждое чужое действие.
   *
   * Теперь спрашиваем только патчи после нашей версии и применяем их
   * тем же кодом, которым применяем ответы на собственные операции.
   * Если сервер отвечает «догнать нечем» — перечитываем снимок: это
   * редкий путь, и лучше он будет медленным, чем незаметно неверным.
   */
  useEffect(() => {
    latest.current = base?.info.version ?? null
  }, [base?.info.version])

  useEffect(() => {
    shown.current = base
    if (!base) return
    for (const card of Object.values(base.cards)) titles.current.set(card.id, card.title)
  }, [base])

  const catchUp = useCallback(async () => {
    if (!boardId) return
    const from = latest.current
    if (from === null) return
    try {
      const catchup = await api.changes(boardId, from)
      if (catchup.full) {
        await reload()
        return
      }
      setBase((current) => {
        if (!current) return current
        // Пока мы ходили за патчами, доска могла уехать вперёд: ответ
        // на собственную операцию мог прийти раньше. Применяем только
        // то, чего у нас ещё нет, — по версии, названной сервером.
        let next = current
        for (const result of catchup.results) {
          if (result.version <= next.info.version) continue
          next = applyPatch(next, result)
        }
        return next
      })
    } catch {
      // Не догнали — перечитаем целиком. Молчание здесь осознанное:
      // человеку об этом знать незачем, доска сойдётся сама.
      await reload()
    }
  }, [boardId, reload])

  // Поток изменений: сервер сообщает «доска доехала до такой-то версии».
  //
  // Своё же изменение мы уже ждём ответом на операцию, поэтому чужой
  // автор — единственный повод дёрнуться.
  useEffect(() => {
    if (!boardId) return
    let source: EventSource
    let retry: number | undefined
    let alive = true

    const listen = (stream: EventSource) => {
      stream.addEventListener('board', (event) => {
        try {
          const change = JSON.parse((event as MessageEvent).data) as {
            version: number
            actorId: string
          }
          setBase((current) => {
            if (!current) return current
            // Версия не новее нашей — новость уже учтена.
            if (change.version <= current.info.version) return current
            // Догоняем вне setBase: обновление состояния должно
            // оставаться чистым.
            queueMicrotask(catchUp)
            return current
          })
        } catch {
          // Непонятное сообщение — не повод ронять доску.
        }
      })

      // Сам EventSource переподключается только после обрыва связи.
      // Ответ не-200 он считает окончательным и закрывается насовсем —
      // а это и приходит, когда сервер перезапускают: браузер стучится
      // в ещё не поднявшийся порт, получает отказ и больше не
      // возвращается. Доска после этого молча стоит устаревшей, и узнать
      // об этом можно только по чужой правке, которая не приехала.
      // Проход по интерфейсу поймал ровно это: вторая вкладка осталась
      // на прежней версии навсегда.
      //
      // Поэтому закрытый поток открываем заново — через паузу, чтобы
      // не долбить поднимающийся сервер, — и сразу догоняем пропущенное.
      stream.addEventListener('error', () => {
        if (!alive || stream.readyState !== EventSource.CLOSED) return
        retry = window.setTimeout(() => {
          if (!alive) return
          open()
          void catchUp()
        }, 3000)
      })
    }

    const open = () => {
      source = new EventSource(`/api/boards/${boardId}/stream`)
      listen(source)
    }

    open()

    return () => {
      alive = false
      if (retry) window.clearTimeout(retry)
      source?.close()
    }
  }, [boardId, catchUp])

  const order = useMemo(() => (base ? renderOrder(base, queue) : {}), [base, queue])

  /** Перемещение применяется мгновенно, подтверждение приходит потом. */
  const moveCard = useCallback((cardId: string, toColumnId: string, placement: Placement) => {
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
  }, [])

  /**
   * Изменение карточки, видимое сразу.
   *
   * Раньше переименование, описание и оценка ждали ответа сервера:
   * «пользователь всё равно печатает, тридцать миллисекунд незаметны».
   * Тридцать — да, а двести на медленной сети — нет: поле возвращается
   * к прежнему значению и человек начинает печатать заново.
   *
   * Откат точечный, а не «вернуть весь снимок»: пока запрос идёт, доска
   * могла уехать от чужих изменений, и возвращать её целиком значило бы
   * стирать чужую работу. Возвращаем ровно ту карточку, которую трогали.
   *
   * Ответ на устаревшую правку не применяется. Пока правку делали
   * по одной — печатали название и уходили из поля, — это было
   * незаметно; шаговый ввод оценки показал это сразу: три нажатия «+»
   * подряд давали двойку, потому что ответ на первую правку приходил
   * после того, как на экране уже стояла вторая, и возвращал её назад.
   * Считаем правки на карточку и применяем ответ только на последнюю;
   * версию доски догонит поток изменений — это уже предусмотренный путь.
   */
  const patchCard = useCallback(
    async (
      cardId: string,
      change: Partial<Card>,
      type: string,
      payload: unknown,
      failureText: string,
    ) => {
      if (!boardId) return
      const previous = shown.current?.cards[cardId] ?? null
      const seq = (edits.current.get(cardId) ?? 0) + 1
      edits.current.set(cardId, seq)
      setBase((current) => {
        if (!current) return current
        const card = current.cards[cardId]
        if (!card) return current
        return { ...current, cards: { ...current.cards, [cardId]: { ...card, ...change } } }
      })

      try {
        const result = await api.operation(boardId, crypto.randomUUID(), type, payload)
        if (edits.current.get(cardId) !== seq) return
        setBase((current) => (current ? applyPatch(current, result) : current))
      } catch (e) {
        // Откатывать тоже нечего, если поверх уже легла новая правка:
        // вернулось бы не прежнее значение, а позапрошлое.
        if (previous && edits.current.get(cardId) === seq) {
          setBase((current) =>
            current ? { ...current, cards: { ...current.cards, [cardId]: previous } } : current,
          )
        }
        notify({
          text: e instanceof Error ? `${failureText}: ${e.message}` : failureText,
          tone: 'warning',
        })
      }
    },
    [boardId, notify],
  )

  // Создание и работа со структурой доски по-прежнему ждут ответа:
  // у новой карточки нет идентификатора до ответа сервера, а колонки
  // заводят раз в месяц.
  const run = useCallback(
    async (type: string, payload: unknown, failureText: string) => {
      if (!boardId) return
      try {
        const result = await api.operation(boardId, crypto.randomUUID(), type, payload)
        setBase((current) => (current ? applyPatch(current, result) : current))
      } catch (e) {
        notify({
          text: e instanceof Error ? `${failureText}: ${e.message}` : failureText,
          tone: 'warning',
        })
      }
    },
    [boardId, notify],
  )

  // Название нужно сообщению об отмене, а к моменту показа карточки
  // на доске уже нет. Читаем через ref, чтобы не тащить весь снимок
  // в зависимости обработчика.
  const titleOf = useCallback((cardId: string) => titles.current.get(cardId) ?? null, [])

  const createCard = useCallback(
    (columnId: string, title: string) =>
      run('CREATE_CARD', { columnId, title, place: 'end' }, 'Не удалось создать карточку'),
    [run],
  )
  const renameCard = useCallback(
    (cardId: string, title: string) =>
      patchCard(
        cardId,
        { title },
        'UPDATE_CARD',
        { cardId, title },
        'Не удалось переименовать карточку',
      ),
    [patchCard],
  )
  /**
   * Убрать карточку — с возможностью вернуть.
   *
   * Диалог «вы уверены?» здесь был бы вопросом, ответ на который в девяти
   * случаях из десяти известен: его закрывают не читая. Дешевле сделать
   * действие обратимым и предложить отмену — обычный случай проходит без
   * лишнего нажатия, редкая ошибка исправляется одним.
   */
  const archiveCard = useCallback(
    async (cardId: string) => {
      const title = titleOf(cardId)
      await run('ARCHIVE_CARD', { cardId }, 'Не удалось убрать карточку')
      notify({
        text: title ? `«${title}» убрана в архив` : 'Карточка убрана в архив',
        tone: 'info',
        action: {
          label: 'Вернуть',
          onAct: () => void run('RESTORE_CARD', { cardId }, 'Не удалось вернуть карточку'),
        },
      })
    },
    [run, notify, titleOf],
  )
  /**
   * Одно действие над многими карточками.
   *
   * По операции на карточку, одна за другой, а не пачкой: сервер
   * сериализует операции по доске, и «пачка» разложилась бы в ту же
   * очередь — только отказ на середине стал бы неразличим, а половина
   * сделанного осталась бы без имени.
   *
   * Возвращаются оба списка. Сделанное нужно для отмены — вернуть
   * можно ровно то, что прошло; несделанное нужно, чтобы назвать его
   * поимённо: «не удалось» без имён отправляет искать, что именно.
   */
  const applyToMany = useCallback(
    async (cardIds: string[], type: string, payload: (cardId: string) => unknown) => {
      const done: string[] = []
      const failed: string[] = []
      if (!boardId) return { done, failed }
      for (const cardId of cardIds) {
        try {
          const result = await api.operation(boardId, crypto.randomUUID(), type, payload(cardId))
          setBase((current) => (current ? applyPatch(current, result) : current))
          done.push(cardId)
        } catch {
          failed.push(cardId)
        }
      }
      return { done, failed }
    },
    [boardId],
  )

  /** Отказ называет карточки по именам, но не все: двадцать названий
   *  в одну строку — это не сообщение, а список. */
  const namesOf = useCallback(
    (cardIds: string[]) => {
      const names = cardIds.map((id) => titles.current.get(id) ?? 'без названия')
      if (names.length <= 3) return names.join(', ')
      return `${names.slice(0, 3).join(', ')} и ещё ${names.length - 3}`
    },
    [],
  )

  /**
   * Перенести выделенные в колонку — с возможностью вернуть.
   *
   * Прежние колонки запоминаются до переноса: без них отмена вернула бы
   * всё в одну кучу, а карточки пришли из разных мест.
   */
  const moveMany = useCallback(
    async (cardIds: string[], toColumnId: string) => {
      const was = new Map<string, string>()
      for (const id of cardIds) {
        const columnId = shown.current?.cards[id]?.columnId
        if (columnId) was.set(id, columnId)
      }
      const name = shown.current?.columns[toColumnId]?.name ?? 'другую колонку'
      const { done, failed } = await applyToMany(cardIds, 'MOVE_CARD', (cardId) => ({
        cardId,
        toColumnId,
        place: 'end',
      }))
      if (done.length > 0) {
        notify({
          text: `${cardsLabel(done.length)} перенесено в «${name}»`,
          tone: 'info',
          action: {
            label: 'Вернуть',
            onAct: () => {
              void (async () => {
                for (const cardId of done) {
                  const back = was.get(cardId)
                  if (!back) continue
                  await applyToMany([cardId], 'MOVE_CARD', () => ({
                    cardId,
                    toColumnId: back,
                    place: 'end',
                  }))
                }
              })()
            },
          },
        })
      }
      if (failed.length > 0) {
        notify({ text: `Не удалось перенести: ${namesOf(failed)}`, tone: 'warning' })
      }
      return done.length
    },
    [applyToMany, notify, namesOf],
  )

  /** Убрать выделенные в архив — одним сообщением и одной отменой
   *  на всех: двадцать уведомлений подряд не читает никто. */
  const archiveMany = useCallback(
    async (cardIds: string[]) => {
      const { done, failed } = await applyToMany(cardIds, 'ARCHIVE_CARD', (cardId) => ({ cardId }))
      if (done.length > 0) {
        notify({
          text: `${cardsLabel(done.length)} убрано в архив`,
          tone: 'info',
          action: {
            label: 'Вернуть',
            onAct: () => void applyToMany(done, 'RESTORE_CARD', (cardId) => ({ cardId })),
          },
        })
      }
      if (failed.length > 0) {
        notify({ text: `Не удалось убрать: ${namesOf(failed)}`, tone: 'warning' })
      }
      return done.length
    },
    [applyToMany, notify, namesOf],
  )

  /**
   * Пометить выделенные — с возможностью снять.
   *
   * Метка ставится, а не переключается у каждой: «пометить десять
   * карточек» — это одно решение, а переключение дало бы половину
   * помеченных и половину снятых, то есть результат, который зависит
   * от того, что было раньше.
   */
  const labelMany = useCallback(
    async (cardIds: string[], labelId: string) => {
      const name = shown.current?.labels.find((l) => l.id === labelId)?.name ?? 'метка'
      const { done, failed } = await applyToMany(cardIds, 'LABEL_CARD', (cardId) => ({
        cardId,
        labelId,
      }))
      if (done.length > 0) {
        notify({
          text: `${cardsLabel(done.length)} помечено: «${name}»`,
          tone: 'info',
          action: {
            label: 'Снять',
            onAct: () =>
              void applyToMany(done, 'UNLABEL_CARD', (cardId) => ({ cardId, labelId })),
          },
        })
      }
      if (failed.length > 0) {
        notify({ text: `Не удалось пометить: ${namesOf(failed)}`, tone: 'warning' })
      }
      return done.length
    },
    [applyToMany, notify, namesOf],
  )

  /** Назначить исполнителя на выделенные. Именно добавить: у карточки
   *  исполнителей несколько, и «назначить» никого не снимает. */
  const assignMany = useCallback(
    async (cardIds: string[], userId: string) => {
      const who = shown.current?.people[userId] ?? 'человек'
      const { done, failed } = await applyToMany(cardIds, 'ASSIGN_CARD', (cardId) => ({
        cardId,
        userId,
      }))
      if (done.length > 0) {
        notify({
          text: `${cardsLabel(done.length)} назначено: ${who}`,
          tone: 'info',
          action: {
            label: 'Снять',
            onAct: () =>
              void applyToMany(done, 'UNASSIGN_CARD', (cardId) => ({ cardId, userId })),
          },
        })
      }
      if (failed.length > 0) {
        notify({ text: `Не удалось назначить: ${namesOf(failed)}`, tone: 'warning' })
      }
      return done.length
    },
    [applyToMany, notify, namesOf],
  )

  /**
   * Проставить уровень выделенным — с возвратом прежних.
   *
   * Прежние уровни запоминаются до правки, как колонки при переносе:
   * без них отмена свалила бы всё в один уровень, а карточки пришли
   * с разными.
   */
  const prioritiseMany = useCallback(
    async (cardIds: string[], priority: Priority) => {
      const was = new Map<string, Priority>()
      for (const id of cardIds) {
        const level = shown.current?.cards[id]?.priority
        if (level) was.set(id, level)
      }
      const { done, failed } = await applyToMany(cardIds, 'UPDATE_CARD', (cardId) => ({
        cardId,
        priority,
      }))
      if (done.length > 0) {
        notify({
          text: `${cardsLabel(done.length)}: приоритет ${PRIORITY_NAMES[priority].toLowerCase()}`,
          tone: 'info',
          action: {
            label: 'Вернуть',
            onAct: () => {
              void (async () => {
                for (const cardId of done) {
                  const back = was.get(cardId)
                  if (!back) continue
                  await applyToMany([cardId], 'UPDATE_CARD', () => ({ cardId, priority: back }))
                }
              })()
            },
          },
        })
      }
      if (failed.length > 0) {
        notify({ text: `Не удалось изменить приоритет: ${namesOf(failed)}`, tone: 'warning' })
      }
      return done.length
    },
    [applyToMany, notify, namesOf],
  )

  /**
   * Удалить карточку насовсем.
   *
   * В отличие от архивации, здесь нет отмены и потому есть вопрос:
   * обратимое действие спрашивать не должно, необратимое обязано.
   * Сам вопрос задаёт доска — здесь только последствие.
   *
   * Патч перечитывается снимком: удаление уносит связи, а их прогресс
   * считается у родителя, который может лежать на другой доске.
   */
  const deleteCard = useCallback(
    async (cardId: string) => {
      const title = titleOf(cardId)
      await run('DELETE_CARD', { cardId }, 'Не удалось удалить карточку')
      reload()
      notify({
        text: title ? `«${title}» удалена навсегда` : 'Карточка удалена навсегда',
        tone: 'info',
      })
    },
    [run, reload, notify, titleOf],
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
  // null снимает оценку, отсутствие поля её не трогает: иначе
  // переименование стирало бы оценку.
  /**
   * Метка на карточке.
   *
   * Оптимистично и точечно: метка — самое частое действие после
   * перемещения, и ждать ответа ради галочки, которая либо есть, либо
   * нет, незачем. Откат возвращает прежний список меток именно этой
   * карточки.
   */
  const toggleLabel = useCallback(
    async (cardId: string, labelId: string, on: boolean) => {
      if (!boardId) return
      const previous = shown.current?.cardLabels[cardId] ?? []
      setBase((current) => {
        if (!current) return current
        const now = current.cardLabels[cardId] ?? []
        const next = on ? [...new Set([...now, labelId])] : now.filter((id) => id !== labelId)
        return { ...current, cardLabels: { ...current.cardLabels, [cardId]: next } }
      })
      try {
        await api.operation(boardId, crypto.randomUUID(), on ? 'LABEL_CARD' : 'UNLABEL_CARD', {
          cardId,
          labelId,
        })
      } catch (e) {
        setBase((current) =>
          current
            ? { ...current, cardLabels: { ...current.cardLabels, [cardId]: previous } }
            : current,
        )
        notify({
          text:
            e instanceof Error
              ? `Не удалось изменить метки: ${e.message}`
              : 'Не удалось изменить метки',
          tone: 'warning',
        })
      }
    },
    [boardId, notify],
  )

  /**
   * Назначить или снять исполнителя.
   *
   * Устроено как метки, и по той же причине: исполнителей несколько,
   * список приходит с сервера целиком, а на экране изменение должно
   * появляться сразу — назначение делают в разговоре, и ждать ответа
   * сервера там нечего. При отказе список возвращается к прежнему,
   * прочитанному из зеркала: к моменту отказа состояние уже другое.
   */
  const assignCard = useCallback(
    async (cardId: string, userId: string, on: boolean) => {
      if (!boardId) return
      const previous = shown.current?.cardAssignees[cardId] ?? []
      setBase((current) => {
        if (!current) return current
        const now = current.cardAssignees[cardId] ?? []
        const next = on ? [...new Set([...now, userId])] : now.filter((id) => id !== userId)
        return { ...current, cardAssignees: { ...current.cardAssignees, [cardId]: next } }
      })
      try {
        await api.operation(boardId, crypto.randomUUID(), on ? 'ASSIGN_CARD' : 'UNASSIGN_CARD', {
          cardId,
          userId,
        })
      } catch (e) {
        setBase((current) =>
          current
            ? { ...current, cardAssignees: { ...current.cardAssignees, [cardId]: previous } }
            : current,
        )
        notify({
          text:
            e instanceof Error
              ? `Не удалось изменить исполнителей: ${e.message}`
              : 'Не удалось изменить исполнителей',
          tone: 'warning',
        })
      }
    },
    [boardId, notify],
  )

  /**
   * Приоритет карточки.
   *
   * Оптимистично, как переименование: уровень — свойство карточки,
   * а не перенос, и ждать ответа сервера, чтобы показать слово,
   * незачем.
   */
  const prioritiseCard = useCallback(
    (cardId: string, priority: Priority) =>
      patchCard(
        cardId,
        { priority },
        'UPDATE_CARD',
        { cardId, priority },
        'Не удалось изменить приоритет',
      ),
    [patchCard],
  )
  /** Дата обязательства. Пустая снимает его: «обязательства нет»
   *  и «дата неизвестна» — разные вещи, и различает их null. */
  const commitCard = useCallback(
    (cardId: string, dueOn: string | null) =>
      patchCard(
        cardId,
        { dueOn },
        'UPDATE_CARD',
        { cardId, dueOn },
        'Не удалось изменить дату обязательства',
      ),
    [patchCard],
  )
  const estimateCard = useCallback(
    (cardId: string, estimate: number | null) =>
      patchCard(
        cardId,
        { estimate },
        'UPDATE_CARD',
        { cardId, estimate },
        'Не удалось сохранить оценку',
      ),
    [patchCard],
  )

  const describeCard = useCallback(
    (cardId: string, description: string) =>
      patchCard(
        cardId,
        { description },
        'UPDATE_CARD',
        { cardId, description },
        'Не удалось сохранить описание',
      ),
    [patchCard],
  )

  // Связи и блокировки меняют больше, чем возвращает патч: прогресс
  // родителя, состав связей, карточки на других досках. Дешевле и честнее
  // перечитать снимок, чем собирать это по кускам на клиенте — действия
  // редкие, в отличие от перетаскивания.
  const runAndReload = useCallback(
    async (type: string, payload: unknown, failureText: string) => {
      await run(type, payload, failureText)
      reload()
    },
    [run, reload],
  )

  // Итерация не меняет ни порядок карточек, ни версию доски, но меняет
  // состав, который показывает снимок, — поэтому после неё перечитываем.
  // Пустое значение снимает поле: «поля нет» и «поле пустое» — одно и то
  // же, и третьего состояния заводить незачем.
  const setCardField = useCallback(
    (cardId: string, fieldId: string, value: string | number | boolean | null) =>
      runAndReload('SET_CARD_FIELD', { cardId, fieldId, value }, 'Не удалось сохранить поле'),
    [runAndReload],
  )

  const addToIteration = useCallback(
    (cardId: string, iterationId: string) =>
      runAndReload('ADD_TO_ITERATION', { cardId, iterationId }, 'Не удалось добавить в итерацию'),
    [runAndReload],
  )
  const removeFromIteration = useCallback(
    (cardId: string, iterationId: string) =>
      runAndReload(
        'REMOVE_FROM_ITERATION',
        { cardId, iterationId },
        'Не удалось убрать из итерации',
      ),
    [runAndReload],
  )

  /**
   * Завести подзадачу одним действием.
   *
   * Не «создать карточку, потом связать»: два вызова с клиента дают два
   * способа оборваться посередине, и оба оставляют мусор — карточку без
   * родителя или связь на несозданное. Сервер делает это одной
   * транзакцией.
   */
  const createSubtask = useCallback(
    (parentCardId: string, title: string, columnId?: string, boardId?: string) =>
      runAndReload(
        'CREATE_SUBTASK',
        { parentCardId, title, columnId, boardId },
        'Не удалось завести подзадачу',
      ),
    [runAndReload],
  )
  const linkCards = useCallback(
    (fromCard: string, toCard: string, kind: LinkKind) =>
      runAndReload('LINK_CARDS', { fromCard, toCard, kind }, 'Не удалось связать карточки'),
    [runAndReload],
  )
  const unlinkCards = useCallback(
    (fromCard: string, toCard: string, kind: LinkKind) =>
      runAndReload('UNLINK_CARDS', { fromCard, toCard, kind }, 'Не удалось убрать связь'),
    [runAndReload],
  )
  /**
   * Отметить работу сделанной, не двигая её по доске.
   *
   * Оптимистично, как переименование: флажок, который «нажимается»
   * через двести миллисекунд, ощущается сломанным сильнее, чем поле,
   * которое столько же думает. Ответ приносит и прогресс родителя —
   * его посчитал сервер, здесь считать нечего.
   */
  const setCardDone = useCallback(
    (cardId: string, done: boolean) =>
      patchCard(
        cardId,
        { doneAt: done ? new Date().toISOString() : null },
        'SET_CARD_DONE',
        { cardId, done },
        done ? 'Не удалось отметить сделанной' : 'Не удалось снять отметку',
      ),
    [patchCard],
  )
  // Держащая карточка — необязательная часть блокировки: «нет доступа
  // к стенду» карточки не имеет, а «ждём согласования сметы» имеет,
  // и по ней ходят.
  const blockCard = useCallback(
    (cardId: string, reason: string, blockingCard?: string) =>
      runAndReload(
        'BLOCK_CARD',
        { cardId, reason, blockingCard },
        'Не удалось отметить блокировку',
      ),
    [runAndReload],
  )
  const unblockCard = useCallback(
    (cardId: string) => runAndReload('UNBLOCK_CARD', { cardId }, 'Не удалось снять блокировку'),
    [runAndReload],
  )

  // Разметка колонки: вид, точки потока, политика входа, жёсткость лимита.
  // Не присланное поле не меняется — так устроена операция, — поэтому здесь
  // передаётся ровно то, что человек тронул.
  const updateColumn = useCallback(
    (columnId: string, patch: ColumnPatch) =>
      run('UPDATE_COLUMN', { columnId, ...patch }, 'Не удалось изменить колонку'),
    [run],
  )

  // null снимает лимит. Отсутствие поля ничего не меняет, поэтому «снять»
  // и «не трогать» приходится различать явно.
  const setColumnLimit = useCallback(
    (columnId: string, wipLimit: number | null) =>
      run('UPDATE_COLUMN', { columnId, wipLimit }, 'Не удалось изменить лимит колонки'),
    [run],
  )

  return {
    base,
    order,
    pending: queue.length,
    loading,
    loadError,
    loadStatus,
    archived,
    reload,
    moveCard,
    createCard,
    renameCard,
    describeCard,
    estimateCard,
    prioritiseCard,
    commitCard,
    archiveCard,
    moveMany,
    archiveMany,
    labelMany,
    assignMany,
    prioritiseMany,
    deleteCard,
    assignCard,
    toggleLabel,
    createSubtask,
    linkCards,
    unlinkCards,
    setCardDone,
    blockCard,
    unblockCard,
    setCardField,
    addToIteration,
    removeFromIteration,
    createColumn,
    renameColumn,
    updateColumn,
    setColumnLimit,
  }
}
