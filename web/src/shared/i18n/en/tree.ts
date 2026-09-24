// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { tree as ru } from '../ru/tree.ts'

export const tree: typeof ru = {
    label: 'Work tree of the board',
    intro:
      "The board's epics, features and tasks as a hierarchy, together with their parts on other boards. Filters do not apply here: a tree with half its branches missing cannot say how far the epic is.",
    empty: 'No card on this board has parts yet — there is no tree.',
    alone: (n: number) => `No parent and no parts: ${n} ${n === 1 ? 'card' : 'cards'}`,
    toggle: (title: string, open: boolean) => `${open ? 'Collapse' : 'Expand'} “${title}”`,
    blocked: 'blocked',
    done: 'done',
}
