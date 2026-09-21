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

// Публичное демо (ROADMAP 30.1): главный путь посетителя — попробовать,
// а не войти, пароля у него нет. Кнопка одна главная на экран, и это она.
it('в демо вход предлагает попробовать без регистрации, и это главное действие', async () => {
  const signedIn = vi.fn()
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/auth/methods')
        return reply({
          password: { enabled: true },
          oidc: { enabled: false },
          signup: { enabled: false },
          demo: { enabled: true },
        })
      if (path === '/api/demo/sandbox' && init?.method === 'POST')
        return reply({ id: 'u1', orgId: 'o1', role: 'owner', sandboxExpiresAt: '2026-09-22T18:40:00Z' })
      return reply({ error: 'не то' }, 404)
    }),
  )
  const user = userEvent.setup()
  render(<Auth onSignedIn={signedIn} />)

  const tryIt = await screen.findByRole('button', { name: 'Попробовать без регистрации' })
  expect(tryIt.className).toContain('primary')
  // Отправка формы главная без класса (правило стилей), поэтому
  // уступить она может только явным `secondary`.
  expect(screen.getByRole('button', { name: 'Войти' }).className).toBe('secondary')

  await user.click(tryIt)
  await waitFor(() => expect(signedIn).toHaveBeenCalledOnce())
  expect(signedIn.mock.calls[0][0].sandboxExpiresAt).toBe('2026-09-22T18:40:00Z')
})

it('без демо кнопки «Попробовать» нет', async () => {
  установка(true)
  render(<Auth onSignedIn={() => {}} />)
  await screen.findByRole('button', { name: 'Завести новую организацию' })
  expect(screen.queryByRole('button', { name: /Попробовать/ })).toBeNull()
})
