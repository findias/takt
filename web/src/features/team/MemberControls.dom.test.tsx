import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import type { Member } from '../../shared/api/index.ts'
import { MemberControls } from './MemberControls.tsx'

const member: Member = { userId: 'boris', name: 'Борис Дятлов', email: 'boris@example.test',
  role: 'member', kind: 'person', joinedAt: '2026-01-01', nameEditable: true, emailEditable: true }
const props = () => ({ member, canManage: true, onRole: vi.fn(), onRename: vi.fn(), onEmail: vi.fn(), onIssueLink: vi.fn(), onRemove: vi.fn(), onErase: vi.fn() })

it('роль остаётся на виду, действия открываются с клавиатуры и передают выбранного человека', async () => {
  const p = props(), user = userEvent.setup()
  render(<MemberControls {...p} />)
  await user.selectOptions(screen.getByRole('combobox', { name: 'Роль: Борис Дятлов' }), 'viewer')
  expect(p.onRole).toHaveBeenCalledWith('boris', 'viewer')
  const trigger = screen.getByRole('button', { name: 'Действия участника: Борис Дятлов' })
  trigger.focus()
  await user.keyboard('{Enter}')
  expect(screen.getAllByRole('menuitem')).toHaveLength(5)
  await user.click(screen.getByRole('menuitem', { name: 'Имя…' }))
  expect(p.onRename).toHaveBeenCalledWith(member)
  expect(screen.queryByRole('menu')).toBeNull()
  expect(document.activeElement).toBe(trigger)
})
it('провайдерские имя и почта не получают обходных действий', async () => {
  const p = props(), user = userEvent.setup()
  render(<MemberControls {...p} member={{ ...member, nameEditable: false, emailEditable: false }} />)
  await user.click(screen.getByRole('button'))
  expect(screen.getAllByRole('menuitem').map((el) => el.textContent)).toEqual(['Исключить', 'Удалить данные'])
  await user.click(screen.getByRole('menuitem', { name: 'Удалить данные' }))
  expect(p.onErase).toHaveBeenCalledTimes(1)
  expect(p.onRemove).not.toHaveBeenCalled()
})
it('владелец подразделения видит только разрешённое обезличивание', async () => {
  const p = props(), user = userEvent.setup(), local = { ...member, erasable: true }
  render(<MemberControls {...p} member={local} canManage={false} />)
  expect(screen.queryByRole('combobox')).toBeNull()
  expect(screen.getByText('Участник')).toBeTruthy()
  await user.click(screen.getByRole('button'))
  expect(screen.getAllByRole('menuitem')).toHaveLength(1)
  await user.click(screen.getByRole('menuitem'))
  expect(p.onErase).toHaveBeenCalledWith(local)
})
it('обычное чтение и ключ интеграции не показывают административные действия', () => {
  const p = props()
  const { rerender } = render(<MemberControls {...p} canManage={false} />)
  expect(screen.queryByRole('button')).toBeNull()
  expect(screen.queryByRole('combobox')).toBeNull()
  rerender(<MemberControls {...p} member={{ ...member, kind: 'service', erasable: true }} />)
  expect(screen.getByText('ключ')).toBeTruthy()
  expect(screen.queryByRole('button')).toBeNull()
  expect(screen.queryByRole('combobox')).toBeNull()
})
