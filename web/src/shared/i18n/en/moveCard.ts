// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { moveCard as ru } from '../ru/moveCard.ts'

export const moveCard: typeof ru = {
    title: (card: string) => `Move “${card}” to another board`,
    board: 'Board',
    column: 'Column',
    confirm: 'Move',
    loading: 'Checking what will change…',
    noBoards: 'There are no other boards of the same level that you can write to. A card can only move to a board where you edit cards.',
    number: 'The card gets a number from that board. Search still finds it by the old number, and links to it keep working.',
    items: (n: number) =>
        `${n} ${n === 1 ? 'item moves' : 'items move'} with it — subtasks on this board. Subtasks on other boards stay where they are.`,
    dropped: (names: string) => `These labels do not apply on that board and will be removed: ${names}.`,
    iteration: (name: string) => `The card leaves the iteration “${name}” — that board has its own iterations. The iteration report does not change.`,
    unseen: (names: string) =>
        `These assignees cannot see that board: ${names}. Remove them from the card or give them access to the board, then move it.`,
    otherLevel: 'A portfolio and a team board are on different levels; a card cannot move between them.',
    previewFailed: 'Could not check what the move will change',
}
