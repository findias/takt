import type { flow as ru } from '../ru/flow.ts'

export const flow: typeof ru = {
    countFailed: 'Could not compute',
    period: 'Over how many days',
    weeks4: '4 weeks',
    months3: '3 months',
    halfYear: 'half a year',
    title: 'Flow',
    label: 'Flow metrics',
    counting: 'Counting…',
    cycleTime: 'Cycle time',
    noCycle:
      'No card was finished in this period. There is nothing to count — any number here would be made up.',
    halfIn: 'half within',
    p85In: '85 of 100 within',
    p95In: '95 of 100 within',
    days: (d: string | number) => `${d} d`,
    countedBy: (n: number, few: boolean) =>
      `Counted from ${n} ${n === 1 ? 'card' : 'cards'}${few ? ' — too few to rely on' : ''}.`,
    now: 'What is running now',
    wip: (n: number) =>
      `In progress: ${n}. Age matters more than cycle time: cycle time speaks of the past, age of what is stuck right now.`,
    nothingStarted:
      'Nothing has started — nothing to age. Age appears as soon as a card leaves the queue for work.',
    blockedSr: 'Blocked. ',
    cfd: 'How work piles up',
    throughput: 'Throughput',
    week: (when: string) => `week of ${when}`,
    byWeek: 'By week, finished work only.',
    discarded: (n: number) => ` Another ${n} taken off the board unfinished — they do not count.`,
    noThroughput:
      'Nothing finished yet — nothing to count. Bars appear when the first card reaches the column marked as finish.',
    forecast: 'How long it will take',
    cards: 'cards',
    half: 'half',
    p85: '85 of 100',
    p95: '95 of 100',
    forecastExplain:
      'The forecast adds up random weeks from the past — a thousand trials. It says only one thing: what happens if things go on as they went.',
    tooLittle: (cards: number, weeks: number) =>
      ` So far the past holds only ${cards} ${cards === 1 ? 'card' : 'cards'} over ${weeks} ${weeks === 1 ? 'week' : 'weeks'} — too little to rely on.`,
    throughputLabel: (list: string) => `Throughput: ${list}`,
    promise: 'Board promise',
    noPromise:
      'No promise. A board without history cannot promise anything — but once finished cards appear, the promise is worth naming: the age of running work is compared with it.',
    promiseIs: (p: number, days: number) => `${p}% of work passes the board within ${days} ${days === 1 ? 'day' : 'days'}`,
    promiseExplain:
      '. Card age is compared with this: a card that exceeds it gets a mark right on the board.',
    takeFromHistory: (days: number) => `Take from history: ${days} ${days === 1 ? 'day' : 'days'}`,
    updateFromHistory: (days: number) => `Update from history: ${days} ${days === 1 ? 'day' : 'days'}`,
    dropPromise: 'Drop the promise',
    nothingToTake: 'Nothing to take from history yet: no card has been finished.',
    importedNote: (n: number) =>
      `${n} ${n === 1 ? 'card' : 'cards'} in this report ${n === 1 ? 'was' : 'were'} imported from another system. Their dates come from there, and since the start of work there is unknown it is taken as the created date — so their cycle time runs longer than the work really took.`,
    withoutImported: 'Count without imported cards',
    importedMark: 'imported',
    importedHollow: 'Hollow ones were imported from another system.',
}
