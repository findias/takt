// Строка текста с переносом (ROADMAP 37.3): Enter сохраняет, а не
// переводит строку; вставленные переводы строк становятся пробелами.

import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, it, vi } from 'vitest'
import { TextLine } from './TextLine.tsx'

function Harness({ onEnter }: { onEnter: (v: string) => void }) {
  const [value, setValue] = useState('')
  return <TextLine label="Причина" value={value} onChange={setValue} onEnter={() => onEnter(value)} />
}

it('Enter сохраняет, перевод строки из вставки становится пробелом', async () => {
  const saved = vi.fn()
  const user = userEvent.setup()
  render(<Harness onEnter={saved} />)
  const field = screen.getByRole('textbox', { name: 'Причина' })
  expect(field.tagName).toBe('TEXTAREA')

  fireEvent.change(field, { target: { value: 'ждём акт\nот склада' } })
  expect((field as HTMLTextAreaElement).value).toBe('ждём акт от склада')

  await user.type(field, '{Enter}')
  expect(saved).toHaveBeenCalledWith('ждём акт от склада')
  expect((field as HTMLTextAreaElement).value).not.toContain('\n')
})
