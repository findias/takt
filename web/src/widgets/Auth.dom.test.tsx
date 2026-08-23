// Экран входа не предлагает дверь, которой нет.
//
// Регистрацию можно закрыть — на установке, выставленной в корпоративную
// сеть, организации заводит владелец, а не всякий, кто открыл адрес.
// Кнопка «Завести новую организацию» на такой установке ведёт ровно
// в отказ, и это тот же случай, что уже разобран с корпоративным входом:
// показывать способ, которого нет, и объяснять это после нажатия —
// значит тратить чужое нажатие на «нельзя».

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { Auth } from './Auth.tsx'

function reply(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

/** Установка отвечает, чем в неё можно войти и можно ли завестись. */
function установка(signup: boolean) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/methods')
        return reply({
          password: { enabled: true },
          oidc: { enabled: false },
          signup: { enabled: signup },
        })
      return reply({ error: 'на этой установке организации заводит владелец' }, 403)
    }),
  )
}

beforeEach(() => {
  window.history.replaceState({}, '', '/')
})
afterEach(() => vi.unstubAllGlobals())

it('на закрытой установке кнопки «Завести новую организацию» нет', async () => {
  установка(false)
  render(<Auth onSignedIn={() => {}} />)

  // Ждём ответа: до него кнопки тоже нет — появившаяся и исчезнувшая
  // кнопка хуже отсутствующей.
  await waitFor(() =>
    expect(screen.getByText(/Организации на этой установке заводит владелец/)).toBeTruthy(),
  )
  expect(screen.queryByRole('button', { name: 'Завести новую организацию' })).toBeNull()
  // Вход при этом на месте: закрыта регистрация, а не дверь.
  expect(screen.getByRole('button', { name: 'Войти' })).toBeTruthy()
})

it('на открытой — есть, и она ведёт к форме', async () => {
  установка(true)
  render(<Auth onSignedIn={() => {}} />)

  const кнопка = await screen.findByRole('button', { name: 'Завести новую организацию' })
  await userEvent.click(кнопка)
  expect(screen.getByRole('heading', { name: 'Новая организация' })).toBeTruthy()
  // И назад: человек, передумавший заводить организацию, не заперт.
  expect(screen.getByRole('button', { name: 'У меня уже есть аккаунт' })).toBeTruthy()
})
