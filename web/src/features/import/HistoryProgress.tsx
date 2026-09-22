import { useEffect, useState } from 'react'
import { t } from '../../shared/i18n/index.ts'
import { importApi } from './api.ts'

type Job = Awaited<ReturnType<typeof importApi.yougileHistory>>

/**
 * Ход дотягивания истории и обсуждения YouGile после переноса
 * (ROADMAP 23.7). Спрашивает раз в три секунды, пока задание идёт:
 * полчаса на доску в 800 задач — это надолго, и человек должен видеть,
 * что работа идёт, а не гадать.
 */
export function HistoryProgress({ boardId }: { boardId: string }) {
  const s = t.imports
  const [job, setJob] = useState<Job | null>(null)

  useEffect(() => {
    let alive = true
    let timer = 0
    const ask = () => {
      importApi
        .yougileHistory(boardId)
        .then((j) => {
          if (!alive) return
          setJob(j)
          if (!j.finished) timer = window.setTimeout(ask, 3000)
        })
        // Задания нет — сервер перезапускали или оно ещё не записалось:
        // спросим ещё раз, но реже.
        .catch(() => alive && (timer = window.setTimeout(ask, 10000)))
    }
    ask()
    return () => {
      alive = false
      window.clearTimeout(timer)
    }
  }, [boardId])

  return (
    <div className="note stack stack--tight" role="status">
      {job?.failed ? (
        <p className="small">{s.historyFailed(job.failed)}</p>
      ) : job?.finished ? (
        <p className="small">{s.historyDone(job.comments, job.history)}</p>
      ) : (
        <>
          <p className="small">{s.historyProgress(job?.done ?? 0, job?.total ?? 0)}</p>
          {job && job.total > 0 && <progress max={job.total} value={job.done} />}
          <p className="muted small">{s.historyAway}</p>
        </>
      )}
    </div>
  )
}
