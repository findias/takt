import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { SubtaskRow } from './SubtaskRow.tsx'
import type { Related } from '../../entities/card/model.ts'
const part: Related = {
  id: 'child', title: 'Подготовить поставку', priority: 'highest', kind: 'subtask',
  where: '', elsewhere: null, stage: null, promise: null, done: false, blocked: false,
  blockedReason: null, reachable: true, onThisBoard: true, assignees: [], replies: 0,
}
it('строка своей и видимой чужой подзадачи показывает её приоритет, скрытая не раскрывает его', () => {
  const props = { people: {}, assignees: [], replies: 0, onOpen: vi.fn(), onMarkDone: vi.fn() }
  const { rerender } = render(<SubtaskRow {...props} subtask={part} />)
  expect(screen.getByTitle('Наивысший').textContent).toContain('горит')
  rerender(<SubtaskRow {...props} subtask={{ ...part, onThisBoard: false, priority: 'high' }} />)
  expect(screen.getByTitle('Высокий').textContent).toContain('важно')
  rerender(<SubtaskRow {...props} subtask={{ ...part, priority: 'medium' }} />)
  expect(screen.queryByTitle('Средний')).toBeNull()
  rerender(<SubtaskRow {...props} subtask={{ ...part, onThisBoard: false, reachable: false }} />)
  expect(screen.queryByTitle('Наивысший')).toBeNull()
})
