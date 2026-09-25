import type { labelsAdmin as ru } from '../ru/labelsAdmin.ts'

export const labelsAdmin: typeof ru = {
    labelsFailed: 'Labels did not load',
    labelArchived: (name: string) => `Label “${name}” archived. It stays on the cards.`,
    restore: 'Restore',
    labels: 'Labels',
    labelsEmpty:
      'No labels. A label answers a yes-or-no question — urgent, external, waiting for an answer — and takes as much room on a card as a word. It is created for the whole organisation, for a subdivision and everything inside it, or for a single board.',
    archiveLabel: (name: string) => `Archive label “${name}”`,
    toArchive: 'Archive',
    archivedTitle: 'Archived',
    archivedExplain: 'They stay on cards and show there, but cannot be hung again until you restore them.',
    restoreLabel: (name: string) => `Restore label “${name}” from the archive`,
    restoreFromArchive: 'Restore from archive',
    labelName: 'Label name',
    labelTone: 'Label colour',
    toneOf: (name: string) => `Colour of label “${name}”`,
    labelPlace: 'Where the label applies',
    placeOrg: 'Organisation',
    placeTeam: 'Subdivision',
    placeBoard: 'Board',
    createLabel: 'Create label',
    wholeOrg: 'Whole organisation',
}
