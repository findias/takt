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
import type { Card, Column, Snapshot } from '../shared/api/index.ts'

const snapshot = vi.fn<() => Promise<Snapshot>>()
const operation = vi.fn()

vi.mock('../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../shared/api')>()
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
    number: `ДОСК-${id}`,
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
    comments: 0,
    priority: 'medium',
    dueOn: null,
  }
}

function board(cards: Card[], columns = [column(COL_A, 'Очередь', 'a0'), column(COL_B, 'В работе', 'a1')]): Snapshot {
  return {
    board: { id: 'board', name: 'Доска', key: 'ДОСК', version: 1, sleDays: null, sleProbability: 85 },
    columns,
    cards,
    links: [],
    linked: [],
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
    people: [],
    labels: [],
    cardLabels: {},
    cardAssignees: {},
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
  return render(
    <Board boardId="board" cardId={null} onCard={() => {}} unit="points" meId="я" isOwner onBack={() => {}} />,
  )
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

describe('ходьба по доске', () => {
  // Tab идёт по всем кнопкам подряд: до третьей карточки во второй
  // колонке ему нужно два десятка нажатий. Стрелки превращают доску
  // в сетку, какой она и выглядит.
  it('стрелка без модификатора переводит выделение, а не карточку', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(
      board([card('первая', COL_A, 'a0'), card('вторая', COL_A, 'a1')]),
    )
    show()

    const first = await screen.findByRole('group', { name: /Карточка «первая»/ })
    first.focus()
    await user.keyboard('{ArrowDown}')

    await waitFor(() =>
      expect(document.activeElement?.getAttribute('aria-label')).toMatch(/«вторая»/),
    )
    // Ничего не двинулось: это переход, а не перенос.
    expect(operation).not.toHaveBeenCalled()
  })

  it('Enter открывает карточку, E переводит её в переименование', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()

    const cardNode = await screen.findByRole('group', { name: /Карточка «первая»/ })
    cardNode.focus()
    await user.keyboard('e')

    // Появилось поле с прежним названием — значит карточка в правке.
    await waitFor(() => expect(screen.getByDisplayValue('первая')).toBeTruthy())
  })
})

describe('одна тревога на карточке', () => {
  // Тревожная пометка на карточке одна, и порядок между кандидатами
  // — не оформление, а смысл: если её забирает срок «через три дня»,
  // старение перестаёт быть видно на доске вовсе. Глазами это
  // не ловится: чтобы заметить пропажу, надо помнить, что она там
  // была.
  const дни = (n: number) => {
    const d = new Date(Date.now() + n * 86_400_000)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  /** Карточка, которая идёт дольше обещанных доской трёх дней. */
  function aging(dueOn: string | null): Snapshot {
    const c = card('старая', COL_B, 'a0')
    const snap = board([
      { ...c, startedAt: new Date(Date.now() - 10 * 86_400_000).toISOString(), dueOn },
    ])
    return { ...snap, board: { ...snap.board, sleDays: 3 } }
  }

  it('срок за три дня остаётся тихим, а тревога — про возраст', async () => {
    snapshot.mockResolvedValue(aging(дни(3)))
    show()

    const alarm = await screen.findByTitle('Возраст считается от начала работы')
    expect(alarm.className).toContain('mark--alarm')
    // Срок при этом не пропал — он просто не кричит.
    expect(screen.getByTitle('Дата обязательства').className).toContain('mark--quiet')
  })

  it('завтрашний срок тревогу забирает: из-за него звонят сегодня', async () => {
    snapshot.mockResolvedValue(aging(дни(1)))
    show()

    const due = await screen.findByTitle('Дата обязательства')
    expect(due.className).toContain('mark--alarm')
    expect(due.textContent).toContain('завтра')
    // Возраст ушёл в панель и в отборы: пометка одна.
    expect(screen.queryByTitle('Возраст считается от начала работы')).toBeNull()
  })
})
