// Колокольчик (ROADMAP 29.3): число непрочитанных — в имени кнопки
// словами, список ведёт прямо на карточку, «прочитать все» гасит счётчик.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { Bell } from './Bell.tsx'
import type { AppNotification } from '../../shared/api/index.ts'

const ITEM: AppNotification = {
  id: 'n-1',
  reason: 'mentioned',
  boardId: 'b-1',
  boardName: 'Поставки',
  cardId: 'c-1',
  cardNumber: 'ПОСТ-3',
  cardTitle: 'Разобрать обращения',
  actorName: 'Борис Дятлов',
  createdAt: new Date().toISOString(),
  read: false,
}

let posted: unknown[] = []

beforeEach(() => {
  posted = []
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/notifications/read') posted.push(JSON.parse(String(init?.body)))
      const body =
        String(input) === '/api/notifications' ? { items: [ITEM], unread: 1 } : {}
      return Promise.resolve(
        new Response(init?.method === 'POST' ? null : JSON.stringify(body), {
          status: init?.method === 'POST' ? 204 : 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }),
  )
})

afterEach(() => vi.unstubAllGlobals())

it('колокольчик называет число, ведёт на карточку и гасит счётчик', async () => {
  render(<Bell />)
  const bell = await screen.findByRole('button', { name: 'Уведомления: непрочитанных 1' })
  await userEvent.click(bell)

  const link = screen.getByRole('link', { name: /Борис Дятлов упоминает вас в обсуждении/ })
  expect(link.getAttribute('href')).toBe('/board/b-1/card/c-1')
  expect(link.textContent).toMatch(/ПОСТ-3/)
  expect(link.textContent).toMatch(/не прочитано/)

  await userEvent.click(screen.getByRole('button', { name: 'Прочитать все' }))
  expect(screen.getByRole('button', { name: 'Уведомления' })).toBeTruthy()
  await waitFor(() => expect(posted).toEqual([{ ids: [] }]))
})
