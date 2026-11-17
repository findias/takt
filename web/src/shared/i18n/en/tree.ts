// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { tree as ru } from '../ru/tree.ts'

export const tree: typeof ru = {
    label: 'Work tree of the board',
    intro:
      "The board's epics, features and tasks as a hierarchy, together with their parts on other boards. Filters do not apply here: a tree with half its branches missing cannot say how far the epic is.",
    empty: 'No tasks with parts yet. Include tasks with no parent or parts, or add a subtask in a card.',
    alone: (n: number) => `No parent and no parts: ${n} ${n === 1 ? 'card' : 'cards'}`,
    toggle: (title: string, open: boolean) => `${open ? 'Collapse' : 'Expand'} “${title}”`,
    blocked: 'blocked',
    done: 'done',
    about: 'About this view',
    search: 'Search the tree',
    sort: 'Order', byNumber: 'By number', byTitle: 'By title',
    expandAll: 'Expand all', collapseAll: 'Collapse all', columns: 'Columns',
    columnNames: { where: 'Board / column', priority: 'Priority', progress: 'Completion' },
    task: 'Task', context: 'Parent of a match',
    noMatches: 'No matches. Change your search.',
    incomplete: 'Some branches on other boards could not be loaded. The tree is incomplete.',
    retry: 'Retry loading branches',
    rows: (n: number) => `Rows in expanded branches: ${n}`,
    level: (n: number) => `Level ${n}`,
    progress: (title: string) => `Completion of “${title}”`,
}
