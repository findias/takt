import { useEffect, useState } from 'react'
import { api } from '../shared/api/index.ts'
import type { BoardInfo } from '../shared/api/index.ts'
import { t } from '../shared/i18n/index.ts'
import { TableSource } from '../features/import/TableSource.tsx'
import { YougileSource } from '../features/import/YougileSource.tsx'
import { PackageSource } from '../features/import/PackageSource.tsx'

type Source = 'file' | 'yougile' | 'package'

const SOURCES: Source[] = ['file', 'yougile', 'package']

/**
 * Перенос (ROADMAP, этап 23): откуда — таблица или YouGile, дальше
 * одинаково: куда, что получится, одна кнопка.
 *
 * Источники не делят состояние: переключились — начали заново. Файл
 * и ключ YouGile — разные вещи, и держать недоделанное в соседней
 * вкладке значило бы однажды перенести не то.
 */
export function ImportScreen() {
  const [source, setSource] = useState<Source>('file')
  const [boards, setBoards] = useState<BoardInfo[]>([])

  useEffect(() => {
    api
      .listBoards()
      .then((r) => setBoards(r.boards.filter((b) => b.writable)))
      .catch(() => setBoards([]))
  }, [])

  return (
    <div className="stack import-screen">
      <h2>{t.app.import}</h2>
      <p>{t.imports.intro}</p>
      <fieldset className="account-group import-source">
        <legend>{t.imports.source}</legend>
        {SOURCES.map((s) => (
          <label key={s}>
            <input type="radio" name="import-source" checked={source === s} onChange={() => setSource(s)} />
            {s === 'file' ? t.imports.fromFile : s === 'yougile' ? t.imports.fromYougile : t.imports.fromPackage}
          </label>
        ))}
      </fieldset>
      {source === 'file' && <TableSource boards={boards} />}
      {source === 'yougile' && <YougileSource boards={boards} />}
      {source === 'package' && <PackageSource boards={boards} />}
    </div>
  )
}
