import { createHash } from 'node:crypto'
import { zip } from './xlsx.ts'

/**
 * Пакет переноса (docs/import-package.md) для сквозных сценариев —
 * собранный по спецификации здесь же, а не выгрузчиком: так проверяется,
 * что формат хватает прочитать по одному описанию.
 */
export function taktPackage(boards: object[], source = 'yougile'): Buffer {
  const files: [string, Buffer][] = boards.map((b, i) => [`boards/${i + 1}.json`, Buffer.from(JSON.stringify(b))])
  const manifest = {
    format: 'takt-import-package',
    version: 1,
    createdAt: '2026-09-22T10:00:00Z',
    createdBy: 'takt-fetch e2e',
    collectedBy: 'сквозной сценарий',
    source: { system: source, account: 'Северная логистика' },
    boards: files.map(([file, data], i) => ({
      file,
      sha256: createHash('sha256').update(data).digest('hex'),
      title: (boards[i] as { title: string }).title,
      cards: ((boards[i] as { cards: unknown[] }).cards ?? []).length,
    })),
    lost: ['файлы вложений'],
  }
  return zip([['manifest.json', Buffer.from(JSON.stringify(manifest))], ...files])
}
