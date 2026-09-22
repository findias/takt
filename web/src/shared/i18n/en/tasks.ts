// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { tasks as ru } from '../ru/tasks.ts'

const count = (n: number) => `${n} ${n === 1 ? 'task' : 'tasks'}`

export const tasks: typeof ru = {
    whose: 'Whose tasks',
    mine: 'Mine',
    withDone: 'Show finished',
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
