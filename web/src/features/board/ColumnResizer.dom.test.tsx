// Ручка ширины колонки (ROADMAP 28.2).
//
// Ручка — орган управления, а не декоративная полоска: названа
// диктором, ходит с клавиатуры, двойной щелчок возвращает исходное.
// И главное для скорости доски: во время перетаскивания ширина едет
// переменной на элементе колонки, а сохраняется один раз — на
// отпускании. Сохранение на каждый `pointermove` перерисовывало бы
// доску десятки раз в секунду.

import { useRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ColumnResizer } from './ColumnResizer.tsx'

function Host({ width, onCommit }: { width: number | null; onCommit: (rem: number | null) => void }) {
  const ref = useRef<HTMLElement>(null)
  return (
    <section ref={ref} aria-label="Готово" data-testid="column">
      <ColumnResizer name="Готово" width={width} columnRef={ref} onCommit={onCommit} />
    </section>
  )
}

function show(width: number | null = null) {
  const onCommit = vi.fn()
  render(<Host width={width} onCommit={onCommit} />)
  return { onCommit, handle: screen.getByRole('separator', { name: 'Ширина колонки «Готово»' }) }
}

describe('ручка ширины колонки', () => {
  it('названа, стоит в порядке обхода и сообщает ширину и пределы', () => {
    const { handle } = show(20)
    expect(handle.getAttribute('aria-orientation')).toBe('vertical')
    expect(handle.tabIndex).toBe(0)
    expect(handle.getAttribute('aria-valuenow')).toBe('20')
    expect(handle.getAttribute('aria-valuemin')).toBe('12')
    expect(handle.getAttribute('aria-valuemax')).toBe('40')
  })

  it('без своей ширины сообщает ширину по умолчанию', () => {
    const { handle } = show(null)
    expect(handle.getAttribute('aria-valuenow')).toBe('17.5')
  })

  it('стрелки меняют ширину шагом и не выходят за пределы', async () => {
    const { onCommit, handle } = show(20)
    handle.focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(onCommit).toHaveBeenLastCalledWith(21)
    await userEvent.keyboard('{ArrowLeft}')
    expect(onCommit).toHaveBeenLastCalledWith(19)
  })

  it('у предела стрелка ничего не сохраняет', async () => {
    const { onCommit, handle } = show(40)
    handle.focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(onCommit).not.toHaveBeenCalled()
  })

  it('двойной щелчок возвращает исходную ширину', async () => {
    const { onCommit, handle } = show(30)
    await userEvent.dblClick(handle)
    expect(onCommit).toHaveBeenLastCalledWith(null)
  })

  it('перетаскивание двигает переменную, а сохраняет один раз — на отпускании', () => {
    const { onCommit, handle } = show(20)
    const column = screen.getByTestId('column')
    // Корень в jsdom — 16px: сдвиг на 32px — это 2rem.
    fireEvent.pointerDown(handle, { clientX: 100, pointerId: 1, button: 0 })
    fireEvent.pointerMove(handle, { clientX: 116, pointerId: 1 })
    fireEvent.pointerMove(handle, { clientX: 132, pointerId: 1 })
    expect(column.style.getPropertyValue('--column-width')).toBe('22rem')
    expect(onCommit).not.toHaveBeenCalled()
    fireEvent.pointerUp(handle, { clientX: 132, pointerId: 1 })
    expect(onCommit).toHaveBeenCalledTimes(1)
    expect(onCommit).toHaveBeenCalledWith(22)
  })

  it('перетаскивание за предел упирается в предел', () => {
    const { onCommit, handle } = show(20)
    fireEvent.pointerDown(handle, { clientX: 500, pointerId: 1, button: 0 })
    fireEvent.pointerMove(handle, { clientX: 0, pointerId: 1 })
    fireEvent.pointerUp(handle, { clientX: 0, pointerId: 1 })
    expect(onCommit).toHaveBeenCalledWith(12)
  })
})
