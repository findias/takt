import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card } from '../../shared/api/index.ts'
import { GanttView } from './GanttView.tsx'
const card = { id: 'a', title: 'Релиз', number: 'РЕЛ-1', plannedStartOn: '2026-11-05', plannedEndOn: '2026-11-09', dueOn: '2026-11-10', percent: 42 } as Card
const base = { cards: { a: card }, links: [], linked: {}, cardAssignees: {}, people: {} } as unknown as BaseState
function props() { return { base, order: { column: ['a'] }, query: new URLSearchParams('view=gantt&ganttFrom=2026-11-01'), onQuery: vi.fn(), onOpenCard: vi.fn(), canEdit: true, onSave: vi.fn().mockResolvedValue(undefined) } }
it('название открывает карточку, масштаб сохраняет отбор в URL, обязательство и процент видны', async () => {
 const p = props(), user = userEvent.setup()
 render(<GanttView {...p} />)
 expect(screen.getByText('· 42%', { exact: false })).toBeTruthy()
 expect(screen.getByRole('img', { name: 'Обязательство: 2026-11-10' })).toBeTruthy()
 await user.click(screen.getByRole('button', { name: 'РЕЛ-1 Релиз' }))
 expect(p.onOpenCard).toHaveBeenCalledWith('a')
 await user.selectOptions(screen.getByRole('combobox', { name: 'Масштаб' }), 'month')
 expect(p.onQuery.mock.calls[0][0].get('view')).toBe('gantt')
 expect(p.onQuery.mock.calls[0][0].get('ganttScale')).toBe('month')
})
it('стрелки сдвигают период и края, отказ сохраняет исходное положение', async () => {
 const p = props(), user = userEvent.setup()
 render(<GanttView {...p} />)
 await user.click(screen.getByRole('button', { name: /^Сдвинуть план:/ }))
 await user.keyboard('{ArrowRight}')
 await waitFor(() => expect(p.onSave).toHaveBeenCalledTimes(1))
 expect(p.onSave.mock.calls[0]).toEqual(['a', { plannedStartOn:'2026-11-06',plannedEndOn:'2026-11-10' }, { plannedStartOn:'2026-11-05',plannedEndOn:'2026-11-09' }])
 p.onSave.mockRejectedValue(new Error('План уже изменён'))
 await user.click(screen.getByRole('button', { name: /^Изменить окончание плана:/ }))
 await user.keyboard('{ArrowLeft}')
 expect(await screen.findByText('План уже изменён')).toBeTruthy()
})
it('перенос пишет один запрос при отпускании, Escape отменяет следующий перенос', async () => {
 const p = props()
 render(<GanttView {...p} />)
 const bar = screen.getByRole('button', { name: /^Сдвинуть план:/ })
 Object.assign(bar, { setPointerCapture: vi.fn() })
 fireEvent.pointerDown(bar, { button: 0, pointerId: 1, clientX: 100 })
 fireEvent.pointerMove(bar, { pointerId: 1, clientX: 164 })
 expect(p.onSave).not.toHaveBeenCalled()
 fireEvent.pointerUp(bar, { pointerId: 1, clientX: 164 })
 await waitFor(() => expect(p.onSave).toHaveBeenCalledTimes(1))
 expect(p.onSave.mock.calls[0][1]).toEqual({ plannedStartOn:'2026-11-07',plannedEndOn:'2026-11-11' })
 fireEvent.pointerDown(bar, { button: 0, pointerId: 2, clientX: 100 })
 fireEvent.pointerMove(bar, { pointerId: 2, clientX: 196 })
 fireEvent.keyDown(bar, { key: 'Escape' })
 fireEvent.pointerUp(bar, { pointerId: 2, clientX: 196 })
 expect(p.onSave).toHaveBeenCalledTimes(1)
 fireEvent.pointerDown(bar, { button: 0, pointerId: 3, clientX: 100 })
 fireEvent.pointerMove(bar, { pointerId: 3, clientX: 3300 })
 fireEvent.pointerUp(bar, { pointerId: 3, clientX: 3300 })
 await waitFor(() => expect(p.onSave).toHaveBeenCalledTimes(2))
 expect(p.onSave.mock.calls[1][1].plannedStartOn).toBe('2027-02-13')
})
it('наблюдатель видит полосу, но не ручки; задачи без плана и пустой отбор объяснены', () => {
 const p = props()
 const { rerender } = render(<GanttView {...p} canEdit={false} />)
 expect(screen.queryByRole('button', { name: /^Сдвинуть план:/ })).toBeNull()
 rerender(<GanttView {...p} base={{ ...base, cards: { a: { ...card, plannedEndOn: null } } }} />)
 expect(screen.getByText('Без полного плана')).toBeTruthy()
 rerender(<GanttView {...p} order={{ column: [] }} />)
 expect(screen.getByText('По этому отбору задач нет')).toBeTruthy()
})

it('приоритеты родителя и подзадачи показаны независимо', () => {
 const p = props(), child = { ...card, id: 'child', title: 'Подзадача', number: 'РЕЛ-2', priority: 'high' as const }
 render(<GanttView {...p} base={{ ...base, cards: { a: { ...card, priority: 'highest' }, child }, links: [{ fromCard: 'a', toCard: 'child', kind: 'subtask' }] }} order={{ column: ['a','child'] }} />)
 expect(screen.getByTitle('Наивысший').textContent).toContain('горит')
 expect(screen.getByTitle('Высокий').textContent).toContain('важно')
})

it('подзадача подписана и связана с родителем; свёртка убирает название и полосу вместе', async () => {
 const p = props(), user = userEvent.setup()
 const child = { ...card, id: 'child', title: 'Подготовить пакет', number: 'РЕЛ-2' }
 render(<GanttView {...p} base={{ ...base, cards: { a: card, child }, links: [{ fromCard: 'a', toCard: 'child', kind: 'subtask' }] }} order={{ column: ['a','child'] }} />)
 expect(screen.getByText('Подзадача')).toBeTruthy()
 expect(screen.getByText(', родитель: Релиз')).toBeTruthy()
 const collapse = screen.getByRole('button', { name: 'Свернуть подзадачи «Релиз»' })
 expect(collapse.getAttribute('aria-expanded')).toBe('true')
 await user.click(collapse)
 expect(screen.queryByRole('button', { name: 'РЕЛ-2 Подготовить пакет' })).toBeNull()
 expect(screen.queryByRole('button', { name: /^Сдвинуть план: РЕЛ-2/ })).toBeNull()
 const expand = screen.getByRole('button', { name: 'Развернуть подзадачи «Релиз»' })
 expect(document.activeElement).toBe(expand)
 await user.keyboard(' ')
 expect(screen.getByRole('button', { name: 'РЕЛ-2 Подготовить пакет' })).toBeTruthy()
 expect(screen.getByRole('button', { name: /^Сдвинуть план: РЕЛ-2/ })).toBeTruthy()
 expect(p.onSave).not.toHaveBeenCalled()
 expect(p.onQuery).not.toHaveBeenCalled()
})
