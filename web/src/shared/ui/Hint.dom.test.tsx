// «?» у понятия (ROADMAP 30.4): раскрывашка, а не подсказка при
// наведении. Проверяется то, что делает её доступной: имя у кнопки,
// текст в живом регионе, ссылка в справку, Escape с возвратом фокуса.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it } from 'vitest'
import { Hint } from './Hint.tsx'

it('«?» называет понятие, объявляет пояснение и ведёт в справку', async () => {
  render(<Hint topic="limit" />)
  const button = screen.getByRole('button', { name: 'Что такое «Лимит колонки»?' })
  expect(button.getAttribute('aria-expanded')).toBe('false')
  // Регион смонтирован заранее и пуст: диктор объявляет изменение
  // существующей области, а появившийся узел пропускает.
  const status = screen.getByRole('status')
  expect(status.textContent).toBe('')

  await userEvent.click(button)
  expect(button.getAttribute('aria-expanded')).toBe('true')
  expect(status.textContent).toMatch(/Сколько карточек колонке стоит держать/)
  const more = screen.getByRole('link', { name: 'Подробнее в справке' })
  expect(more.getAttribute('href')).toBe('/help/ru/howto#columns')
  expect(more.getAttribute('target')).toBe('_blank')

  // Из ссылки Escape закрывает пояснение и возвращает фокус на «?».
  more.focus()
  await userEvent.keyboard('{Escape}')
  expect(screen.queryByRole('link', { name: 'Подробнее в справке' })).toBeNull()
  expect(document.activeElement).toBe(button)
  expect(status.textContent).toBe('')
})
