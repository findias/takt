import type { ComponentProps } from 'react'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { ColumnView } from './ColumnView.tsx'
import { t } from '../../shared/i18n/index.ts'

function props(): ComponentProps<typeof ColumnView> {
  return {
    columnId: 'column', name: 'Queue',
    column: { id: 'column', name: 'Queue', position: 'a0', kind: 'queue',
      isStartedPoint: false, isFinishedPoint: false, policy: '', wipLimit: null, wipLimitHard: false },
    collapsed: false, grouped: false, canEdit: true, cardIds: [], hiddenByFilter: 0,
    cards: {}, unit: 'points', sleDays: null, people: {}, assignable: {},
    cardAssignees: {}, justMoved: null, labels: [], width: null, boardId: 'board',
    cardLabels: {}, cardRefs: {}, parents: {}, iterations: {}, iterationLate: {},
    iterationEnd: {}, changed: {}, children: {}, holds: {}, waitsFor: {}, selected: new Set(), columns: [],
    onToggleCollapsed: vi.fn(), onAssign: vi.fn(), onResize: vi.fn(), onLabel: vi.fn(),
    onEpic: vi.fn(), onSelect: vi.fn(), onPrioritise: vi.fn(), onBlock: vi.fn(),
    onUnblock: vi.fn(), onMarkDone: vi.fn(), onSubtask: vi.fn(), onMoveToColumn: vi.fn(),
    onOpenCard: vi.fn(), onMoveByKeyboard: vi.fn(), onNavigate: vi.fn(), onCreateCard: vi.fn(),
    onRenameColumn: vi.fn(), onSetLimit: vi.fn(), onUpdateColumn: vi.fn(),
    onRenameCard: vi.fn(), onArchiveCard: vi.fn(),
  }
}

it('верхний вход возвращает в единственную форму с черновиком и последовательным вводом', async () => {
  const user = userEvent.setup()
  const p = props()
  const { container } = render(<ColumnView {...p} />)
  const header = within(container.querySelector('header')!)
  const top = header.getByRole('button', { name: t.column.addCardTo(p.name) })
  await user.click(top)
  const field = await screen.findByRole('textbox', { name: t.column.whatToDo })
  expect(document.activeElement).toBe(field)
  await user.type(field, 'First')
  await user.click(top)
  expect(screen.getAllByRole('textbox', { name: t.column.whatToDo })).toHaveLength(1)
  expect(document.activeElement).toBe(field)
  expect((field as HTMLTextAreaElement).value).toBe('First')
  await user.keyboard('{Enter}')
  await user.type(field, 'Second{Enter}')
  expect(p.onCreateCard).toHaveBeenNthCalledWith(1, 'First', [])
  expect(p.onCreateCard).toHaveBeenNthCalledWith(2, 'Second', [])
  await user.keyboard('{Escape}')
  const bottom = screen.getAllByRole('button', { name: t.column.addCardTo(p.name) }).find((b) => b !== top)!
  await user.click(bottom)
  expect(await screen.findByRole('textbox', { name: t.column.whatToDo })).toBeTruthy()
})

it('верхний вход скрыт у наблюдателя и в свёрнутой колонке', () => {
  const p = props()
  const { rerender } = render(<ColumnView {...p} canEdit={false} />)
  expect(screen.queryByRole('button', { name: t.column.addCardTo(p.name) })).toBeNull()
  rerender(<ColumnView {...p} collapsed />)
  expect(screen.queryByRole('button', { name: t.column.addCardTo(p.name) })).toBeNull()
})
