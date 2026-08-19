// Копирование в буфер обмена.
//
// Проверка есть потому, что буфер обмена — единственное место
// в интерфейсе, куда человек не может заглянуть: скопировалось или нет,
// видно только по сообщению. А копируются здесь значения, которые
// показываются один раз, — ссылка-приглашение и ключи.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { CopyButton } from './CopyButton.tsx'
import { ToastHost } from './Toast.tsx'

function show() {
  render(
    <ToastHost>
      <CopyButton value="ПРО-секрет" what="ключ доступа" />
    </ToastHost>,
  )
  return screen.getByRole('button', { name: 'Скопировать ключ доступа' })
}

function clipboard(impl: (() => Promise<void>) | null) {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: impl ? { writeText: vi.fn(impl) } : undefined,
  })
}

afterEach(() => clipboard(null))

it('получилось — говорит, что именно скопировано', async () => {
  clipboard(() => Promise.resolve())
  await userEvent.click(show())

  expect(await screen.findByText('Скопировали ключ доступа.')).toBeTruthy()
  expect(navigator.clipboard.writeText).toHaveBeenCalledWith('ПРО-секрет')
})

it('браузер отказал — говорит, что делать руками', async () => {
  clipboard(() => Promise.reject(new Error('нет разрешения')))
  await userEvent.click(show())

  expect(await screen.findByText(/Не удалось скопировать ключ доступа/)).toBeTruthy()
})

it('буфера нет вовсе — тоже не молчит', async () => {
  clipboard(null)
  await userEvent.click(show())

  expect(await screen.findByText(/Браузер не дал скопировать ключ доступа/)).toBeTruthy()
})
