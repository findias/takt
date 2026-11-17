import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { CardPanelActions } from './CardPanelActions.tsx'
import { t } from '../../shared/i18n/index.ts'
import { cardLink } from '../../shared/router/index.ts'

function show(canEdit = true, optional = true) {
  const callbacks = { onArchive: vi.fn(), onDelete: vi.fn(), onMoveToBoard: vi.fn() }
  render(<CardPanelActions
    boardId="board-1" cardId="card-2" title="Смета" canEdit={canEdit}
    onArchive={callbacks.onArchive}
    onDelete={optional ? callbacks.onDelete : undefined}
    onMoveToBoard={optional ? callbacks.onMoveToBoard : undefined}
  />)
  return callbacks
}

it('прячет перенос в меню и передаёт действия над открытой карточкой родителю', async () => {
  const user = userEvent.setup()
  const callbacks = show()
  expect(screen.queryByText(t.panel.moveToBoard)).toBeNull()
  for (const [label, callback, args] of [
    [t.panel.moveToBoard, callbacks.onMoveToBoard, ['card-2', 'Смета']],
    [t.cardView.archive, callbacks.onArchive, ['card-2']],
    [t.cardView.deleteForever, callbacks.onDelete, ['card-2', 'Смета']],
  ] as const) {
    await user.click(screen.getByRole('button', { name: t.cardView.actions('Смета') }))
    await user.click(screen.getByRole('menuitem', { name: label }))
    expect(callback).toHaveBeenCalledExactlyOnceWith(...args)
    expect(screen.queryByRole('menu')).toBeNull()
  }
})

it('наблюдателю оставляет только копирование даже с callbacks записи', async () => {
  const user = userEvent.setup()
  const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  const callbacks = show(false)
  await user.click(screen.getByRole('button', { name: t.cardView.actions('Смета') }))
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([t.panel.copyLink])
  await user.click(screen.getByRole('menuitem', { name: t.panel.copyLink }))
  expect(writeText).toHaveBeenCalledExactlyOnceWith(cardLink('board-1', 'card-2'))
  Object.values(callbacks).forEach((callback) => expect(callback).not.toHaveBeenCalled())
})

it('без callbacks не предлагает удаление и перенос; Escape возвращает фокус', async () => {
  const user = userEvent.setup()
  show(true, false)
  const trigger = screen.getByRole('button', { name: t.cardView.actions('Смета') })
  await user.click(trigger)
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([
    t.panel.copyLink, t.cardView.archive,
  ])
  await user.keyboard('{Escape}')
  expect(document.activeElement).toBe(trigger)
})
