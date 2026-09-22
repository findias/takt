// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { passwordLink as ru } from '../ru/passwordLink.ts'

export const passwordLink: typeof ru = {
    opening: 'Opening the link…',
    failed: 'The link did not open',
    title: (org: string) => `Password for “${org}”`,
    forWhom: (name: string, email: string) => `${name}, you will sign in as ${email}.`,
    until: (when: string) => `The link works once and is valid until ${when}.`,
    password: 'Choose a password',
    submit: 'Set password and sign in',
    toHome: 'Home',
    issue: 'Sign-in link',
    issueOf: (name: string) => `Issue a sign-in link: ${name}`,
    issued: (name: string) =>
      `${name}: a sign-in link has been created. It is shown once and works once; the previous link no longer works. Pass it on yourself — takt sends no emails.`,
    link: 'Sign-in link',
    linkWhat: 'the sign-in link',
    awaiting: 'not signed in yet',
    awaitingHint: 'The account was created for this person and they have not set a password yet. Issue them a sign-in link.',
}
