import { useEffect, useRef, useState } from 'react'
import type { BoardInfo, ImportReport as Report } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { importApi } from './api.ts'
import { useHelpTopic } from '../../shared/lib/help.ts'
import { FormError } from '../../shared/ui/Field.tsx'
import { Done, Preview } from './Preview.tsx'
import { TargetPicker, boardIdOf } from './Target.tsx'
import { YougileLogin } from './YougileLogin.tsx'
import type { Target } from './Target.tsx'

type YgBoard = { id: string; title: string; project: string }

/**
 * Перенос из YouGile по его API (ROADMAP 23.3): вход → компания →
 * ключ → доска YouGile → куда → предпросмотр → перенос.
 *
 * Ключ живёт только здесь, в памяти страницы; пароль — ещё короче,
 * во входе (`YougileLogin`). Закрыли экран — забыто всё: хранить чужие
 * учётные данные ради разового переезда незачем.
 */
export function YougileSource({ boards }: { boards: BoardInfo[] }) {
  const [key, setKey] = useState('')
  const [keyCreated, setKeyCreated] = useState(false)
  const [ygBoards, setYgBoards] = useState<YgBoard[] | null>(null)
  const [ygBoard, setYgBoard] = useState('')
  const [target, setTarget] = useState<Target>({ kind: 'new', name: '' })
  const [columnMap, setColumnMap] = useState<Record<string, string>>({})
  const [report, setReport] = useState<Report | null>(null)
  const [busy, setBusy] = useState<'asking' | 'checking' | 'applying' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<Report | null>(null)
  const asked = useRef(0)
  // F1 отсюда — про YouGile, а не про таблицу.
  useHelpTopic('importYougile')

  // Отказ YouGile — словами сервера: он различает «не пустили», «нет
  // сети» и «подождите» и говорит, что делать в каждом случае.
  const fail = (e: unknown) => setError(e instanceof Error ? e.message : t.imports.failed)
  const ask = <T,>(call: Promise<T>, then: (v: T) => void) => {
    setBusy('asking')
    setError(null)
    call
      .then(then)
      .catch(fail)
      .finally(() => setBusy(null))
  }

  // Доски — как только есть ключ.
  useEffect(() => {
    if (!key) return
    ask(importApi.yougileBoards(key), (r) => {
      setYgBoards(r.boards)
      setYgBoard('')
    })
  }, [key])

  const board = ygBoards?.find((b) => b.id === ygBoard) ?? null
  const boardId = boardIdOf(target)
  const boardName = target.kind === 'new' ? target.name.trim() : ''
  const request = (apply: boolean, name: string) =>
    importApi.yougileImport({
      key,
      board: ygBoard,
      boardId: boardId || undefined,
      newBoardName: boardId ? undefined : name,
      columns: boardId ? columnMap : undefined,
      apply,
    })

  useEffect(() => {
    if (!key || !board || (target.kind === 'existing' && !boardId)) return
    const n = ++asked.current
    setBusy('checking')
    // `request` собирается из тех же значений, что стоят в зависимостях.
    request(false, board.title)
      .then((r) => {
        if (n !== asked.current) return
        setReport(r.report)
        setError(null)
      })
      .catch((e) => {
        if (n !== asked.current) return
        setReport(null)
        fail(e)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
  }, [key, board, boardId, target.kind, columnMap])

  const canApply =
    !!report && report.created > 0 && busy === null && (target.kind === 'existing' || boardName !== '')
  const apply = () => {
    if (!canApply) return
    const n = ++asked.current
    setBusy('applying')
    request(true, boardName)
      .then((r) => {
        if (n === asked.current) setDone(r.report)
      })
      .catch((e) => {
        if (n === asked.current) fail(e)
      })
      .finally(() => {
        if (n === asked.current) setBusy(null)
      })
  }

  const forget = () => {
    setKey('')
    setKeyCreated(false)
    setYgBoards(null)
    setYgBoard('')
    setReport(null)
    setColumnMap({})
  }

  if (done)
    return (
      <Done
        report={done}
        onAnother={() => {
          setDone(null)
          setYgBoard('')
          setReport(null)
          setColumnMap({})
        }}
      />
    )

  return (
    <>
      <p className="small muted">{t.imports.ygIntro}</p>

      {!key && (
        <YougileLogin
          onKey={(k, created) => {
            setKeyCreated(created)
            setKey(k)
          }}
        />
      )}

      {key && (
        <>
          {keyCreated && <p className="small">{t.imports.ygKeyCreated}</p>}
          <p>
            <button className="link" onClick={forget}>
              {t.imports.ygChangeKey}
            </button>
          </p>
          {busy === 'asking' && <p className="muted small">{t.imports.ygLoading}</p>}
          {ygBoards && (
            <label className="import-file">
              <span>{t.imports.ygBoard}</span>
              <select
                value={ygBoard}
                disabled={busy === 'applying'}
                onChange={(e) => {
                  setYgBoard(e.target.value)
                  setColumnMap({})
                  const picked = ygBoards.find((b) => b.id === e.target.value)
                  setTarget((was) => (was.kind === 'new' ? { kind: 'new', name: picked?.title ?? '' } : was))
                }}
              >
                <option value="" disabled>
                  —
                </option>
                {ygBoards.map((b) => (
                  <option key={b.id} value={b.id}>
                    {t.imports.ygBoardOf(b.project, b.title)}
                  </option>
                ))}
              </select>
            </label>
          )}
        </>
      )}

      {key && board && (
        <>
          <TargetPicker
            target={target}
            boards={boards}
            fallbackName={board.title}
            disabled={busy === 'applying'}
            onChange={setTarget}
            onBoardChange={() => setColumnMap({})}
          />
          <Preview
            report={report}
            boardName={boardName}
            checking={busy === 'checking'}
            applying={busy === 'applying'}
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
