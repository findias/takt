import { render, screen, waitFor, cleanup, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { PlanEditor } from './PlanEditor.tsx'
import { ApiError } from '../../shared/api/index.ts'
import type { Card } from '../../shared/api/index.ts'
const card = { id: 'a', plannedStartOn: null, plannedEndOn: null } as Card
afterEach(cleanup)
it('план сохраняет обе даты при выходе из блока, Enter не нужен', async () => {
  const save = vi.fn().mockResolvedValue(undefined)
  const user = userEvent.setup()
  render(<><PlanEditor card={card} canEdit onSave={save} /><button>Дальше</button></>)
  fireEvent.change(screen.getByLabelText('Плановое начало'), { target: { value: '2026-11-05' } })
  await user.click(screen.getByLabelText('Плановое начало'))
  await user.tab()
  expect(save).not.toHaveBeenCalled()
  fireEvent.change(screen.getByLabelText('Плановое окончание'), { target: { value: '2026-11-09' } })
  await user.tab()
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
  expect(save).toHaveBeenCalledWith('a', { plannedStartOn: '2026-11-05', plannedEndOn: '2026-11-09' }, { plannedStartOn: null, plannedEndOn: null })
})
it('обратный период не уходит, Escape отменяет, отказ сервера остаётся у поля', async () => {
  const save = vi.fn().mockRejectedValue(new Error('План изменён соседом'))
  const user = userEvent.setup()
  render(<><PlanEditor card={card} canEdit onSave={save} /><button>Дальше</button></>)
  const start = screen.getByLabelText('Плановое начало'), end = screen.getByLabelText('Плановое окончание')
  fireEvent.change(start, { target: { value: '2026-11-09' } }); fireEvent.change(end, { target: { value: '2026-11-05' } })
  await user.click(end); await user.click(screen.getByRole('button', { name: 'Дальше' }))
  expect(save).not.toHaveBeenCalled()
  expect(end.getAttribute('aria-invalid')).toBe('true')
  await user.click(start); await user.keyboard('{Escape}'); await user.click(screen.getByRole('button', { name: 'Дальше' }))
  expect(save).not.toHaveBeenCalled()
  fireEvent.change(start, { target: { value: '2026-11-05' } })
  await user.click(start); await user.keyboard('{Enter}')
  expect(await screen.findByText('План изменён соседом')).toBeTruthy()
})
it('снятие плана атомарно, наблюдателю не показываются поля', async () => {
  const save = vi.fn().mockResolvedValue(undefined), original = { ...card, plannedStartOn: '2026-11-05', plannedEndOn: '2026-11-09' }
  const { rerender } = render(<PlanEditor card={original} canEdit onSave={save} />)
  await userEvent.click(screen.getByRole('button', { name: 'Снять план' }))
  expect(save).toHaveBeenCalledWith('a', { plannedStartOn: null, plannedEndOn: null }, { plannedStartOn: '2026-11-05', plannedEndOn: '2026-11-09' })
  rerender(<PlanEditor card={original} canEdit={false} onSave={save} />)
  expect(screen.queryByLabelText('Плановое начало')).toBeNull()
})

it('после конфликта черновик уступает новым серверным датам', async () => {
  let reject: (error: unknown) => void = () => {}
  const save = vi.fn(() => new Promise<void>((_, no) => { reject = no }))
  const user = userEvent.setup()
  const { rerender } = render(<><PlanEditor card={card} canEdit onSave={save} /><button>Дальше</button></>)
  const input = screen.getByLabelText('Плановое начало')
  fireEvent.change(input, { target: { value: '2026-11-05' } })
  await user.click(input); await user.click(screen.getByRole('button', { name: 'Дальше' }))
  const fresh = { ...card, plannedStartOn: '2026-11-20' }
  rerender(<><PlanEditor card={fresh} canEdit onSave={save} /><button>Дальше</button></>)
  reject(new ApiError(409, { code: 'plan_changed', error: 'План изменён' }))
  await waitFor(() => expect((input as HTMLInputElement).value).toBe('2026-11-20'))
  expect(screen.getByText('План изменён')).toBeTruthy()
})
