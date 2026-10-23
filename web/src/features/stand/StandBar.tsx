import { Suspense, lazy, useEffect, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { StandNote } from '../../shared/api/index.ts'
import { loadSections, t } from '../../shared/i18n/index.ts'

// Диалог «Что на стенде» — своим куском: открывают его по нажатию и только
// на нашем стенде, а у заказчика он не нужен никогда, при этом лежал
// в первой загрузке, которая стоит у порога (web/e2e/perf.spec.ts).
const StandDialog = lazy(() => import('./StandDialog.tsx').then((m) => ({ default: m.StandDialog })))

/**
 * Полоса тестового стенда ветки (ROADMAP 30.7): на каждом экране,
 * включая вход, — чтобы снимок со стенда не спутали с продуктом,
 * а пароль был под рукой там, где он нужен.
 *
 * Не стенд — ручки на сервере нет, ответ 404, и полосы нет вовсе:
 * на установке заказчика этот компонент ничего не рисует.
 */
export function StandBar() {
  const [note, setNote] = useState<StandNote | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    // Тексты стенда — отдельным разделом: установке заказчика, где
    // стенда нет, они в первой загрузке ни к чему (порог — perf.spec.ts).
    api
      .stand()
      .then((n) => loadSections('stand').then(() => setNote(n)))
      // Отказ — обычный ответ установки, которая не стенд: молчим.
      .catch(() => setNote(null))
  }, [])

  if (!note) return null
  return (
    <div className="stand-bar">
      <span>{t.stand.bar(note.branch || '—', note.version)}</span>
      <button
        type="button"
        className="link"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        {t.stand.open}
      </button>
      {open && (
        <Suspense fallback={null}>
          <StandDialog note={note} onClose={() => setOpen(false)} />
        </Suspense>
      )}
    </div>
  )
}

