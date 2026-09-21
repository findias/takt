// Выбор метки на карточке.
//
// Проверяется то, что обещано в ROADMAP (28.4): набранное новое
// название заводит метку и вешает её, одноимённое в другом регистре
// предлагает существующую, убранное — вернуть из архива, наблюдателю
// пункта «Завести» нет, и всё это проходится с клавиатуры.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { BoardLabel, LabelPlace } from '../../shared/api/index.ts'

const labelPlaces = vi.fn()
const createLabel = vi.fn()
const restoreLabel = vi.fn()

vi.mock('../../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../../shared/api')>()
  return { ...real, api: { ...real.api, labelPlaces, createLabel, restoreLabel } }
})

const { LabelCombobox } = await import('./LabelPicker.tsx')

const PLACES: LabelPlace[] = [
  { scope: 'org', name: '' },
  { scope: 'board', id: 'b', name: 'Поставки' },
]

function label(id: string, name: string, extra: Partial<BoardLabel> = {}): BoardLabel {
  return {
    id,
    name,
    tone: 'green',
    scope: 'org',
    archived: false,
    offered: true,
    applies: true,
    ...extra,
  }
}

let board = 0

function show(labels: BoardLabel[], hung: string[] = [], canEdit = true) {
  const onToggle = vi.fn()
  // Места кэшируются по доске — у каждой проверки своя.
  board++
  render(
    <LabelCombobox
      boardId={`board-${board}`}
      labels={labels}
      hung={hung}
      canEdit={canEdit}
      onToggle={onToggle}
    />,
  )
  return { onToggle, input: screen.getByRole('combobox', { name: 'Найти или завести метку' }) }
}

const options = () => screen.queryAllByRole('option').map((o) => o.textContent)

beforeEach(() => {
  labelPlaces.mockReset().mockResolvedValue({ places: PLACES })
  createLabel.mockReset()
  restoreLabel.mockReset()
})

describe('выбор метки', () => {
  it('новое название заводит метку на этой доске и вешает её — с клавиатуры', async () => {
    createLabel.mockResolvedValue(label('new', 'Ждём юристов'))
    const { onToggle, input } = show([label('u', 'Срочно')])
    await userEvent.type(input, 'Ждём юристов')
    await waitFor(() => expect(options()).toEqual([
      'Завести «Ждём юристов»на этой доске',
      'Завести «Ждём юристов»на всю организацию',
    ]))
    // Диктор слышит место пункта в списке, не уходя из поля.
    const first = screen.getAllByRole('option')[0]
    expect(input.getAttribute('aria-activedescendant')).toBe(first.id)
    expect(first.getAttribute('aria-setsize')).toBe('2')

    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(onToggle).toHaveBeenCalledWith('new', true))
    // Оттенок не спрашивается — его выберет сервер.
    expect(createLabel).toHaveBeenCalledWith('Ждём юристов', undefined, PLACES[1])
    expect((input as HTMLInputElement).value).toBe('')
  })

  it('стрелка вниз выбирает место шире', async () => {
    createLabel.mockResolvedValue(label('new', 'Общая'))
    const { input } = show([])
    await userEvent.type(input, 'Общая')
    await waitFor(() => expect(options()).toHaveLength(2))
    await userEvent.keyboard('{ArrowDown}{Enter}')
    await waitFor(() => expect(createLabel).toHaveBeenCalledWith('Общая', undefined, PLACES[0]))
  })

  it('одноимённое в другом регистре предлагает существующую, заведения нет', async () => {
    const { onToggle, input } = show([label('u', 'Срочно')])
    await userEvent.type(input, 'срочно')
    await waitFor(() => expect(labelPlaces).toHaveBeenCalled())
    expect(options()).toEqual(['Срочновся организация'])
    await userEvent.keyboard('{Enter}')
    expect(onToggle).toHaveBeenCalledWith('u', true)
    expect(createLabel).not.toHaveBeenCalled()
  })

  it('название убранной предлагает вернуть её из архива', async () => {
    restoreLabel.mockResolvedValue(undefined)
    const old = label('old', 'Старый формат', { archived: true, offered: false })
    const { onToggle, input } = show([old])
    await userEvent.type(input, 'старый формат')
    await waitFor(() => expect(options()).toEqual(['Вернуть из архива «Старый формат»вся организация']))
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(onToggle).toHaveBeenCalledWith('old', true))
    expect(restoreLabel).toHaveBeenCalledWith('old')
    expect(createLabel).not.toHaveBeenCalled()
  })

  it('отказ сервера живёт у поля и называет причину', async () => {
    createLabel.mockRejectedValue(new Error('метка «Срочно» уже есть у доски «Склад»'))
    const { onToggle, input } = show([])
    await userEvent.type(input, 'Срочно')
    await waitFor(() => expect(options()).toHaveLength(2))
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(input.getAttribute('aria-invalid')).toBe('true'))
    const described = document.getElementById(input.getAttribute('aria-describedby') ?? '')
    expect(described?.textContent).toContain('уже есть у доски «Склад»')
    expect(onToggle).not.toHaveBeenCalled()
    // Набранное не стирается: исправить опечатку дешевле, чем набрать заново.
    expect((input as HTMLInputElement).value).toBe('Срочно')
  })

  it('наблюдателю пункта «Завести» нет, и мест он не спрашивает', async () => {
    const { input } = show([label('u', 'Срочно')], [], false)
    await userEvent.type(input, 'Новая')
    expect(options()).toEqual([])
    expect(labelPlaces).not.toHaveBeenCalled()
    expect(screen.getByText('Такой метки нет.')).toBeTruthy()
  })

  it('встроенный выбор раскрывается по просьбе, и уход из поля его не сворачивает', async () => {
    board++
    render(
      <>
        <LabelCombobox
          boardId={`board-${board}`}
          labels={[label('u', 'Срочно')]}
          hung={[]}
          canEdit
          quietWhenIdle
          onToggle={vi.fn()}
        />
        <button>Снять</button>
      </>,
    )
    const input = screen.getByRole('combobox')
    await userEvent.click(input)
    expect(options()).toEqual([])
    await userEvent.keyboard('{ArrowDown}')
    expect(options()).toEqual(['Срочновся организация'])
    // Щелчок рядом не сворачивает список: свернувшись, он сдвинул бы
    // раскладку, и нажатие ушло бы мимо.
    await userEvent.click(screen.getByRole('button', { name: 'Снять' }))
    expect(options()).toHaveLength(1)
    input.focus()
    await userEvent.keyboard('{Escape}')
    expect(options()).toEqual([])
  })

  it('висящая отмечена, повторный выбор снимает', async () => {
    const { onToggle, input } = show([label('u', 'Срочно')], ['u'])
    expect(screen.getByRole('option').getAttribute('aria-checked')).toBe('true')
    input.focus()
    await userEvent.keyboard('{Enter}')
    expect(onToggle).toHaveBeenCalledWith('u', false)
  })
})
