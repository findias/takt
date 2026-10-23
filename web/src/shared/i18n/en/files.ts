// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { files as ru } from '../ru/files.ts'

export const files: typeof ru = {
    title: 'Files',
    loading: 'Loading files…',
    readFailed: 'Could not read the file list',
    none: 'No files yet.',
    attach: 'Attach a file',
    hint: (file: string, card: string) => `Up to ${file} per file and ${card} per card. Images, PDF, text, office documents.`,
    tooBig: (limit: string) => `The file is over ${limit} — the limit for one attachment.`,
    delete: 'Remove',
    deleteOf: (name: string) => `Remove file ${name}`,
    deleteTitle: 'Remove the file?',
    deleteBody: (name: string) => `“${name}” disappears from the card; the history keeps a record of it.`,
    mb: (n: string) => `${n} MB`,
    kb: (n: string) => `${n} KB`,
    b: (n: string) => `${n} B`,
}
