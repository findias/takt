import { useCallback, useEffect, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { BoardView } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { CloseIcon } from '../../shared/ui/icons.tsx'
import { IconButton } from '../../shared/ui/Button.tsx'

/**
 * Сохранённые виды.
 *
 * Вид — это сохранённая ссылка: строка запроса, в которой уже лежат
 * фильтры и группировка. Поэтому «сохранить» здесь буквально означает
 * «запомнить адрес», а «открыть» — перейти по нему.
 *
 * Свои у каждого: список чужих сохранённых фильтров рассказывает, кто
 * чем занят, и показывать его незачем даже внутри одной организации.
 */
export function Views({
  boardId,
  query,
  onOpen,
}: {
  boardId: string
  /** Текущая строка запроса — её и предлагается сохранить. */
  query: string
  onOpen: (query: string) => void
}) {
  const [views, setViews] = useState<BoardView[]>([])
  const [naming, setNaming] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .listViews(boardId)
      .then((r) => setViews(r.views))
      // Молчим намеренно: доска работает и без списка видов.
      .catch(() => setViews([]))
  }, [boardId])

  useEffect(load, [load])

  const save = () => {
    if (!name.trim()) return
    setError(null)
    api
      .saveView(boardId, name.trim(), query)
      .then(() => {
        setName('')
        setNaming(false)
        load()
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось сохранить'))
  }

  return (
    <div className="views row row--tight">
      {views.map((view) => (
        <span key={view.id} className="view">
          <button className="btn btn--quiet view-open" onClick={() => onOpen(view.query)}>
            {view.name}
          </button>
          <IconButton
            label={`Забыть вид «${view.name}»`}
            onClick={() => void api.deleteView(view.id).then(load).catch(() => load())}
          >
            <CloseIcon />
          </IconButton>
        </span>
      ))}

      {naming ? (
        <form
          className="row row--tight"
          onSubmit={(e) => {
            e.preventDefault()
            save()
          }}
        >
          <input
            autoFocus
            value={name}
            placeholder="Название вида"
            aria-label="Название вида"
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === 'Escape' && setNaming(false)}
          />
          <Button kind="primary" type="submit" disabled={!name.trim()}>
            Сохранить
          </Button>
          <Button kind="quiet" onClick={() => setNaming(false)}>
            Отмена
          </Button>
        </form>
      ) : (
        // Предлагаем сохранить только то, что настроено: кнопка «сохранить
        // вид» при пустом фильтре сохраняла бы вид «доска как есть».
        query !== '' && (
          <Button kind="quiet" onClick={() => setNaming(true)}>
            Сохранить вид
          </Button>
        )
      )}
      {error && <span className="error small">{error}</span>}
    </div>
  )
}
