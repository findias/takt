// «Отбор» раскрывается кнопкой и обязан закрываться Escape.
//
// Панель свёрнута на любой ширине (разбор 21.09.2026), и раскрытая
// она сдвигает доску вниз на ряд. Закрыть её можно было только тем же
// нажатием на кнопку: Escape из флажка или списка не делал ничего,
// хотя всякое другое раскрытое на этом экране — меню, выбор метки,
// панель карточки — им закрывается. Найдено на подготовке показа.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { FilterBar } from './FilterBar.tsx'
import { EMPTY } from './filters.ts'

function show() {
  const onOuterKey = vi.fn()
  render(
    <div onKeyDown={(e) => e.key === 'Escape' && onOuterKey()}>
      <FilterBar
        filters={EMPTY}
        people={[]}
        labels={[]}
        iterations={[]}
        hidden={0}
        hasBlockDeadlines={false}
        onChange={() => {}}
      />
    </div>,
  )
  return { onOuterKey }
}

it('Escape из раскрытого отбора сворачивает его и возвращает фокус на кнопку', async () => {
  const user = userEvent.setup()
  const { onOuterKey } = show()
  const button = screen.getByRole('button', { name: 'Отбор' })
  await user.click(button)
  expect(button.getAttribute('aria-expanded')).toBe('true')

  await user.click(screen.getByRole('checkbox', { name: 'Горит' }))
  await user.keyboard('{Escape}')

  expect(button.getAttribute('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(button)
  // Escape истрачен здесь: иначе тот же нажим закрыл бы ещё и панель
  // карточки, открытую рядом.
  expect(onOuterKey).not.toHaveBeenCalled()
})

it('Escape на самой кнопке тоже сворачивает', async () => {
  const user = userEvent.setup()
  show()
  const button = screen.getByRole('button', { name: 'Отбор' })
  await user.click(button)
  await user.keyboard('{Escape}')
  expect(button.getAttribute('aria-expanded')).toBe('false')
})

it('свёрнутый отбор Escape не трогает — он принадлежит тем, кто вокруг', async () => {
  const user = userEvent.setup()
  const { onOuterKey } = show()
  screen.getByRole('button', { name: 'Отбор' }).focus()
  await user.keyboard('{Escape}')
  expect(onOuterKey).toHaveBeenCalledOnce()
})

it('поиск Escape оставлен себе: там он стирает набранное', async () => {
  const user = userEvent.setup()
  const { onOuterKey } = show()
  await user.click(screen.getByRole('button', { name: 'Отбор' }))
  await user.click(screen.getByRole('searchbox', { name: 'Найти карточку' }))
  await user.keyboard('{Escape}')
  expect(screen.getByRole('button', { name: 'Отбор' }).getAttribute('aria-expanded')).toBe('true')
  expect(onOuterKey).toHaveBeenCalledOnce()
})
