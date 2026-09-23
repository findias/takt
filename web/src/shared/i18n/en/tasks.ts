// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { tasks as ru } from '../ru/tasks.ts'

const count = (n: number) => `${n} ${n === 1 ? 'task' : 'tasks'}`

export const tasks: typeof ru = {
    whose: 'Whose tasks',
    mine: 'Mine',
    withDone: 'Show finished',
    status: 'Status',
    statusAny: 'Any',
    statuses: { queue: 'Not started', in_progress: 'In progress', done: 'Done', blocked: 'Blocked' },
    due: 'Due',
    dueAny: 'Any',
    dues: { overdue: 'Overdue', soon: 'Within 3 days', none: 'No due date' },
    label: 'Label',
    labelAny: 'Any',
    reset: 'Clear filters',
    shownOf: (shown: number, total: number) => `${shown} of ${total}`,
    emptyFiltered: 'No task matches the filters.',
    count,
    truncated: (n: number) => `The first ${n} are shown: open the boards to see the rest.`,
    caption: (who: string) => `Tasks: ${who}`,
    colNumber: 'Number',
    colTask: 'Task',
    colBoard: 'Board',
    colColumn: 'Column',
    colDue: 'Due',
    colPriority: 'Priority',
    colLabels: 'Labels',
    colAge: 'In progress',
    days: (n: number) => `${n} d`,
    blocked: 'blocked',
    done: 'finished',
    empty: 'No tasks on the boards you can see.',
    loadFailed: 'Could not load the tasks',
    hint: 'Cards where the person is an assignee, on every board you can see. Private boards you have no access to are not here.',
}
