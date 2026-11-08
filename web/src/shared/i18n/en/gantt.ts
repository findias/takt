import type { gantt as ru } from '../ru/gantt.ts'
export const gantt: typeof ru = {
  title: 'Gantt', plan: 'Plan', start: 'Planned start', end: 'Planned end',
  clear: 'Clear plan', invalid: 'Check the dates: the end must not be before the start',
  failed: 'Could not save the plan', saved: 'Plan saved', saving: 'Saving plan…',
  today: 'Today', previous: 'Previous period', next: 'Next period', scale: 'Scale',
  weeks: 'Weeks', months: 'Months', from: 'Period starts', task: 'Task',
  empty: 'No tasks match these filters', unplanned: 'Without a complete plan', outside: 'Show plan',
  context: 'Parent outside the filters', incomplete: 'Open the card to see parts on other boards',
  due: 'Commitment', blocked: 'Blocked', overdue: 'Plan overdue',
  move: 'Move plan', resizeStart: 'Change planned start', resizeEnd: 'Change planned end',
  hint: 'Move a bar or its edges. Dates are also in the card. Arrow keys move by a day, Shift by a week; Escape cancels dragging.',
  subtask: 'Subtask',
  children: (n: number) => `Subtasks: ${n}`,
  parentIs: (name: string) => `, parent: ${name}`,
  toggleChildren: (title: string, open: boolean) => `${open ? 'Collapse' : 'Expand'} subtasks of “${title}”`,
  noDates: 'No plan set',
  range: (a: string, b: string) => `${a || '…'} — ${b || '…'}`,
}
