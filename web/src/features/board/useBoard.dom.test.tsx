// Хук состояния доски.
//
// Здесь живёт то, ради чего доска ощущается быстрой: перемещение
// применяется до ответа сервера, очередь разбирается по одной команде,
// а расхождение с сервером чинится точечно. Модели проверены отдельно
// и без DOM; здесь проверяется поведение во времени — что и когда уходит
// на сервер, и что видит человек, когда сервер отвечает не то.
//
// Сервер подменён: настоящий проверен интеграционными тестами на Go,
// а здесь интересны ровно ответы, которые он может дать.

import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Card, Column, Snapshot } from '../../shared/api/index.ts'

const snapshot = vi.fn<() => Promise<Snapshot>>()
const operation = vi.fn()

// Подменяется только объект api: классы ошибок остаются настоящими,
// потому что хук различает их через instanceof, и подделка проверяла бы
// подделку.
vi.mock('../../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../../shared/api')>()
  return { ...real, api: { ...real.api, snapshot, operation } }
})

const { ApiError, NetworkError } = await import('../../shared/api')
const { useBoard } = await import('./useBoard')

const COL_A = 'col-a'
const COL_B = 'col-b'

function column(id: string, name: string, position: string): Column {
  return {
    id,
    name,
    position,
    kind: 'queue',
    isStartedPoint: false,
    isFinishedPoint: false,
    policy: '',
    wipLimit: null,
    wipLimitHard: false,
  }
}

function card(id: string, columnId: string, position: string): Card {
  return {
    id,
    columnId,
    position,
    title: id,
    description: '',
    version: 1,
    columnEnteredAt: '2026-08-15T12:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
  }
}

function board(version = 1): Snapshot {
  return {
    board: { id: 'board', name: 'Доска', version, sleDays: null, sleProbability: 85 },
    columns: [column(COL_A, 'Очередь', 'a0'), column(COL_B, 'В работе', 'a1')],
    cards: [card('первая', COL_A, 'a0'), card('вторая', COL_A, 'a1')],
    links: [],
    linked: [],
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
  }
}

/** Поток изменений в тестах никуда не ходит, но его надо кем-то заменить. */
class FakeEventSource {
  static last: FakeEventSource | null = null
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  closed = false

  constructor(readonly url: string) {
    FakeEventSource.last = this
  }
  addEventListener(name: string, fn: (e: MessageEvent) => void) {
    ;(this.listeners[name] ??= []).push(fn)
  }
  close() {
    this.closed = true
  }
  emit(name: string, data: unknown) {
    for (const fn of this.listeners[name] ?? []) {
      fn(new MessageEvent(name, { data: JSON.stringify(data) }))
    }
  }
}

beforeEach(() => {
  snapshot.mockReset()
  operation.mockReset()
  snapshot.mockResolvedValue(board())
  FakeEventSource.last = null
  vi.stubGlobal('EventSource', FakeEventSource)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

/** Хук с уже загруженной доской. */
async function loaded() {
  const view = renderHook(() => useBoard('board'))
  await waitFor(() => expect(view.result.current.base).not.toBeNull())
  return view
}

describe('загрузка', () => {
  it('раскладывает снимок по колонкам в порядке позиций', async () => {
    const view = await loaded()
    expect(view.result.current.order[COL_A]).toEqual(['первая', 'вторая'])
    expect(view.result.current.order[COL_B]).toEqual([])
    expect(view.result.current.loadError).toBeNull()
  })

  it('не роняет доску, когда снимок не пришёл, и говорит об этом', async () => {
    snapshot.mockRejectedValueOnce(new NetworkError('Сеть недоступна'))
    const view = renderHook(() => useBoard('board'))
    await waitFor(() => expect(view.result.current.loadError).toBe('Сеть недоступна'))
    expect(view.result.current.loading).toBe(false)
  })
})

describe('перемещение', () => {
  // Смысл оптимистичного интерфейса: карточка уезжает под курсором,
  // а не после ответа сервера.
  it('видно сразу, до ответа сервера', async () => {
    let answer: (value: unknown) => void = () => {}
    operation.mockImplementation(() => new Promise((resolve) => (answer = resolve)))

    const view = await loaded()
    act(() => view.result.current.moveCard('первая', COL_B, { place: 'end' }))

    expect(view.result.current.order[COL_A]).toEqual(['вторая'])
    expect(view.result.current.order[COL_B]).toEqual(['первая'])
    expect(view.result.current.pending).toBe(1)

    await act(async () => {
      answer({
        version: 2,
        patch: { cards: [{ ...card('первая', COL_B, 'a2') }] },
      })
    })
    await waitFor(() => expect(view.result.current.pending).toBe(0))
    expect(view.result.current.order[COL_B]).toEqual(['первая'])
  })

  // Сервер сериализует операции по доске, поэтому слать их пачкой —
  // значит гадать о порядке применения.
  it('уходит на сервер строго по одной команде', async () => {
    const answers: ((value: unknown) => void)[] = []
    operation.mockImplementation(() => new Promise((resolve) => answers.push(resolve)))

    const view = await loaded()
    act(() => {
      view.result.current.moveCard('первая', COL_B, { place: 'end' })
      view.result.current.moveCard('вторая', COL_B, { place: 'end' })
    })

    await waitFor(() => expect(operation).toHaveBeenCalledTimes(1))
    expect(view.result.current.pending).toBe(2)
    // Обе уже видны на своих местах: очередь применяется поверх базы.
    expect(view.result.current.order[COL_B]).toEqual(['первая', 'вторая'])

    await act(async () => {
      answers[0]({ version: 2, patch: { cards: [card('первая', COL_B, 'a2')] } })
    })
    await waitFor(() => expect(operation).toHaveBeenCalledTimes(2))
  })
})

describe('расхождение с сервером', () => {
  // Конфликт с известным порядком чинится точечно: перезагружать доску
  // целиком из-за одной колонки — значит терять место и прокрутку.
  it('пересобирает колонку по присланному порядку и не перезапрашивает снимок', async () => {
    operation.mockRejectedValue(
      new ApiError(409, {
        error: 'Доска изменилась',
        columnId: COL_A,
        currentOrder: [
          { id: 'вторая', position: 'a0' },
          { id: 'первая', position: 'a1' },
        ],
      }),
    )

    const view = await loaded()
    expect(snapshot).toHaveBeenCalledTimes(1)

    await act(async () => {
      view.result.current.moveCard('первая', COL_A, { place: 'start' })
    })

    await waitFor(() => expect(view.result.current.pending).toBe(0))
    expect(view.result.current.order[COL_A]).toEqual(['вторая', 'первая'])
    expect(snapshot).toHaveBeenCalledTimes(1)
    expect(view.result.current.notices.map((n) => n.text)).toEqual(['Доска изменилась'])
  })

  // А вот незнакомая карточка в присланном порядке означает, что своими
  // силами не сойтись: нужен полный снимок.
  it('перечитывает доску, если в присланном порядке есть неизвестная карточка', async () => {
    operation.mockRejectedValue(
      new ApiError(409, {
        error: 'Доска изменилась',
        columnId: COL_A,
        currentOrder: [
          { id: 'чужая', position: 'a0' },
          { id: 'первая', position: 'a1' },
        ],
      }),
    )

    const view = await loaded()
    await act(async () => {
      view.result.current.moveCard('первая', COL_A, { place: 'start' })
    })

    await waitFor(() => expect(snapshot).toHaveBeenCalledTimes(2))
  })

  // Обрыв связи не должен оставлять карточку висеть в неизвестности:
  // она возвращается на место, а повтор предлагается явно — и уходит
  // с тем же operationId, поэтому «дважды» ничего не произойдёт.
  it('возвращает карточку на место и предлагает повтор тем же operationId', async () => {
    operation.mockRejectedValueOnce(new NetworkError('Нет связи'))

    const view = await loaded()
    await act(async () => {
      view.result.current.moveCard('первая', COL_B, { place: 'end' })
    })

    await waitFor(() => expect(view.result.current.notices.length).toBe(1))
    expect(view.result.current.order[COL_A]).toEqual(['первая', 'вторая'])
    expect(view.result.current.order[COL_B]).toEqual([])

    const notice = view.result.current.notices[0]
    expect(notice.retry).toBeTypeOf('function')
    const firstOperationId = operation.mock.calls[0][1]

    operation.mockResolvedValueOnce({
      version: 2,
      patch: { cards: [card('первая', COL_B, 'a2')] },
    })
    await act(async () => notice.retry?.())
    await waitFor(() => expect(operation).toHaveBeenCalledTimes(2))
    expect(operation.mock.calls[1][1]).toBe(firstOperationId)
  })
})

describe('поток изменений', () => {
  it('перечитывает доску, когда чужая версия новее нашей', async () => {
    await loaded()
    expect(snapshot).toHaveBeenCalledTimes(1)

    snapshot.mockResolvedValue(board(2))
    await act(async () => {
      FakeEventSource.last?.emit('board', { version: 2, actorId: 'кто-то' })
    })
    await waitFor(() => expect(snapshot).toHaveBeenCalledTimes(2))
  })

  // Новость о версии, которая у нас уже есть, — не повод дёргать сервер.
  it('не перечитывает доску на новость о версии не новее нашей', async () => {
    await loaded()
    await act(async () => {
      FakeEventSource.last?.emit('board', { version: 1, actorId: 'кто-то' })
    })
    expect(snapshot).toHaveBeenCalledTimes(1)
  })

  it('закрывает поток, когда доску закрывают', async () => {
    const view = await loaded()
    const source = FakeEventSource.last
    view.unmount()
    expect(source?.closed).toBe(true)
  })
})
