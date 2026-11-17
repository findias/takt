import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { fromSnapshot } from '../../entities/board/model.ts'
import type { Card, Snapshot } from '../../shared/api/index.ts'
import { TreeView } from './TreeView.tsx'
import { navigate } from '../../shared/router/index.ts'

const { request } = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('../../shared/api/index.ts', async (original) => ({ ...await original<typeof import('../../shared/api/index.ts')>(), request }))

function base() {
  const cards = ['Эпик', 'Доставка', 'Другое'].map((title, i): Card => ({ id: `${i}`, number: `T-${i}`, title,
    columnId: 'q', position: `${i}`, description: '', version: 1, columnEnteredAt: '2026-01-01',
    startedAt: null, finishedAt: null, outcome: null, estimate: null, comments: 0, priority: 'medium', dueOn: null, doneAt: null,
    ...(i === 0 ? { progress: { done: 1, total: 2, byWeight: false } } : {}) }))
  return fromSnapshot({ board: { id: 'b', name: 'Доска', key: 'T', version: 1, sleDays: null, sleProbability: 85 },
    columns: [{ id: 'q', name: 'Очередь', position: 'a', kind: 'queue', isStartedPoint: false, isFinishedPoint: false, policy: '', wipLimit: null, wipLimitHard: false }],
    cards, links: [{ fromCard: '0', toCard: '1', kind: 'subtask' }], linked: [], iterations: [], cardIterations: {}, fields: [], fieldValues: {}, cardRefs: {}, people: [], labels: [], cardLabels: {}, cardAssignees: {} } satisfies Snapshot)
}
beforeEach(() => { navigate('/board/b?view=tree&blocked=1'); request.mockReset() })

it('показывает настоящий прогресс, раскрывает ветку и открывает карточку', async () => {
  const open = vi.fn(), user = userEvent.setup()
  render(<TreeView base={base()} onOpenCard={open} />)
  expect(screen.getByRole('progressbar', { name: 'Выполнение «Эпик»' }).getAttribute('value')).toBe('50')
  await user.click(screen.getByRole('button', { name: 'Свернуть «Эпик»' }))
  expect(screen.queryByRole('button', { name: 'Доставка' })).toBeNull()
  await user.click(screen.getByRole('button', { name: 'Раскрыть «Эпик»' }))
  await user.click(screen.getByRole('button', { name: 'Доставка' }))
  expect(open).toHaveBeenCalledWith('1')
})
it('поиск раскрывает предков и настройки не сбрасывают отбор доски', async () => {
  const user = userEvent.setup()
  render(<TreeView base={base()} onOpenCard={vi.fn()} />)
  await user.click(screen.getByRole('button', { name: 'Свернуть всё' }))
  await user.type(screen.getByRole('searchbox', { name: 'Найти в дереве' }), 'доставка')
  expect(screen.getByRole('button', { name: 'Доставка' })).toBeTruthy()
  expect(screen.getByText('Родитель совпадения')).toBeTruthy()
  expect(new URLSearchParams(location.search).get('blocked')).toBe('1')
  await user.click(screen.getByText('Столбцы', { exact: true }))
  await user.click(screen.getByRole('checkbox', { name: 'Приоритет' }))
  expect(screen.queryByRole('columnheader', { name: 'Приоритет' })).toBeNull()
  expect(new URLSearchParams(location.search).get('treeColumns')).toBe('where,progress')
})
it('возвращает настройки из URL и называет неполноту чужой ветки с повтором', async () => {
  navigate('/board/b?view=tree&treeAlone=1&treeColumns=none')
  const b = base()
  b.links.push({ fromCard: '0', toCard: 'foreign', kind: 'subtask' })
  request.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ nodes: [] })
  const user = userEvent.setup()
  render(<TreeView base={b} onOpenCard={vi.fn()} />)
  expect(screen.getByRole('button', { name: 'Другое' })).toBeTruthy()
  expect(screen.getAllByRole('columnheader')).toHaveLength(1)
  await waitFor(() => expect(screen.getByText('Часть веток других досок не загрузилась. Дерево неполное.')).toBeTruthy())
  await user.click(screen.getByRole('button', { name: 'Загрузить ветки ещё раз' }))
  await waitFor(() => expect(screen.queryByText('Часть веток других досок не загрузилась. Дерево неполное.')).toBeNull())
  expect(request).toHaveBeenCalledTimes(2)
})
