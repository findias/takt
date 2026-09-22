import { useEffect, useRef, useState } from 'react'
import type { BoardInfo, ImportAnswer, ImportField, ImportReport as Report } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { importApi } from './api.ts'
import { FormError } from '../../shared/ui/Field.tsx'
import { Mapping } from './Mapping.tsx'
import { Done, Preview } from './Preview.tsx'
import { TargetPicker, boardIdOf } from './Target.tsx'
import type { Target } from './Target.tsx'

const MAX_FILE = 5 << 20

/**
 * Перенос из таблицы (ROADMAP 23.1–23.2): файл → сопоставление →
 * предпросмотр → перенос.
 *
 * Предпросмотр — не отдельный шаг с кнопкой «Проверить», а то, что
 * показывается всё время: выбрали файл, поправили колонку, выбрали
 * доску — отчёт уже пересчитан. Кнопка одна, и она переносит; импорт
 * без предпросмотра — необратимое без вопроса, а у нас правило обратное.
 *
 * Файл не хранится на сервере между шагами: он едет с каждым запросом.
 * Таблица в пять мегабайт — не повод заводить хранилище черновиков,
 * а черновик, брошенный на полпути, некому было бы убрать.
 */
export function TableSource({ boards }: { boards: BoardInfo[] }) {
  const [file, setFile] = useState<{ name: string; data: string } | null>(null)
  const [target, setTarget] = useState<Target>({ kind: 'new', name: '' })
  const [mapping, setMapping] = useState<ImportField[] | null>(null)
  // Пусто — первый лист книги; у CSV листов нет вовсе.
  const [sheet, setSheet] = useState('')
  // Куда ложатся значения колонки файла — только на существующей доске.
  const [columnMap, setColumnMap] = useState<Record<string, string>>({})
  const [answer, setAnswer] = useState<ImportAnswer | null>(null)
  const [busy, setBusy] = useState<'reading' | 'checking' | 'applying' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<Report | null>(null)
  // Ответ на устаревший запрос не должен лечь поверх свежего: человек
  // успел поменять колонку, пока сервер считал прежнюю.
  const asked = useRef(0)

  const boardId = boardIdOf(target)
  const request = (apply: boolean, boardName: string) =>
    importApi.importTable({
      file: file?.data ?? '',
      mapping,
      sheet: sheet || undefined,
      columns: boardId ? columnMap : undefined,
      boardId: boardId || undefined,
      newBoardName: boardId ? undefined : boardName,
      apply,
    })

  useEffect(() => {
    if (!file || (target.kind === 'existing' && !boardId)) return
    const n = ++asked.current
    setBusy('checking')
    // Название доски на предпросмотр не влияет, кроме подписи, —
    // поэтому и не перезапускает его: подпись берётся из поля.
    request(false, file.name)
      .then((a) => {
        if (n !== asked.current) return
        // Предложенное сервером сопоставление в состояние не кладётся:
        // пока человек ничего не правил, `null` и есть «как предложит
        // сервер», а записанное оно перезапустило бы предпросмотр впустую.
        setAnswer(a)
        setError(null)
      })
      .catch((e) => {
        if (n !== asked.current) return
        setAnswer(null)
        setError(e instanceof Error ? e.message : t.imports.readFailed)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
    // `request` собирается из тех же значений, что стоят в зависимостях.
  }, [file, mapping, sheet, boardId, target.kind, columnMap])

  const choose = (picked: File | undefined) => {
    setDone(null)
    setAnswer(null)
    setMapping(null)
    setSheet('')
    setColumnMap({})
    setError(null)
    if (!picked) {
      setFile(null)
      return
    }
    if (picked.size > MAX_FILE) {
      setFile(null)
      setError(t.imports.tooBig)
      return
    }
    setBusy('reading')
    picked
      .arrayBuffer()
      .then((buf) => {
        const name = picked.name.replace(/\.[^.]+$/, '')
        setFile({ name, data: toBase64(buf) })
        setTarget((was) => (was.kind === 'new' ? { kind: 'new', name } : was))
      })
      .catch(() => {
        setBusy(null)
        setError(t.imports.readFailed)
      })
  }

  const report = answer?.report ?? null
  const boardName = target.kind === 'new' ? target.name.trim() : ''
  const canApply =
    !!file && !!report && report.created > 0 && busy === null && (target.kind === 'existing' || boardName !== '')

  const apply = () => {
    if (!canApply) return
    const n = ++asked.current
    setBusy('applying')
    request(true, boardName)
      .then((a) => {
        if (n !== asked.current) return
        setDone(a.report)
        setError(null)
      })
      .catch((e) => {
        if (n !== asked.current) return
        setError(e instanceof Error ? e.message : t.imports.failed)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
  }

  if (done) return <Done report={done} onAnother={() => choose(undefined)} />

  return (
    <>
      <p className="small">
        <a className="link link--alone" href="/api/import/table/sample" download>
          {t.imports.sample}
        </a>
        <span className="muted"> — {t.imports.sampleHint}</span>
      </p>

      <label className="import-file">
        <span>{t.imports.file}</span>
        <input
          type="file"
          accept=".csv,text/csv,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
          disabled={busy === 'applying'}
          onChange={(e) => choose(e.target.files?.[0])}
        />
      </label>
      {busy === 'reading' && <p className="muted small">{t.imports.reading}</p>}

      {answer && answer.sheets.length > 1 && (
        <label className="import-file">
          <span>{t.imports.sheet}</span>
          <select
            value={sheet || answer.sheet || answer.sheets[0]}
            disabled={busy === 'applying'}
            onChange={(e) => {
              // Другой лист — другие колонки: прежнее сопоставление
              // к ним не относится, сервер предложит новое.
              setMapping(null)
              setColumnMap({})
              setSheet(e.target.value)
            }}
          >
            {answer.sheets.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
      )}

      {answer && (
        <>
          <TargetPicker
            target={target}
            boards={boards}
            fallbackName={file?.name ?? ''}
            disabled={busy === 'applying'}
            onChange={setTarget}
            onBoardChange={() => setColumnMap({})}
          />

          {answer.headers.length > 0 && (
            <div className="stack stack--tight">
              <h3 className="section-title">{t.imports.mapping}</h3>
              <p className="muted small">{t.imports.mappingHint}</p>
              <Mapping
                headers={answer.headers}
                sample={answer.sample}
                mapping={mapping ?? answer.mapping}
                onChange={setMapping}
                disabled={busy === 'applying'}
              />
            </div>
          )}

          <Preview
            report={report}
            boardName={boardName}
            checking={busy === 'checking'}
            applying={busy === 'applying'}
            problem={answer.mappingError}
            canApply={canApply}
            columnMap={columnMap}
            onColumnMap={setColumnMap}
            onApply={apply}
          />
        </>
      )}
      <FormError>{error}</FormError>
    </>
  )
}

/** base64 без data:-приставки. Кусками: `String.fromCharCode(...всё)`
 *  на мегабайтах переполняет стек вызова. */
function toBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let out = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    out += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(out)
}
