// Сообщения.
//
// Главное здесь — отмена: она заменяет диалог «вы уверены?», который
// закрывают не читая. Поэтому проверяется, что действие доходит,
// что сообщение с действием живёт дольше обычного и что живая область
// объявляет всё это тем, кто не видит экрана.

import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastHost, useToast } from './Toast.tsx'

function Caller({ onReady }: { onReady: (push: ReturnType<typeof useToast>) => void }) {
  const push = useToast()
  onReady(push)
  return null
}

function show() {
  let push: ReturnType<typeof useToast> = () => {}
  render(
    <ToastHost>
      <Caller onReady={(p) => (push = p)} />
    </ToastHost>,
  )
  return (message: Parameters<ReturnType<typeof useToast>>[0]) => act(() => push(message))
}

beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }))
afterEach(() => vi.useRealTimers())

describe('сообщения', () => {
  it('показываются в живой области, а не тревогой', () => {
    const push = show()
    push({ text: 'Карточка перенесена', tone: 'info' })

    const live = screen.getByRole('status')
    expect(live.getAttribute('aria-live')).toBe('polite')
    expect(live.textContent).toContain('Карточка перенесена')
  })

  it('исчезают сами', async () => {
    const push = show()
    push({ text: 'Сохранено', tone: 'info' })
    expect(screen.getByText('Сохранено')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100)
    })
    expect(screen.queryByText('Сохранено')).toBeNull()
  })

  // Отмена — замена подтверждению: обычный случай проходит без лишнего
  // нажатия, редкая ошибка исправляется одним.
  it('сообщение с действием живёт дольше и это действие выполняет', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    const push = show()
    const onAct = vi.fn()
    push({ text: 'Карточка убрана', tone: 'info', action: { label: 'Отменить', onAct } })

    // Через шесть секунд обычное сообщение уже исчезло бы.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000)
    })
    expect(screen.getByText('Карточка убрана')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Отменить' }))
    expect(onAct).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Карточка убрана')).toBeNull()
  })

  it('больше трёх разом не показывает', () => {
    const push = show()
    for (let i = 1; i <= 5; i++) push({ text: `Сообщение ${i}`, tone: 'info' })

    // Стена сообщений не читается: остаются последние три.
    expect(screen.queryByText('Сообщение 1')).toBeNull()
    expect(screen.queryByText('Сообщение 2')).toBeNull()
    expect(screen.getByText('Сообщение 5')).toBeTruthy()
  })

  it('закрывается руками', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    const push = show()
    push({ text: 'Что-то пошло не так', tone: 'warning' })

    await user.click(screen.getByRole('button', { name: 'Закрыть сообщение' }))
    expect(screen.queryByText('Что-то пошло не так')).toBeNull()
  })
})
