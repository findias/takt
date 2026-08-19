// Заведение доски: ключ и отказы по нему.
//
// Ключ — единственное свойство доски, которое после заведения уже
// не сменить: он стоит в номере каждой карточки. Поэтому проверяется
// не «форма отправилась», а то, что задано человеком, доехало без
// правок, и что отказ виден там, где стоит форма, — она под списком
// досок, и сообщение над списком человек не увидит.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import type { BoardInfo, Principal } from '../shared/api/index.ts'
import { ToastHost } from '../shared/ui/Toast.tsx'

const createBoard = vi.fn()
const deleteBoard = vi.fn()
const archivedBoards = vi.fn()

vi.mock('../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../shared/api')>()
  return {
    ...real,
    api: {
      ...real.api,
      createBoard,
      deleteBoard,
      archivedBoards,
      listBoards: vi.fn().mockResolvedValue({ boards: [] }),
      team: vi.fn().mockResolvedValue({ members: [] }),
      listTeams: vi.fn().mockResolvedValue({ teams: [] }),
    },
  }
})

const { BoardList } = await import('./BoardList')
const { ApiError } = await import('../shared/api')

const ANNA: Principal = {
  id: 'u-1',
  email: 'anna@example.test',
  name: 'Анна Королёва',
  orgId: 'org-1',
  orgName: 'Северный проект',
  orgSlug: 'sever',
  role: 'owner',
  estimateUnit: 'points',
}

function show() {
  return render(
    <ToastHost>
      <BoardList principal={ANNA} onOpen={() => {}} />
    </ToastHost>,
  )
}

const ARCHIVED: BoardInfo = {
  id: 'b-9',
  name: 'Найм',
  key: 'НАЙМ',
  version: 1,
  sleDays: null,
  sleProbability: 85,
  visibility: 'org',
  cards: 0,
}

it('ключ доезжает таким, каким его набрали, и заглавными', async () => {
  createBoard.mockResolvedValue({ id: 'b-1', name: 'Поставки', key: 'ПОСТ' })
  show()

  await userEvent.type(await screen.findByPlaceholderText('Название новой доски'), 'Поставки')
  await userEvent.type(screen.getByLabelText(/Ключ доски/), 'пост')
  await userEvent.click(screen.getByRole('button', { name: 'Завести доску' }))

  await waitFor(() => expect(createBoard).toHaveBeenCalledWith('Поставки', 'ПОСТ'))
})

it('незаданный ключ уходит пустым: выводит его сервер, а не мы', async () => {
  createBoard.mockResolvedValue({ id: 'b-2', name: 'Найм', key: 'НАЙМ' })
  show()

  await userEvent.type(await screen.findByPlaceholderText('Название новой доски'), 'Найм')
  await userEvent.click(screen.getByRole('button', { name: 'Завести доску' }))

  await waitFor(() => expect(createBoard).toHaveBeenCalledWith('Найм', ''))
})

it('занятый ключ и негодный ключ — разные отказы, и оба помечают поле', async () => {
  createBoard.mockRejectedValue(
    new ApiError(409, { error: 'такой ключ уже занят другой доской', code: 'board_key_taken' }),
  )
  show()

  await userEvent.type(await screen.findByPlaceholderText('Название новой доски'), 'Поставки')
  await userEvent.type(screen.getByLabelText(/Ключ доски/), 'ПОСТ')
  await userEvent.click(screen.getByRole('button', { name: 'Завести доску' }))

  expect(await screen.findByText('такой ключ уже занят другой доской')).toBeTruthy()
  expect(screen.getByLabelText(/Ключ доски/).getAttribute('aria-invalid')).toBe('true')
  // Набранное остаётся на месте: отказ — повод поправить, а не набирать
  // всё заново.
  expect((screen.getByLabelText(/Ключ доски/) as HTMLInputElement).value).toBe('ПОСТ')

  createBoard.mockRejectedValue(
    new ApiError(400, {
      error: 'ключ доски — от двух до шести букв или цифр, начиная с буквы',
      code: 'board_key_invalid',
    }),
  )
  await userEvent.click(screen.getByRole('button', { name: 'Завести доску' }))
  expect(await screen.findByText(/от двух до шести букв/)).toBeTruthy()
})

it('отказ не про ключ поле не пятнает', async () => {
  createBoard.mockRejectedValue(
    new ApiError(403, { error: 'у вас доступ только на чтение', code: 'forbidden' }),
  )
  show()

  await userEvent.type(await screen.findByPlaceholderText('Название новой доски'), 'Поставки')
  await userEvent.click(screen.getByRole('button', { name: 'Завести доску' }))

  expect(await screen.findByText('у вас доступ только на чтение')).toBeTruthy()
  expect(screen.getByLabelText(/Ключ доски/).getAttribute('aria-invalid')).toBeNull()
})

it('удаление насовсем называет удалённое: строка исчезает молча, а это «навсегда»', async () => {
  archivedBoards.mockResolvedValue({ boards: [ARCHIVED] })
  deleteBoard.mockResolvedValue(undefined)
  show()

  await userEvent.click(await screen.findByRole('button', { name: 'Показать архив' }))
  await userEvent.click(await screen.findByRole('button', { name: /Удалить навсегда: Найм/ }))
  await userEvent.type(screen.getByLabelText('Название доски для подтверждения'), 'Найм')

  archivedBoards.mockResolvedValue({ boards: [] })
  await userEvent.click(screen.getByRole('button', { name: 'Удалить навсегда' }))

  expect(await screen.findByText('Доска «Найм» удалена навсегда.')).toBeTruthy()
})
