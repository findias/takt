// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { newCard as ru } from '../ru/newCard.ts'

export const newCard: typeof ru = {
    assignees: 'Who will do the new card',
    addAssignee: 'Assign the new card to',
    addAssigneePick: 'Assignee…',
    unassign: (name: string) => `Remove assignee ${name}`,
}
