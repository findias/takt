import type { hooks as ru } from '../ru/hooks.ts'

export const hooks: typeof ru = {
    aboutMinutes: (n: number) => `about ${n} minutes`,
    aboutHours: (n: number) => `about ${n} ${n === 1 ? 'hour' : 'hours'}`,
    title: 'Event subscriptions',
    intro:
      'A subscription carries events outside: we send them to your address and sign them with a key, so the receiver can check they come from us. Delivery is at least once.',
    policy: (p: {
      timeout: number
      attempts: number
      first: number
      maxMinutes: number
      total: string
      keepDays: number
    }) =>
      `What we do on our own: wait ${p.timeout} s for an answer; if the receiver does not answer, retry up to ${p.attempts} times, doubling the pause from ${p.first} s to ${p.maxMinutes} min. That is ${p.total} in total. After that we stop: the subscription is switched off and its events stop piling up altogether. Any manual retry of a delivery switches it back on. We keep the delivery log for ${p.keepDays} days and then clean it up ourselves.`,
    created:
      'Subscription created. The signing key is shown once — put it where the receiver checks the signature header.',
    secret: 'Signing key',
    secretWhat: 'the signing key',
    checkHow: 'It is checked like this:',
    checkIs: 'is',
    checkHmac: 'followed by HMAC-SHA256 of the string “',
    checkBody: '.request body”, keyed with this string. In full — in the',
    contract: 'contract description',
    createFailed: 'Could not create the subscription',
    name: 'Subscription name',
    namePlaceholder: 'What the subscription is for',
    url: 'Receiver address',
    create: 'Create subscription',
    createShort: 'Create',
    disabled: 'Switched off: the receiver was not answering.',
    disabledHint: 'A retry of any delivery switches the subscription back on — so does “Resume”.',
    paused: 'Paused',
    pausedHint:
      'While paused, events for this subscription do not pile up: resuming will not turn into an avalanche for the whole downtime.',
    queued: (n: number) => `Queued: ${n}. `,
    lastTry: (when: string) => `Last attempt ${when}`,
    answered: (status: number) => `, answer ${status}.`,
    deliveries: 'Deliveries',
    hideDeliveries: 'Hide deliveries',
    resumeOf: (name: string) => `Resume subscription “${name}”`,
    pauseOf: (name: string) => `Pause subscription “${name}”`,
    resume: 'Resume',
    pause: 'Pause',
    deleteOf: (name: string) => `Delete subscription “${name}”`,
    delete: 'Delete',
    noDeliveries: 'No deliveries yet: none of the events it subscribes to have happened.',
    attempts: (n: number) => ` · attempts: ${n}`,
    status: (n: number) => ` · answer ${n}`,
    retryFailed: 'Could not retry',
    retry: 'Retry',
    delivered: 'delivered',
    gaveUp: 'not delivered, attempts exhausted',
    nextTry: (when: string) => `next attempt ${when}`,
    inQueue: 'queued',
}
