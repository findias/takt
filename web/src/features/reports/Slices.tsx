import { useCallback, useEffect, useId, useState } from 'react'
import { ApiError, request } from '../../shared/api/index.ts'
import { Button, IconButton } from '../../shared/ui/Button.tsx'
import { CloseIcon } from '../../shared/ui/icons.tsx'
import { useToast } from '../../shared/ui/Toast.tsx'
import { t } from '../../shared/i18n/index.ts'

export type Slice = { id: string; name: string; query: string }

// Вызовы — здесь, а не в общем клиенте: экран грузится отдельным куском.
const listSlices = () => request<{ slices: Slice[] }>('GET', '/api/reports/slices')
const saveSlice = (name: string, query: string) =>
  request<Slice>('POST', '/api/reports/slices', { name, query })
const deleteSlice = (id: string) => request<void>('DELETE', `/api/reports/slices/${id}`)

/**
 * Именованные срезы (этап 24.2): отбор под именем, открывается одним
 * нажатием. Без них каждый отчёт собирается заново, и два отчёта
 * за разные месяцы оказываются посчитаны по-разному.
 *
 * Срез — сохранённая строка адреса, как вид доски: «сохранить» —
 * запомнить адрес, «открыть» — перейти по нему. Готовый период в нём
 * словом, поэтому «прошлый квартал» всегда прошлый. Свои у каждого:
 * поделиться — ссылкой.
 *
 * Убранный срез возвращается из тоста: вернуть его — значит сохранить
 * то же имя и тот же отбор, и спрашивать перед удалением незачем.
 */
export function Slices({ query, onOpen }: { query: string; onOpen: (query: string) => void }) {
  const s = t.reports
  const toast = useToast()
  const [slices, setSlices] = useState<Slice[]>([])
  const [naming, setNaming] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const fieldId = useId()

  const load = useCallback(() => {
    listSlices()
      .then((r) => setSlices(r.slices))
      // Молча: выгрузка работает и без списка срезов.
      .catch(() => setSlices([]))
  }, [])
  useEffect(load, [load])

  const save = () => {
    if (!name.trim()) return
    setBusy(true)
    setError('')
    saveSlice(name.trim(), query)
      .then(() => {
        setName('')
        setNaming(false)
        load()
      })
      .catch((e) => setError(e instanceof Error ? e.message : s.sliceSaveFailed))
      .finally(() => setBusy(false))
  }

  const remove = (slice: Slice) => {
    setSlices((list) => list.filter((x) => x.id !== slice.id))
    deleteSlice(slice.id)
      .then(() =>
        toast({
          text: s.sliceRemoved(slice.name),
          tone: 'info',
          action: {
            label: s.sliceRestore,
            onAct: () => void saveSlice(slice.name, slice.query).then(load, load),
          },
        }),
      )
      .catch((e) => {
        load()
        toast({ text: e instanceof ApiError ? e.message : s.sliceRemoveFailed, tone: 'warning' })
      })
  }

  return (
    <div className="row row--tight report-slices" role="group" aria-label={s.slices}>
      <span className="small report-name">{s.slices}</span>
      {slices.length === 0 && !naming && <span className="muted small">{s.slicesEmpty}</span>}
      {slices.map((slice) => (
        <span key={slice.id} className="view">
          <button
            type="button"
            className="btn btn--quiet view-open"
            aria-current={slice.query === query ? 'true' : undefined}
            onClick={() => onOpen(slice.query)}
          >
            {slice.name}
          </button>
          <IconButton label={s.sliceForget(slice.name)} onClick={() => remove(slice)}>
            <CloseIcon />
          </IconButton>
        </span>
      ))}
      {naming ? (
        <form
          className="row row--tight"
          // Escape закрывает форму, где бы ни был фокус: после неудачной
          // отправки он стоит на кнопке, а не в поле.
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              setNaming(false)
              setError('')
            }
          }}
          onSubmit={(e) => {
            e.preventDefault()
            save()
          }}
        >
          <label className="sr-only" htmlFor={fieldId}>
            {s.sliceName}
          </label>
          <input
            id={fieldId}
            autoFocus
            required
            maxLength={200}
            value={name}
            aria-invalid={error ? true : undefined}
            aria-describedby={`${fieldId}-error`}
            onChange={(e) => {
              setName(e.target.value)
              setError('')
            }}
          />
          <Button kind="primary" type="submit" disabled={!name.trim()} aria-busy={busy || undefined}>
            {t.common.save}
          </Button>
          <Button kind="quiet" onClick={() => setNaming(false)}>
            {t.common.cancel}
          </Button>
          <p className="form-error" id={`${fieldId}-error`}>
            {error}
          </p>
        </form>
      ) : (
        <Button kind="quiet" onClick={() => setNaming(true)}>
          {s.sliceSave}
        </Button>
      )}
    </div>
  )
}
