import { useCallback, useEffect, useState } from 'react'
import { availabilityAPI } from '../../shared/api/availability.ts'
import type { Availability } from '../../shared/api/availability.ts'

// Отпуск меняется и без операций доски. Перечитываем при возврате
// в окно и раз в минуту, в том числе при наступлении следующего дня.
export function useAvailability(board?: string, boardVersion?: number) {
  const [data, setData] = useState<Availability | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [revision, setRevision] = useState(0)
  const refresh = useCallback(() => setRevision((v) => v + 1), [])
  useEffect(() => {
    let current = true
    availabilityAPI.load(board).then((next) => { if (current) { setData(next); setError(null) } })
      .catch((e: unknown) => { if (current) setError(e instanceof Error ? e.message : String(e)) })
    return () => { current = false }
  }, [board, boardVersion, revision])
  useEffect(() => {
    const timer = setInterval(refresh, 60_000)
    window.addEventListener('focus', refresh)
    return () => { clearInterval(timer); window.removeEventListener('focus', refresh) }
  }, [refresh])
  return { data, error, refresh }
}
