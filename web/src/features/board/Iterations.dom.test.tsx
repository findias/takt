import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { Iterations } from './Iterations.tsx'
import { api } from '../../shared/api/index.ts'
import type { Iteration } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

const iteration: Iteration = {
  id: 'iteration', name: 'Sprint 1', goal: '',
  startsOn: '2026-11-01', endsOn: '2026-11-15', closedAt: null, cardCount: 3,
}

it('закрытие через меню сохраняет подтверждение названием', async () => {
  const user = userEvent.setup()
  const close = vi.spyOn(api, 'closeIteration').mockResolvedValue(undefined)
  const onChanged = vi.fn()
  render(<Iterations boardId="board" canEdit iterations={[iteration]} onChanged={onChanged} onReport={vi.fn()} />)

  expect(screen.queryByRole('menuitem')).toBeNull()
  const trigger = screen.getByRole('button', { name: t.screen.closeIterationOf(iteration.name) })
  trigger.focus()
  await user.keyboard('{Enter}')
  const action = screen.getByRole('menuitem', { name: t.screen.closeIteration })
  expect(document.activeElement).toBe(action)
  await user.keyboard('{Enter}')

  expect(close).not.toHaveBeenCalled()
  const confirm = screen.getByRole('button', { name: t.screen.closeIteration })
  expect((confirm as HTMLButtonElement).disabled).toBe(true)
  await user.type(screen.getByRole('textbox', { name: t.screen.closeIterationConfirmName }), iteration.name)
  await user.click(confirm)
  expect(close).toHaveBeenCalledWith('board', iteration.id)
  expect(onChanged).toHaveBeenCalledOnce()
})

it('наблюдатель видит отчёт, но не меню закрытия', async () => {
  const onReport = vi.fn()
  render(<Iterations boardId="board" canEdit={false} iterations={[iteration]} onChanged={vi.fn()} onReport={onReport} />)
  expect(screen.queryByRole('button', { name: t.screen.closeIterationOf(iteration.name) })).toBeNull()
  await userEvent.click(screen.getByRole('button', { name: /Sprint 1/ }))
  expect(onReport).toHaveBeenCalledWith(iteration)
})
