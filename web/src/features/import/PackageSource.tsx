import { useEffect, useRef, useState } from 'react'
import type { BoardInfo, PersonChoice, ImportReport as Report } from '../../shared/api/index.ts'
import { t, locale } from '../../shared/i18n/index.ts'
import { useHelpTopic } from '../../shared/lib/help.ts'
import { FormError } from '../../shared/ui/Field.tsx'
import { importApi } from './api.ts'
import type { PackageSummary } from './api.ts'
import { toBase64 } from './file.ts'
import { Done, Preview, hasWork } from './Preview.tsx'
import { TargetPicker, boardIdOf } from './Target.tsx'
import type { Target } from './Target.tsx'

const MAX_PACKAGE = 50 << 20

/**
 * Пакет переноса (docs/import-package.md): файл, собранный выгрузчиком
 * за периметром и принесённый в закрытый контур.
 *
 * Досок в пакете может быть несколько, а переносится одна за раз: у
 * каждой свой предпросмотр, своё «куда» и свои колонки, и одна кнопка
 * на всё сразу спрятала бы решение по каждой. Перенесли — «Перенести
 * следующую доску пакета» возвращает к тому же файлу.
 */
export function PackageSource({ boards }: { boards: BoardInfo[] }) {
  const [file, setFile] = useState<string | null>(null)
  const [summary, setSummary] = useState<PackageSummary | null>(null)
  const [board, setBoard] = useState(1)
  const [target, setTarget] = useState<Target>({ kind: 'new', name: '' })
  const [columnMap, setColumnMap] = useState<Record<string, string>>({})
  // Выбор по людям источника (ROADMAP 23.6): ключ человека → что с ним делать.
  const [people, setPeople] = useState<Record<string, PersonChoice>>({})
  // Выбор по колонкам и людям — про доску и её людей: сменилась доска
  // или файл — прежний выбор ни к чему.
  const resetChoices = () => {
    setColumnMap({})
    setPeople({})
  }
  const [report, setReport] = useState<Report | null>(null)
  const [busy, setBusy] = useState<'reading' | 'checking' | 'applying' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<Report | null>(null)
  const asked = useRef(0)
  // F1 отсюда — про пакет.
  useHelpTopic('importPackage')

  const boardId = boardIdOf(target)
  const boardName = target.kind === 'new' ? target.name.trim() : ''
  const request = (apply: boolean, name: string) =>
    importApi.importPackage({
      file: file ?? '',
      board,
      boardId: boardId || undefined,
      newBoardName: boardId ? undefined : name,
      columns: boardId ? columnMap : undefined,
      people,
      apply,
    })

  useEffect(() => {
    if (!file || (target.kind === 'existing' && !boardId)) return
    const n = ++asked.current
    setBusy('checking')
    // `request` собирается из тех же значений, что стоят в зависимостях;
    // название доски на предпросмотр не влияет, кроме подписи.
    request(false, 'preview')
      .then((r) => {
        if (n !== asked.current) return
        setSummary(r.package)
        setReport(r.report)
        setError(null)
        // Название новой доски — как в пакете, пока человек его не менял.
        setTarget((was) => (was.kind === 'new' && was.name === '' ? { kind: 'new', name: r.package.boards[board - 1]?.title ?? '' } : was))
      })
      .catch((e) => {
        if (n !== asked.current) return
        setReport(null)
        setError(e instanceof Error ? e.message : t.imports.readFailed)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
  }, [file, board, boardId, target.kind, columnMap, people])

  const choose = (picked: File | undefined) => {
    setDone(null)
    setSummary(null)
    setReport(null)
    setBoard(1)
    resetChoices()
    setTarget({ kind: 'new', name: '' })
    setError(null)
    if (!picked) {
      setFile(null)
      return
    }
    if (picked.size > MAX_PACKAGE) {
      setFile(null)
      setError(t.imports.packageTooBig)
      return
    }
    setBusy('reading')
    picked
      .arrayBuffer()
      .then((buf) => setFile(toBase64(buf)))
      .catch(() => {
        setBusy(null)
        setError(t.imports.readFailed)
      })
  }

  const canApply =
    !!report && hasWork(report) && busy === null && (target.kind === 'existing' || boardName !== '')
  const apply = () => {
    if (!canApply) return
    const n = ++asked.current
    setBusy('applying')
    request(true, boardName)
      .then((r) => {
        if (n === asked.current) setDone(r.report)
      })
      .catch((e) => {
        if (n === asked.current) setError(e instanceof Error ? e.message : t.imports.failed)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
  }

  if (done)
    return (
      <Done
        report={done}
        anotherLabel={
          summary && board < summary.boards.length ? t.imports.nextPackageBoard : t.imports.another
        }
        onAnother={() => {
          // Тот же пакет, следующая доска — переносят их обычно подряд.
          setDone(null)
          setReport(null)
          resetChoices()
          setTarget({ kind: 'new', name: '' })
          if (summary && board < summary.boards.length) setBoard(board + 1)
          else choose(undefined)
        }}
      />
    )

  return (
    <>
      <p className="small muted">{t.imports.packageIntro}</p>
      <label className="import-file">
        <span>{t.imports.packageFile}</span>
        <input type="file" accept=".takt" disabled={busy === 'applying'} onChange={(e) => choose(e.target.files?.[0])} />
      </label>
      {busy === 'reading' && <p className="muted small">{t.imports.reading}</p>}

      {summary && (
        <p className="small">
          {t.imports.packageFrom(
            summary.source,
            summary.account ?? '',
            summary.createdBy,
            new Date(summary.createdAt).toLocaleDateString(locale()),
          )}
          {summary.collectedBy && <span className="muted"> — {summary.collectedBy}</span>}
        </p>
      )}

      {summary && summary.boards.length > 1 && (
        <label className="import-file">
          <span>{t.imports.packageBoard}</span>
          <select
            value={board}
            disabled={busy === 'applying'}
            onChange={(e) => {
              resetChoices()
              setTarget((was) => (was.kind === 'new' ? { kind: 'new', name: '' } : was))
              setBoard(Number(e.target.value))
            }}
          >
            {summary.boards.map((b, i) => (
              <option key={i} value={i + 1}>
                {t.imports.packageBoardOf(b.title, b.cards)}
              </option>
            ))}
          </select>
        </label>
      )}

      {summary && (
        <>
          <TargetPicker
            target={target}
            boards={boards}
            fallbackName={summary.boards[board - 1]?.title ?? ''}
            disabled={busy === 'applying'}
            onChange={setTarget}
            onBoardChange={resetChoices}
          />
          <Preview
            report={report}
            boardName={boardName}
            checking={busy === 'checking'}
            applying={busy === 'applying'}
            canApply={canApply}
            columnMap={columnMap}
            onColumnMap={setColumnMap}
            people={people}
            onPeople={setPeople}
            onApply={apply}
          />
        </>
      )}
      <FormError>{error}</FormError>
    </>
  )
}
