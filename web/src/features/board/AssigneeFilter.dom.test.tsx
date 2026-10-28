// Выбор исполнителя в отборе доски: порядок переключается, поиск
// находит, выбор ставит отбор и возвращает фокус на кнопку.

import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { AssigneeFilter } from './AssigneeFilter.tsx'
import type { AssigneeOption } from './assigneeFilter.ts'
import { UNASSIGNED } from './filters.ts'

const person = (userId: string, name: string, cards: number, weight = 0, noAccess = false): AssigneeOption => ({
  userId,
  name,
  cards,
  weight,
  unestimated: 0,
  noAccess,
})

const options = [
  person('anna', 'Анна Королёва', 1, 3),
  person('boris', 'Борис Дятлов', 3, 8),
  person('gleb', 'Глеб Тишин', 2, 0, true),
]

beforeEach(() => localStorage.clear())

function show(value: string | null = null) {
  const onChange = vi.fn()
  render(<AssigneeFilter value={value} options={options} unit="hours" onChange={onChange} />)
  return { onChange }
}

const names = () =>
  within(screen.getByRole('listbox'))
    .getAllByRole('option')
    .map((o) => o.querySelector('.palette-title')?.textContent)

it('по умолчанию по имени, переключается по загрузке и помнит выбор', async () => {
  const user = userEvent.setup()
  show()
  await user.click(screen.getByRole('button', { name: 'Исполнитель: Все исполнители' }))
  expect(names()).toEqual(['Все исполнители', 'Ни на ком', 'Анна Королёва', 'Борис Дятлов', 'Глеб Тишин'])

  await user.click(screen.getByRole('button', { name: 'По карточкам' }))
  expect(names()).toEqual(['Все исполнители', 'Ни на ком', 'Борис Дятлов', 'Глеб Тишин', 'Анна Королёва'])
  expect(localStorage.getItem('takt.assigneeSort')).toBe('cards')

  // Загрузка и отсутствие доступа — словами рядом с именем.
  const gleb = screen.getByRole('option', { name: /Глеб Тишин/ })
  expect(gleb.textContent).toContain('нет доступа к доске')
  expect(screen.getByRole('option', { name: /Борис Дятлов/ }).textContent).toContain('3 в работе, 8 ч')
})

it('набор находит человека, Enter ставит отбор и возвращает фокус на кнопку', async () => {
  const user = userEvent.setup()
  const { onChange } = show()
  const button = screen.getByRole('button', { name: /Исполнитель/ })
  await user.click(button)
  await user.keyboard('бор')
  expect(names()).toEqual(['Борис Дятлов'])
  await user.keyboard('{Enter}')
  expect(onChange).toHaveBeenCalledWith('boris')
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(document.activeElement).toBe(button)
})

it('«Ни на ком» выбирается стрелками; Escape сперва стирает набранное, потом закрывает', async () => {
  const user = userEvent.setup()
  const { onChange } = show('anna')
  const button = screen.getByRole('button', { name: 'Исполнитель: Анна Королёва' })
  await user.click(button)
  await user.keyboard('{ArrowDown}{Enter}')
  expect(onChange).toHaveBeenCalledWith(UNASSIGNED)

  await user.click(button)
  await user.keyboard('xyz')
  expect(screen.getByText('Никого не нашлось')).toBeTruthy()
  await user.keyboard('{Escape}')
  expect(screen.getByRole('listbox')).toBeTruthy()
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(document.activeElement).toBe(button)
})
