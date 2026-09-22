// Полоса тестового стенда (ROADMAP 30.7): видна только на стенде
// и показывает коммиты ветки на языке интерфейса.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { StandBar, reflow } from './StandBar.tsx'
import type { StandNote } from '../../shared/api/index.ts'

const NOTE: StandNote = {
  branch: 'feature/stand',
  version: 'v0.3.0-5-gabc1234',
  email: 'anna@example.test',
  password: 'parol12345',
  commits: [
    {
      hash: 'abc1234def',
      date: '2026-09-22T10:00:00+03:00',
      en: { title: 'Personal settings', body: 'The name opens a dialog.', check: '1. Click your name.' },
      ru: { title: 'Личные настройки', body: 'Имя открывает диалог.', check: '1. Нажмите на имя.' },
    },
    {
      hash: 'fff0000aaa',
      date: '2026-09-21T10:00:00+03:00',
      en: { title: 'Only English', body: '', check: '' },
      ru: null,
    },
  ],
}

function serve(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify(body), {
          status,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    ),
  )
}

afterEach(() => vi.unstubAllGlobals())

it('на установке заказчика полосы нет: ручка отвечает 404', async () => {
  serve(404, { error: 'not found' })
  const { container } = render(<StandBar />)
  await waitFor(() => expect(fetch).toHaveBeenCalled())
  expect(container.textContent).toBe('')
})

it('на стенде полоса называет ветку, а заметка — коммиты по-русски и как их проверить', async () => {
  serve(200, NOTE)
  render(<StandBar />)
  expect(await screen.findByText(/Тестовый стенд: ветка feature\/stand/)).toBeTruthy()

  await userEvent.click(screen.getByRole('button', { name: 'Что на стенде' }))
  const dialog = screen.getByRole('dialog', { name: 'Что на стенде' })
  // Пароль — там, где он нужен.
  expect(dialog.textContent).toMatch(/anna@example\.test, пароль parol12345/)
  // Русская половина, а не английская.
  expect(screen.getByRole('heading', { name: 'Личные настройки' })).toBeTruthy()
  expect(dialog.textContent).toMatch(/Нажмите на имя/)
  // Без перевода — английский текст с пометкой; без «как проверить» —
  // пометка, а не пустое место.
  expect(screen.getByRole('heading', { name: 'Only English' })).toBeTruthy()
  expect(dialog.textContent).toMatch(/Перевода в коммите нет/)
  expect(dialog.textContent).toMatch(/Как проверить — не написано/)
  // Ссылка на коммит — на GitHub.
  expect(screen.getByRole('link', { name: 'Коммит abc1234 на GitHub' }).getAttribute('href')).toBe(
    'https://github.com/findias/takt/commit/abc1234def',
  )
})

it('ручные переносы склеиваются, а пункты списка остаются на своих строках', () => {
  const text = 'Первая строка абзаца\nи её продолжение.\n\n1. Шаг первый\n   с переносом.\n2. Шаг второй.'
  expect(reflow(text)).toBe(
    'Первая строка абзаца и её продолжение.\n\n1. Шаг первый с переносом.\n2. Шаг второй.',
  )
})
