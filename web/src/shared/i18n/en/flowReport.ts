import type { flowReport as ru } from '../ru/flowReport.ts'

export const flowReport: typeof ru = {
    cfdLabel: (days: number, queued: number, inProgress: number, done: number) =>
      `Cumulative flow diagram over ${days} ${days === 1 ? 'day' : 'days'}. Now: ${queued} queued, ${inProgress} in progress, ${done} done.`,
    cfdCaption:
      'Bottom to top: done, in progress, queued. A band that grows upwards while its lower edge stays put is work piling up, not moving.',
    cycleLabel: (points: number, min: string | number, max: string | number) =>
      `Cycle time per card: ${points} ${points === 1 ? 'point' : 'points'}, from ${min} to ${max} days.`,
    half: 'half',
    p85: '85 of 100',
    cardDays: (title: string, days: string | number) => `${title}: ${days} d`,
    cycleCaption:
      'Each dot is a finished card. Red ones took longer than 85 of 100: those are the ones to ask about.',
    agingLabel: (n: number, oldest: string | number) =>
      `Age of work in progress: ${n} ${n === 1 ? 'card' : 'cards'}, the oldest ${oldest} ${Number(oldest) === 1 ? 'day' : 'days'}.`,
    promise: 'promise',
    agingPoint: (title: string, days: string | number, column: string, blocked: boolean) =>
      `${title}: ${days} d in “${column}”${blocked ? ', blocked' : ''}`,
    agingCaption: 'Blocked cards are red: they age while nothing happens.',
    reportOf: (name: string) => `Report on iteration “${name}”`,
    reportFailed: 'Could not read the report.',
    counting: 'Counting…',
    closedOn: (when: string) => ` · closed ${when}, scope frozen`,
    running: ' · running, counted as of now',
    scope: 'What was in scope',
    noCards: 'Not a single card.',
    closedEmpty: 'That is how it closed.',
    stillEmpty: 'Empty so far.',
    doneLabel: 'done',
    doneWeight: (unit: string) => `done, ${unit}`,
    ofTotal: (done: string | number, total: string | number) => `${done} of ${total}`,
    lateAdded: 'added after the start',
    dropped: 'dropped along the way',
    unestimated:
      'Some cards in scope are not estimated — weight is not counted: a sum without them would show less than there was.',
    doneSr: 'Done. ',
    cardDropped: 'removed from the iteration',
    cardLate: 'added after the start',
    cardArchived: 'taken off the board',
}
