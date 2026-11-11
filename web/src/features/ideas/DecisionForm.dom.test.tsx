import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import type { Idea } from '../../shared/api/index.ts'
import { DecisionForm } from './DecisionForm.tsx'
import { decideIdea } from './api.ts'

vi.mock('./api.ts', () => ({ decideIdea: vi.fn() }))

const idea: Idea = {
  id: 'idea-1', title: 'Общий чат', body: '', authorId: 'u1', author: 'Анна',
  teamId: null, team: null, votes: 0, voters: [], voted: false,
  createdAt: '2026-11-01T12:00:00Z', decision: null, canDecide: true, canMove: true,
}

beforeEach(() => {
  vi.mocked(decideIdea).mockReset()
})

it('после обрыва повторяет тот же requestId, изменённое решение отправляет с новым', async () => {
  vi.mocked(decideIdea).mockRejectedValue(new Error('Ответ потерян'))
  const user = userEvent.setup()
  render(<DecisionForm idea={idea} boards={[]} others={[]} onDecided={vi.fn()} onCancel={vi.fn()} />)

  await user.click(screen.getByRole('radio', { name: 'Позже' }))
  await user.click(screen.getByRole('button', { name: 'Записать' }))
  await screen.findByText('Ответ потерян')
  const first = vi.mocked(decideIdea).mock.calls[0][1]
  expect(first.requestId).toMatch(/^[0-9a-f-]{36}$/)

  await user.click(screen.getByRole('button', { name: 'Записать' }))
  await waitFor(() => expect(decideIdea).toHaveBeenCalledTimes(2))
  const second = vi.mocked(decideIdea).mock.calls[1][1]
  expect(second).toEqual(first)

  await user.type(screen.getByRole('textbox', { name: 'Пояснение' }), 'После согласования')
  await user.click(screen.getByRole('button', { name: 'Записать' }))
  await waitFor(() => expect(decideIdea).toHaveBeenCalledTimes(3))
  const changed = vi.mocked(decideIdea).mock.calls[2][1]
  expect(changed.requestId).not.toBe(first.requestId)
  expect(changed.note).toBe('После согласования')
})
