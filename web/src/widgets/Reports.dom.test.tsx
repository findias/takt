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
        })
      // Сводка (ROADMAP 42.3, срез 1) — то же, что первым листом файла;
      // разрез по людям — внутри неё.
      if (path.startsWith('/api/reports/summary'))
        return reply({
          cards: 3,
          finished: 1,
          discarded: 0,
          wip: 1,
          cycleTime: { p50: 3.5, p85: 3.5 },
          age: { p50: 6, p85: 6 },
          due: { withDue: 7, finished: 4, onTime: 3, overdueNow: 2 },
          throughput: [
            { week: '2026-09-28', count: 0, created: 1 },
            { week: '2026-10-05', count: 1, created: 0 },
            { week: '2026-10-12', count: 0, created: 2 },
            { week: '2026-10-19', count: 0, created: 0 },
          ],
          flowStep: 'day',
          flow: [
            { day: '2026-10-01', queued: 2, inProgress: 1, done: 0 },
            { day: '2026-10-23', queued: 1, inProgress: 1, done: 1 },
          ],
          iterations: [
            { name: 'Неделя 41', board: 'Разработка', startsOn: '2026-10-05', endsOn: '2026-10-11', closed: true, planned: 4, done: 3 },
          ],
          teams: [
            { team: 'Разработка', cards: 2, finished: 1, wip: 1, cycleTimeP50: 3.5, cycleTimeP85: 3.5 },
            { team: '', cards: 1, finished: 0, wip: 0, cycleTimeP50: null, cycleTimeP85: null },
          ],
          people: [
            { userId: 'u-boris', name: 'Борис Дятлов', cards: 2, finished: 1, wip: 1, cycleTimeP50: 3.5, cycleTimeP85: 4, workTimeP50: 2 },
            { userId: null, name: '', cards: 1, finished: 0, wip: 0, cycleTimeP50: null, cycleTimeP85: null, workTimeP50: null },
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

  const table = await screen.findByRole('table', { name: 'По исполнителям' })
  expect(within(table).getByRole('button', { name: /Только Борис Дятлов/ })).toBeTruthy()
  expect(within(table).getByText('Без исполнителя')).toBeTruthy()
  expect(within(table).getByText('3,5 дн.')).toBeTruthy()

  // Имя ставит отбор на человека: адрес меняется, и выгрузка — его.
  await user.click(within(table).getByRole('button', { name: /Только Борис Дятлов/ }))
  await waitFor(() => expect(window.location.search).toContain('assignee=u-boris'))
  expect(screen.getByRole('link', { name: 'Excel (XLSX)' }).getAttribute('href')).toContain('assignee=u-boris')
})

// Сводка на экране (ROADMAP 42.3, срез 1): числа первого листа файла
// видны под отбором — сделано, в работе, цикл, итерации, подразделения;
// доля итерации — сделанное от взятого, без добавленного по ходу.
it('сводка за период видна на экране под тем же отбором', async () => {
  render(<Reports />)
  await waitFor(() => expect(asked.some((p) => p.startsWith('/api/reports/summary?') && p.includes('team=t-dev'))).toBe(true))
  expect(await screen.findByRole('heading', { name: 'Сводка за период' })).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Пропускная способность' })).toBeTruthy()
  expect(screen.getByRole('img', { name: /^Пропускная способность: .*сделано 1, создано 0/ })).toBeTruthy()
  // Сроки: доля в срок от сделанных со сроком, просроченные — числом.
  expect(screen.getByText('в срок из 4 сделанных со сроком').previousSibling?.textContent).toBe('75 %')
  expect(screen.getByText('просрочено сейчас').previousSibling?.textContent).toBe('2')
  expect(screen.getByRole('img', { name: /^Накопительная диаграмма потока/ })).toBeTruthy()

  const iterations = screen.getByRole('table', { name: 'Выполнение итераций' })
  const row = within(iterations).getByRole('row', { name: /Неделя 41/ })
  expect(within(row).getByText('75 %')).toBeTruthy()

  const teams = screen.getByRole('table', { name: 'По подразделениям' })
  expect(within(teams).getByText('Без подразделения')).toBeTruthy()
})
