// Экран выгрузки: люди под отбор (ROADMAP 37.1–37.2).
//
// Отбор по исполнителю был и раньше, но экран показывал одно число,
// а список людей не зависел от выбранного подразделения. Проверяется:
// список исполнителей берётся из ответа под отбор, разрез по людям
// виден на экране, и нажатие на имя ставит отбор на этого человека.

import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { Reports } from './Reports.tsx'

const asked: string[] = []

function reply(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

beforeEach(() => {
  asked.length = 0
  window.history.replaceState(null, '', '/reports?from=2026-10-01&to=2026-10-23&team=t-dev')
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      asked.push(path)
      if (path === '/api/boards') return reply({ boards: [] })
      if (path === '/api/teams') return reply({ teams: [{ id: 't-dev', name: 'Разработка' }] })
      if (path.startsWith('/api/labels')) return reply({ labels: [] })
      if (path.startsWith('/api/reports/cards/count')) return reply({ total: 3, limit: 50000 })
      if (path.startsWith('/api/reports/people'))
        return reply({
          candidates: [
            { id: 'u-boris', name: 'Борис Дятлов' },
            { id: 'u-vera', name: 'Вера Соколова' },
          ],
          rows: [
            { userId: 'u-boris', name: 'Борис Дятлов', cards: 2, finished: 1, wip: 1, cycleTimeP50: 3.5, cycleTimeP85: 4 },
            { userId: null, name: '', cards: 1, finished: 0, wip: 0, cycleTimeP50: null, cycleTimeP85: null },
          ],
        })
      if (path.startsWith('/api/reports/slices')) return reply({ slices: [] })
      return reply({})
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

it('список исполнителей — люди выбранного подразделения, разрез по людям на экране', async () => {
  const user = userEvent.setup()
  render(<Reports />)

  // Люди спрошены тем же отбором, что и число карточек.
  await waitFor(() => expect(asked.some((p) => p.startsWith('/api/reports/people?') && p.includes('team=t-dev'))).toBe(true))
  expect(await screen.findByText('В списке — люди выбранных досок и подразделений.')).toBeTruthy()

  const table = await screen.findByRole('table')
  expect(within(table).getByRole('button', { name: /Только Борис Дятлов/ })).toBeTruthy()
  expect(within(table).getByText('Без исполнителя')).toBeTruthy()
  expect(within(table).getByText('3,5 дн.')).toBeTruthy()

  // Имя ставит отбор на человека: адрес меняется, и выгрузка — его.
  await user.click(within(table).getByRole('button', { name: /Только Борис Дятлов/ }))
  await waitFor(() => expect(window.location.search).toContain('assignee=u-boris'))
  expect(screen.getByRole('link', { name: 'Excel (XLSX)' }).getAttribute('href')).toContain('assignee=u-boris')
})
