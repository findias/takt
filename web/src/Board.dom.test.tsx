// Экран доски.
//
// Проверяется не вёрстка, а обещания, которые легко потерять при правке
// и невозможно заметить глазами: перенос карточки с клавиатуры, объявление
// результата для тех, кто не видит экрана, и объяснение пустой доски.
// Перетаскивание мышью не проверяется — оно живёт в чужой библиотеке
// и требует настоящих событий указателя; клавиатурный путь для WCAG
// важнее, и он наш.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Card, Column, Snapshot } from './api'

const snapshot = vi.fn<() => Promise<Snapshot>>()
const operation = vi.fn()

vi.mock('./api', async (importOriginal) => {
  const real = await importOriginal<typeof import('./api')>()
  return {
    ...real,
    api: {
      ...real.api,
      snapshot,
      operation,
      // Экран доски дёргает соседние ленты при открытии; они здесь
      // не интересны, но молчать должны без ошибок.
      events: vi.fn().mockResolvedValue({ events: [], next: null }),
      metrics: vi.fn().mockResolvedValue({}),
      comments: vi.fn().mockResolvedValue({ comments: [] }),
    },
  }
})

const { Board } = await import('./Board')

const COL_A = 'col-a'
const COL_B = 'col-b'

function column(id: string, name: string, position: string, extra: Partial<Column> = {}): Column {
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
    ...extra,
  }
}

function card(id: string, columnId: string, position: string, title = id): Card {
  return {
    id,
    columnId,
    position,
    title,
    description: '',
    version: 1,
    columnEnteredAt: '2026-08-15T12:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
  }
}

function board(cards: Card[], columns = [column(COL_A, 'Очередь', 'a0'), column(COL_B, 'В работе', 'a1')]): Snapshot {
  return {
    board: { id: 'board', name: 'Доска', version: 1 },
    columns,
    cards,
    links: [],
    linked: [],
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
  }
}

class FakeEventSource {
  addEventListener() {}
  close() {}
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  snapshot.mockReset()
  operation.mockReset()
  operation.mockResolvedValue({ version: 2, patch: {} })
  vi.stubGlobal('EventSource', FakeEventSource)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function show() {
  return render(<Board boardId="board" unit="points" meId="я" onBack={() => {}} />)
}

describe('перенос карточки с клавиатуры', () => {
  // Требование WCAG 2.5.7: всё, что делается перетаскиванием, должно
  // делаться и без него. Это единственный путь, который у нас свой, —
  // мышиный живёт в чужой библиотеке.
  it('Ctrl со стрелкой отправляет карточку в соседнюю колонку', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()

    const cardNode = await screen.findByRole('group', { name: /Карточка «первая»/ })
    cardNode.focus()
    await user.keyboard('{Control>}{ArrowRight}{/Control}')

    await waitFor(() => expect(operation).toHaveBeenCalledTimes(1))
    const [, , type, payload] = operation.mock.calls[0]
    expect(type).toBe('MOVE_CARD')
    expect(payload).toMatchObject({ cardId: 'первая', toColumnId: COL_B })
  })

  // Перенос вверх у самой верхней карточки — не ошибка и не повод дёргать
  // сервер: двигаться некуда.
  it('не отправляет ничего, когда двигаться некуда', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()

    const cardNode = await screen.findByRole('group', { name: /Карточка «первая»/ })
    cardNode.focus()
    await user.keyboard('{Control>}{ArrowLeft}{/Control}')
    await user.keyboard('{Control>}{ArrowUp}{/Control}')

    expect(operation).not.toHaveBeenCalled()
  })

  // Тот, кто не видит экрана, узнаёт о переносе только отсюда. Объявление
  // ставится с задержкой около секунды: смена фокуса иначе перебивает его,
  // и скринридер читает пустоту.
  it('объявляет результат переноса в живой области', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()

    const cardNode = await screen.findByRole('group', { name: /Карточка «первая»/ })
    cardNode.focus()
    await user.keyboard('{Control>}{ArrowRight}{/Control}')

    const live = document.querySelector('[aria-live="polite"]')
    expect(live).not.toBeNull()
    expect(live?.textContent).toBe('')

    await vi.advanceTimersByTimeAsync(1100)
    await waitFor(() => expect(live?.textContent).toMatch(/В работе/))
  })
})

describe('состояния доски', () => {
  // Пустая доска должна объяснять себя. «Ничего не видно» без объяснения
  // читается как поломка — так и было, пока не написали этот текст.
  it('пустая доска объясняет себя и подсказывает, что делать', async () => {
    snapshot.mockResolvedValue(board([]))
    show()

    // Ищем по тексту, а не по роли: заметок на экране бывает несколько,
    // и роль сама по себе не говорит, какая именно нужна.
    const note = (await screen.findByText(/ещё нет карточек/)).closest('[role="note"]')
    expect(note?.textContent).toMatch(/Ctrl со стрелками/)
  })

  it('доска с карточками ничего не объясняет', async () => {
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()

    await screen.findByRole('group', { name: /Карточка «первая»/ })
    expect(screen.queryByText(/ещё нет карточек/)).toBeNull()
  })

  // Мягкий лимит показывает счётчик и предупреждает, но работать
  // не мешает. Проверяется именно счётчик: он и есть весь смысл лимита
  // на экране.
  it('счётчик колонки показывает лимит', async () => {
    snapshot.mockResolvedValue(
      board(
        [card('первая', COL_A, 'a0'), card('вторая', COL_A, 'a1')],
        [column(COL_A, 'Очередь', 'a0', { wipLimit: 3 }), column(COL_B, 'В работе', 'a1')],
      ),
    )
    show()

    const queue = await screen.findByRole('region', { name: 'Очередь' })
    expect(queue.textContent).toMatch(/2\s*\/\s*3/)
  })
})
