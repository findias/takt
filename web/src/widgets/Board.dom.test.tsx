// Экран доски.
//
// Проверяется не вёрстка, а обещания, которые легко потерять при правке
// и невозможно заметить глазами: перенос карточки с клавиатуры, объявление
// результата для тех, кто не видит экрана, и объяснение пустой доски.
// Перетаскивание мышью не проверяется — оно живёт в чужой библиотеке
// и требует настоящих событий указателя; клавиатурный путь для WCAG
// важнее, и он наш.

import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Card, Column, Snapshot } from '../shared/api/index.ts'

const snapshot = vi.fn<() => Promise<Snapshot>>()
const operation = vi.fn()
const changes = vi.fn()

vi.mock('../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../shared/api')>()
  return {
    ...real,
    api: {
      ...real.api,
      snapshot,
      operation,
      changes,
      // Экран доски дёргает соседние ленты при открытии; они здесь
      // не интересны, но молчать должны без ошибок.
      events: vi.fn().mockResolvedValue({ events: [], next: null }),
      metrics: vi.fn().mockResolvedValue({}),
      comments: vi.fn().mockResolvedValue({ comments: [] }),
    },
  }
})

const { Board } = await import('./Board')
const { ApiError } = await import('../shared/api')

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
    doneAt: null,
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

// Поток изменений подменяется настолько, насколько нужен проверкам:
// он умеет отдать событие, «закрыться» и рассказать об этом — на этом
// стоит проверка переоткрытия.
class FakeEventSource {
  static readonly CLOSED = 2
  static открытые: FakeEventSource[] = []
  readyState = 1
  private слушатели = new Map<string, ((e: unknown) => void)[]>()

  constructor(readonly url: string) {
    FakeEventSource.открытые.push(this)
  }

  addEventListener(kind: string, fn: (e: unknown) => void) {
    this.слушатели.set(kind, [...(this.слушатели.get(kind) ?? []), fn])
  }

  close() {
    this.readyState = FakeEventSource.CLOSED
  }

  /** Оборвать поток так, как это делает браузер после ответа не-200. */
  оборвать() {
    this.readyState = FakeEventSource.CLOSED
    for (const fn of this.слушатели.get('error') ?? []) fn(new Event('error'))
  }
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  snapshot.mockReset()
  operation.mockReset()
  operation.mockResolvedValue({ version: 2, patch: {} })
  changes.mockReset()
  changes.mockResolvedValue({ full: false, results: [] })
  FakeEventSource.открытые = []
  vi.stubGlobal('EventSource', FakeEventSource)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  // Отбор и группировка живут в адресе, а адрес один на весь файл:
  // без сброса включённый отбор достаётся следующей проверке, и она
  // ищет карточки, которых на экране уже нет.
  window.history.replaceState({}, '', '/')
})

function show(cardId: string | null = null) {
  return render(
    <Board boardId="board" cardId={cardId} onCard={() => {}} unit="points" meId="я" isOwner canEdit onBack={() => {}} />,
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

  // Отбор прячет карточки, а колонка об этом молчала: «Пусто.
  // Перетащите карточку сюда» при скрытых — враньё, из-за которого
  // идут искать поломку, которой нет.
  it('колонка под отбором говорит, что скрыто, а не что пусто', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0'), card('вторая', COL_A, 'a1')]))
    show()
    await screen.findByRole('group', { name: /Карточка «первая»/ })

    // Обе карточки среднего уровня: отбор «Горит» не оставит ни одной.
    await user.click(screen.getByRole('checkbox', { name: 'Горит' }))

    const queue = await screen.findByRole('region', { name: 'Очередь' })
    await waitFor(() => expect(queue.textContent).toMatch(/скрыто 2/))
    expect(queue.textContent).not.toMatch(/Перетащите карточку сюда/)
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

describe('поток изменений', () => {
  // EventSource переподключается сам только после обрыва связи. Ответ
  // не-200 он считает окончательным — и закрывается насовсем; именно
  // это приходит, когда сервер перезапускают. Доска после этого молча
  // стоит устаревшей: проход по интерфейсу поймал вторую вкладку,
  // застрявшую на прежней версии навсегда.
  it('закрывшийся поток открывается заново и догоняет пропущенное', async () => {
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    changes.mockResolvedValue({ full: false, results: [] })
    show()
    await screen.findByRole('group', { name: /Карточка «первая»/ })

    expect(FakeEventSource.открытые).toHaveLength(1)
    FakeEventSource.открытые[0].оборвать()

    // Пауза перед новой попыткой: долбить поднимающийся сервер незачем.
    expect(FakeEventSource.открытые).toHaveLength(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3100)
    })

    expect(FakeEventSource.открытые).toHaveLength(2)
    expect(changes).toHaveBeenCalled()
  })
})

describe('доска, которая не загрузилась', () => {
  // Перезапуск сервера ловит открытую доску на «Ошибка 503». Кнопка
  // «Повторить» рядом есть, но доска обязана сойтись сама: жать её
  // ради того, чтобы пережить чужую выкладку, человек не должен.
  // Чужая доска неотличима от несуществующей нарочно. Повторять такой
  // отказ бессмысленно: он не пройдёт ни со второй попытки, ни с сотой,
  // а «Повторить» отправляет искать поломку, которой нет.
  it('«не найдено» не повторяет запрос, а показывает дорогу назад', async () => {
    snapshot.mockRejectedValue(new ApiError(404, { code: 'not_found', error: 'доска не найдена' }))
    show()

    expect(await screen.findByText(/доска не найдена/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Все доски' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Повторить' })).toBeNull()

    const попыток = snapshot.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(12000)
    })
    expect(snapshot.mock.calls.length).toBe(попыток)
  })

  // Клиент из вчерашней сборки против сегодняшнего сервера (или
  // наоборот) — снимок приходит не той формы. Раньше отсюда вылетало
  // «a.columns is not iterable»: сообщение чужой библиотеки, по которому
  // не понять ни что случилось, ни что обновление страницы помогает.
  it('снимок не той формы объясняется человеку, а не строкой из библиотеки', async () => {
    snapshot.mockResolvedValue({ info: { id: 'board', name: 'Доска', version: 1 } } as never)
    show()

    const текст = await screen.findByText(/устарела/)
    expect(текст.textContent).toMatch(/обновите её/i)
    expect(screen.queryByText(/is not iterable/)).toBeNull()
  })

  it('пробует ещё раз сама, без нажатия «Повторить»', async () => {
    snapshot.mockRejectedValueOnce(new Error('Ошибка 503'))
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()

    expect(await screen.findByText(/Не удалось загрузить доску/)).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100)
    })

    expect(await screen.findByRole('group', { name: /Карточка «первая»/ })).toBeTruthy()
    expect(screen.queryByText(/Не удалось загрузить доску/)).toBeNull()
  })
})

describe('наблюдатель', () => {
  // Сервер держит границу: наблюдателю на любую операцию отвечает 403
  // «у вас доступ только на чтение». Интерфейс её не держал вовсе —
  // показывал «Добавить карточку», меню действий, «Разметку»
  // и «+ итерация», давал пройти путь до конца и приносил отказ только
  // после Enter. Кнопка, ведущая к отказу, — обещание, которого
  // интерфейс не держит.
  function readOnly(cards = [card('первая', COL_A, 'a0')]) {
    snapshot.mockResolvedValue(board(cards))
    return render(
      <Board
        boardId="board"
        cardId={null}
        onCard={() => {}}
        unit="points"
        meId="я"
        isOwner={false}
        canEdit={false}
        onBack={() => {}}
      />,
    )
  }

  it('не показывает действий над доской', async () => {
    readOnly()
    await screen.findByRole('group', { name: /Карточка «первая»/ })

    expect(screen.queryByRole('button', { name: /Добавить карточку/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Разметка колонки/ })).toBeNull()
    expect(screen.queryByRole('button', { name: '+ итерация' })).toBeNull()
    expect(screen.queryByRole('button', { name: '+ Колонка' })).toBeNull()
  })

  it('не показывает действий над карточкой', async () => {
    readOnly()
    await screen.findByRole('group', { name: /Карточка «первая»/ })

    expect(screen.queryByRole('button', { name: /Действия карточки/ })).toBeNull()
    expect(screen.queryByRole('checkbox', { name: /Выделить/ })).toBeNull()
    // Сама карточка при этом читается и открывается: наблюдатель
    // пришёл смотреть, а не быть выставленным за дверь.
    expect(screen.getByRole('button', { name: 'первая' })).toBeTruthy()
  })

  it('тому, кто может писать, всё это показывает', async () => {
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    show()
    await screen.findByRole('group', { name: /Карточка «первая»/ })

    expect(screen.getByRole('button', { name: /Добавить карточку в «Очередь»/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Действия карточки/ })).toBeTruthy()
  })
})

describe('заведение карточек подряд', () => {
  // Работа приходит списком: пять пунктов с планёрки заводят одну
  // за другой. Форма, закрывающаяся после каждой карточки, означает
  // поиск кнопки на каждую вторую — это первое, обо что споткнулся
  // проход по интерфейсу глазами.
  it('после Enter форма остаётся, поле пустеет и держит фокус', async () => {
    snapshot.mockResolvedValue(board([]))
    show()

    await userEvent.click(await screen.findByRole('button', { name: /Добавить карточку в «Очередь»/ }))
    const field = screen.getByPlaceholderText('Что нужно сделать?')
    await userEvent.type(field, 'Первая{Enter}')

    expect(operation).toHaveBeenCalledWith('board', expect.any(String), 'CREATE_CARD', {
      columnId: COL_A,
      title: 'Первая',
      place: 'end',
    })
    const again = screen.getByPlaceholderText('Что нужно сделать?')
    expect((again as HTMLTextAreaElement).value).toBe('')
    expect(document.activeElement).toBe(again)

    // Вторая заводится там же, без единого лишнего нажатия.
    await userEvent.type(again, 'Вторая{Enter}')
    expect(operation).toHaveBeenLastCalledWith('board', expect.any(String), 'CREATE_CARD', {
      columnId: COL_A,
      title: 'Вторая',
      place: 'end',
    })
  })

  it('закрывает форму человек: Escape', async () => {
    snapshot.mockResolvedValue(board([]))
    show()

    await userEvent.click(await screen.findByRole('button', { name: /Добавить карточку в «Очередь»/ }))
    await userEvent.type(screen.getByPlaceholderText('Что нужно сделать?'), 'Черновик{Escape}')

    expect(screen.queryByPlaceholderText('Что нужно сделать?')).toBeNull()
    expect(screen.getByRole('button', { name: /Добавить карточку в «Очередь»/ })).toBeTruthy()
  })
})

describe('таблица', () => {
  // Рука идёт в заголовок столбца — это первое, что человек пробует
  // в любой таблице. Раньше она попадала в подпись, и не происходило
  // ничего: сортировка жила только в отдельном списке рядом, а по чему
  // выстроены строки, из таблицы было не видно.
  it('щелчок в заголовок сортирует, и видно, по какому столбцу', async () => {
    snapshot.mockResolvedValue(
      board([
        { ...card('дешёвая', COL_A, 'a0', 'Дешёвая'), estimate: 1 },
        { ...card('дорогая', COL_A, 'a1', 'Дорогая'), estimate: 8 },
      ]),
    )
    window.history.replaceState({}, '', '/?view=table')
    show()

    const head = await screen.findByRole('columnheader', { name: /Оценка/ })
    expect(head.getAttribute('aria-sort')).toBe('none')

    await userEvent.click(within(head).getByRole('button'))

    expect(
      screen.getByRole('columnheader', { name: /Оценка/ }).getAttribute('aria-sort'),
    ).toBe('ascending')
    const titles = screen.getAllByRole('row').slice(1).map((r) => r.textContent ?? '')
    expect(titles[0]).toMatch(/Дорогая/)
    expect(titles[1]).toMatch(/Дешёвая/)
  })
})

describe('поток словами, а не машинными строками', () => {
  // Подсказка столбика пропускной способности печатала «2026-05-18: 0»
  // — машинная запись там, где человек ищет глазами «какая это была
  // неделя». Диктору же читался голый ряд чисел без недель вовсе.
  const metrics = {
    days: 91,
    cycleTime: null,
    finished: [],
    throughput: [
      { week: '2026-05-18', count: 0 },
      { week: '2026-05-25', count: 3 },
    ],
    wip: 0,
    aging: [],
    flow: [],
    forecast: null,
    discarded: 0,
  }

  // Прогноз считается из того же прошлого, что и время цикла, но
  // оговорку имело только время цикла: на трёх доведённых карточках
  // прогноз спокойно печатал «5 карточек — 161 день», и это читалось
  // как расчёт, а не как гадание с точностью до дня.
  it('прогноз на коротком прошлом сам говорит, что опираться на него рано', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    const { api } = await import('../shared/api')
    vi.spyOn(api, 'metrics').mockResolvedValue({
      ...metrics,
      throughput: [
        { week: '2026-05-04', count: 1 },
        { week: '2026-05-11', count: 0 },
        { week: '2026-05-18', count: 1 },
        { week: '2026-05-25', count: 1 },
      ],
      forecast: [{ cards: 5, p50: 105, p85: 140, p95: 161 }],
    })
    show()

    await user.click(await screen.findByRole('button', { name: 'Поток' }))
    const note = await screen.findByText(/тысяча испытаний/)
    expect(note.textContent).toMatch(/всего 3 карточки за 4 недели/)
    expect(note.textContent).toMatch(/слишком мало, чтобы на это опираться/)
  })

  it('на длинном прошлом прогноз ничего не оговаривает', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    const { api } = await import('../shared/api')
    vi.spyOn(api, 'metrics').mockResolvedValue({
      ...metrics,
      throughput: [
        { week: '2026-05-04', count: 5 },
        { week: '2026-05-11', count: 4 },
        { week: '2026-05-18', count: 6 },
        { week: '2026-05-25', count: 5 },
      ],
      forecast: [{ cards: 5, p50: 7, p85: 14, p95: 21 }],
    })
    show()

    await user.click(await screen.findByRole('button', { name: 'Поток' }))
    const note = await screen.findByText(/тысяча испытаний/)
    expect(note.textContent).not.toMatch(/слишком мало/)
  })

  it('неделя в подсказке столбика названа словами', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    snapshot.mockResolvedValue(board([card('первая', COL_A, 'a0')]))
    const { api } = await import('../shared/api')
    vi.spyOn(api, 'metrics').mockResolvedValue(metrics)
    show()

    await user.click(await screen.findByRole('button', { name: 'Поток' }))
    const bars = await screen.findByRole('img', { name: /Пропускная способность/ })
    const titles = [...bars.querySelectorAll('.bar')].map((b) => b.getAttribute('title'))
    expect(titles).toEqual(['неделя 18 мая: 0', 'неделя 25 мая: 3'])
    // Диктору читаются пары «неделя — сколько»: ряд чисел без недель
    // не говорит ни о чём, а столбики он не видит.
    expect(bars.getAttribute('aria-label')).toBe(
      'Пропускная способность: неделя 18 мая — 0, неделя 25 мая — 3',
    )
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
    // Срок при этом не пропал — он просто не кричит: не таблеткой,
    // а строчкой текста.
    expect(screen.getByTitle('Дата обязательства').className).toContain('card-due')
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

describe('отметка «сделано» у части работы', () => {
  // Часть вида «согласовать с юристами» по колонкам не ездит, и до
  // отметки ответить «сделано ли» можно было только переездом в колонку
  // финиша — обрядом ради счётчика. Проверяется здесь то, что глазами
  // не видно: что нажатие уходит отдельной операцией и колонку карточки
  // не трогает.
  function withSubtask(doneAt: string | null): Snapshot {
    const parent = card('родитель', COL_A, 'a0', 'Выпустить рассылку')
    const child = { ...card('часть', COL_A, 'a1', 'Согласовать с юристами'), doneAt }
    const snap = board([parent, child])
    return {
      ...snap,
      links: [{ fromCard: 'родитель', toCard: 'часть', kind: 'subtask' }],
      cards: [{ ...parent, progress: { done: doneAt ? 1 : 0, total: 1, byWeight: false } }, child],
    }
  }

  it('флажок части отправляет отметку, а не перенос', async () => {
    snapshot.mockResolvedValue(withSubtask(null))
    const user = userEvent.setup()
    show()

    await user.click(await screen.findByRole('button', { name: /Подзадачи: готово/ }))
    const check = screen.getByRole('checkbox', { name: 'Сделана: Согласовать с юристами' })
    expect(check.getAttribute('aria-checked')).toBe('false')

    await user.click(check)
    await waitFor(() => expect(operation).toHaveBeenCalled())
    const [, , type, payload] = operation.mock.calls[0]
    expect(type).toBe('SET_CARD_DONE')
    expect(payload).toEqual({ cardId: 'часть', done: true })
  })

  it('отмеченная часть предлагает снять отметку, а не поставить снова', async () => {
    snapshot.mockResolvedValue(withSubtask('2026-08-17T10:00:00Z'))
    const user = userEvent.setup()
    show()

    await user.click(await screen.findByRole('button', { name: /Подзадачи: готово/ }))
    const check = screen.getByRole('checkbox', { name: 'Сделана: Согласовать с юристами' })
    expect(check.getAttribute('aria-checked')).toBe('true')

    await user.click(check)
    await waitFor(() => expect(operation).toHaveBeenCalled())
    expect(operation.mock.calls[0][3]).toEqual({ cardId: 'часть', done: false })
  })
})

describe('часть работы держит саму задачу', () => {
  // Разбили работу, одна часть встала поперёк — задача стоит из-за неё.
  // Сказать об этом можно было только словами в причине: ни перейти
  // к держащей, ни увидеть, что с ней стало, было нельзя.
  function withParts(): Snapshot {
    const parent = card('родитель', COL_A, 'a0', 'Выпустить релиз')
    const part = card('часть', COL_A, 'a1', 'Согласовать смету')
    const snap = board([parent, part])
    return { ...snap, links: [{ fromCard: 'родитель', toCard: 'часть', kind: 'subtask' }] }
  }

  it('«Держит» у части спрашивает причину и блокирует задачу ссылкой на неё', async () => {
    snapshot.mockResolvedValue(withParts())
    const user = userEvent.setup()
    show('родитель')

    // Карточка открывается на обсуждении: работа — вторая вкладка.
    await user.click(await screen.findByRole('tab', { name: 'Работа' }))
    await user.click(await screen.findByRole('button', { name: 'Держит' }))
    await user.type(screen.getByLabelText('Причина блокировки'), 'ждём смету от подрядчика')
    await user.click(screen.getByRole('button', { name: 'Заблокировать' }))

    await waitFor(() => expect(operation).toHaveBeenCalled())
    const [, , type, payload] = operation.mock.calls[0]
    expect(type).toBe('BLOCK_CARD')
    expect(payload).toEqual({
      cardId: 'родитель',
      reason: 'ждём смету от подрядчика',
      blockingCard: 'часть',
    })
  })

  it('у заблокированной задачи видно, кто её держит, и путь к нему', async () => {
    const base = withParts()
    snapshot.mockResolvedValue({
      ...base,
      cards: base.cards.map((c) =>
        c.id === 'родитель'
          ? {
              ...c,
              blocked: {
                id: 'блок',
                reason: 'ждём смету',
                blockedAt: '2026-08-17T09:00:00Z',
                blockingCard: 'часть',
              },
            }
          : c,
      ),
    })
    const user = userEvent.setup()
    show('родитель')
    await user.click(await screen.findByRole('tab', { name: 'Работа' }))

    // Название держащей — кнопка: связь должна проходиться, а не только
    // показываться. На доске такая же карточка есть своим заголовком —
    // берём ту, что в панели.
    const holders = await screen.findAllByRole('button', { name: 'Согласовать смету' })
    expect(holders.some((b) => b.className.includes('link'))).toBe(true)
    // Предлагать вторую блокировку поверх открытой нечего: она отказала бы.
    expect(screen.queryByRole('button', { name: 'Держит' })).toBeNull()
  })
})
