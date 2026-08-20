// Приглашение адресное, и экран обязан это показывать до нажатия.
//
// Пока экран не знал, кто в браузере, вошедшему под своей почтой
// показывали форму заведения аккаунта на чужой адрес: он заполнял имя
// и пароль, жал «Присоединиться» и только тогда получал отказ. Сервер
// отказывает и сейчас — он последнее слово, — но нажимать на это
// человеку незачем.

import { render, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import type { InviteInfo } from '../shared/api/index.ts'

const inviteInfo = vi.fn()

vi.mock('../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../shared/api')>()
  return { ...real, api: { ...real.api, inviteInfo } }
})

const { InviteScreen } = await import('./Invite')

const INFO: InviteInfo = {
  orgName: 'Северный проект',
  email: 'boris@example.test',
  role: 'member',
  needsAccount: true,
}

it('вошедшему под другой почтой предлагают выйти, а не завести второй аккаунт', async () => {
  inviteInfo.mockResolvedValue(INFO)
  render(
    <InviteScreen
      token="t"
      signedInAs="anna@example.test"
      onJoined={() => {}}
      onCancel={() => {}}
    />,
  )

  await waitFor(() => expect(screen.getByText(/Северный проект/)).toBeTruthy())
  expect(screen.getByRole('button', { name: /Выйти и открыть приглашение заново/ })).toBeTruthy()
  // Ни формы заведения аккаунта, ни кнопки, которая всё равно откажет.
  expect(screen.queryByLabelText('Как вас зовут')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Присоединиться' })).toBeNull()
})

it('тому, кому приглашение выписано, показывают обычную форму', async () => {
  inviteInfo.mockResolvedValue(INFO)
  render(
    <InviteScreen token="t" signedInAs={null} onJoined={() => {}} onCancel={() => {}} />,
  )

  await waitFor(() => expect(screen.getByLabelText('Как вас зовут')).toBeTruthy())
  expect(screen.getByRole('button', { name: 'Присоединиться' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: /Выйти и открыть/ })).toBeNull()
})
