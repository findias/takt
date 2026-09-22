import { useEffect, useRef, useState } from 'react'
import { api } from '../shared/api/index.ts'
import type { BoardInfo, ImportAnswer, ImportField, ImportReport as Report } from '../shared/api/index.ts'
import { t } from '../shared/i18n/index.ts'
import { boardPath, navigate } from '../shared/router/index.ts'
import { Mapping } from '../features/import/Mapping.tsx'
import { ImportReport } from '../features/import/ImportReport.tsx'
import { FormError, ScreenError } from '../shared/ui/Field.tsx'

const MAX_FILE = 5 << 20

type Target = { kind: 'new'; name: string } | { kind: 'existing'; boardId: string }

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
export function ImportScreen() {
  const [file, setFile] = useState<{ name: string; data: string } | null>(null)
  const [target, setTarget] = useState<Target>({ kind: 'new', name: '' })
  const [boards, setBoards] = useState<BoardInfo[]>([])
  const [mapping, setMapping] = useState<ImportField[] | null>(null)
  // Пусто — первый лист книги; у CSV листов нет вовсе.
  const [sheet, setSheet] = useState('')
  const [answer, setAnswer] = useState<ImportAnswer | null>(null)
  const [busy, setBusy] = useState<'reading' | 'checking' | 'applying' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<Report | null>(null)
  // Ответ на устаревший запрос не должен лечь поверх свежего: человек
  // успел поменять колонку, пока сервер считал прежнюю.
  const asked = useRef(0)

  useEffect(() => {
    api
      .listBoards()
      .then((r) => setBoards(r.boards.filter((b) => b.writable)))
      .catch(() => setBoards([]))
  }, [])

  const boardId = target.kind === 'existing' ? target.boardId : ''
  useEffect(() => {
    if (!file || (target.kind === 'existing' && !boardId)) return
    const n = ++asked.current
    setBusy('checking')
    api
      .importTable({
        file: file.data,
        mapping,
        sheet: sheet || undefined,
        boardId: boardId || undefined,
        // Название доски на предпросмотр не влияет, кроме подписи, —
        // поэтому и не перезапускает его: подпись берётся из поля.
        newBoardName: boardId ? undefined : file.name,
        apply: false,
      })
      .then((a) => {
        if (n !== asked.current) return
        setAnswer(a)
        setError(null)
        // Предложенное сервером в состояние не кладётся: пока человек
        // ничего не правил, `null` и есть «как предложит сервер», а
        // записанное оно перезапустило бы предпросмотр впустую.
      })
      .catch((e) => {
        if (n !== asked.current) return
        setAnswer(null)
        setError(e instanceof Error ? e.message : t.imports.readFailed)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
  }, [file, mapping, sheet, boardId, target.kind])

  const choose = (picked: File | undefined) => {
    setDone(null)
    setAnswer(null)
    setMapping(null)
    setSheet('')
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
    if (!file || !canApply) return
    const n = ++asked.current
    setBusy('applying')
    api
      .importTable({
        file: file.data,
        mapping,
        sheet: sheet || undefined,
        boardId: boardId || undefined,
        newBoardName: boardId ? undefined : boardName,
        apply: true,
      })
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

  if (done) {
    return (
      <div className="stack import-screen">
        <h2>{t.app.import}</h2>
        <ImportReport report={done} boardName={done.boardName} />
        <div className="form-row">
          <button className="primary" onClick={() => done.boardId && navigate(boardPath(done.boardId))}>
            {t.imports.open}
          </button>
          <button onClick={() => choose(undefined)}>{t.imports.another}</button>
        </div>
      </div>
    )
  }

  return (
    <div className="stack import-screen" aria-busy={busy !== null || undefined}>
      <h2>{t.app.import}</h2>
      <p>{t.imports.intro}</p>
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
          <fieldset className="account-group" disabled={busy === 'applying'}>
            <legend>{t.imports.where}</legend>
            <label>
              <input
                type="radio"
                name="import-target"
                checked={target.kind === 'new'}
                onChange={() => setTarget({ kind: 'new', name: file?.name ?? '' })}
              />
              {t.imports.newBoard}
            </label>
            {target.kind === 'new' && (
              <input
                className="import-indent"
                aria-label={t.imports.newBoardName}
                value={target.name}
                required
                onChange={(e) => setTarget({ kind: 'new', name: e.target.value })}
              />
            )}
            <label>
              <input
                type="radio"
                name="import-target"
                checked={target.kind === 'existing'}
                disabled={boards.length === 0}
                onChange={() => setTarget({ kind: 'existing', boardId: boards[0]?.id ?? '' })}
              />
              {t.imports.existingBoard}
              {boards.length === 0 && <span className="muted small"> — {t.imports.noWritableBoards}</span>}
            </label>
            {target.kind === 'existing' && (
              <select
                className="import-indent"
                aria-label={t.imports.board}
                value={target.boardId}
                onChange={(e) => setTarget({ kind: 'existing', boardId: e.target.value })}
              >
                {boards.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.name}
                  </option>
                ))}
              </select>
            )}
          </fieldset>

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

          <div className="stack stack--tight" aria-live="polite">
            <h3 className="section-title">{t.imports.preview}</h3>
            {busy === 'checking' && <p className="muted small">{t.imports.checking}</p>}
            <ScreenError>{answer.mappingError}</ScreenError>
            {report && <ImportReport report={report} boardName={boardName} />}
          </div>

          <button className="primary" disabled={!canApply} aria-busy={busy === 'applying' || undefined} onClick={apply}>
            {busy === 'applying'
              ? t.imports.applying
              : report && report.created > 0
                ? t.imports.apply(report.created)
                : t.imports.nothing}
          </button>
        </>
      )}
      <FormError>{error}</FormError>
    </div>
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
