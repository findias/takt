// Состояния экрана.
//
// Здесь проверяется то, что легко потерять при правке и невозможно
// увидеть на снимке: заглушка не мелькает на быстром ответе, ошибка
// называет причину и даёт что нажать, пустота объясняет, а не
// констатирует.

import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EmptyState, ErrorState, Skeleton } from './states.tsx'

beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }))
afterEach(() => vi.useRealTimers())

describe('заглушка', () => {
  // Мигание хуже ожидания: заглушка, мелькнувшая на сто миллисекунд,
  // читается как сбой отрисовки.
  it('не появляется раньше задержки', async () => {
    const { container } = render(<Skeleton lines={2} />)
    expect(container.querySelector('.skeleton')).toBeNull()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(250)
    })
    expect(container.querySelectorAll('.skeleton-line').length).toBe(2)
  })

  it('исчезает вместе с содержимым и ничего не оставляет скринридеру', async () => {
    const { container, unmount } = render(<Skeleton />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250)
    })

    // Заглушка — картинка ожидания, читать её вслух нечего.
    expect(container.querySelector('.skeleton')?.getAttribute('aria-hidden')).toBe('true')
    unmount()
    expect(container.querySelector('.skeleton')).toBeNull()
  })
})

describe('ошибка', () => {
  it('называет причину и даёт повторить', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    const onRetry = vi.fn()
    render(<ErrorState what="загрузить доску" error="Нет связи с сервером" onRetry={onRetry} />)

    // Тревога, а не тихое сообщение: экран не показал того, за чем
    // пришли.
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toContain('Не удалось загрузить доску')
    expect(alert.textContent).toContain('Нет связи с сервером')

    await user.click(screen.getByRole('button', { name: 'Повторить' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('без обработчика повтора кнопки нет', () => {
    // Кнопка, которая ничего не делает, хуже её отсутствия.
    render(<ErrorState what="прочитать команду" error="Отказано" />)
    expect(screen.queryByRole('button')).toBeNull()
  })
})

describe('пустота', () => {
  it('объясняет, а не констатирует', () => {
    render(
      <EmptyState title="Досок пока нет">
        Доска — это колонки и карточки: заведите первую внизу.
      </EmptyState>,
    )
    const note = screen.getByRole('note')
    expect(note.textContent).toContain('Досок пока нет')
    // «Нет данных» — сообщение о состоянии базы; человеку нужно знать,
    // что делать дальше.
    expect(note.textContent).toContain('заведите первую')
  })
})
