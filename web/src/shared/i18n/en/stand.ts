// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { stand as ru } from '../ru/stand.ts'

export const stand: typeof ru = {
    bar: (branch: string, version: string) => `Test stand: branch ${branch}, ${version}`,
    open: 'What is on the stand',
    title: 'What is on the stand',
    signIn: (email: string, password: string) =>
      `Sign in: ${email}, password ${password}. boris@, vera@ and gleb@ sign in the same way, with the same password.`,
    empty: 'The branch has no commits on top of master: the stand shows the same as the demo.',
    since: (base: string, n: number) =>
      base === 'master'
        ? `On top of master: ${n} ${n === 1 ? 'commit' : 'commits'}, newest first.`
        : `Done since release ${base}: ${n} ${n === 1 ? 'commit' : 'commits'}, newest first.`,
    emptySince: (base: string) => `No commits since release ${base}.`,
    onlyRussian: 'This commit predates English messages — it is shown in Russian.',
    check: 'How to check',
    noCheck: 'How to check — not written',
    noTranslation: 'The commit has no translation — the English text is shown.',
    commit: (hash: string) => `Commit ${hash} on GitHub`,
  }
