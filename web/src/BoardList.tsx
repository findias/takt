import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import type { BoardInfo, Principal } from './api'

export function BoardList({
  principal,
  onOpen,
}: {
  principal: Principal
  onOpen: (id: string) => void
}) {
  const [boards, setBoards] = useState<BoardInfo[] | null>(null)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const canEdit = principal.role !== 'viewer'

  const load = useCallback(() => {
    setBoards(null)
    api
      .listBoards()
      .then((r) => setBoards(r.boards))
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось загрузить список'))
  }, [])

  // Перезагружаем при смене организации: доски у каждой свои.
  useEffect(load, [load, principal.orgId])

  return (
    <div className="stack">
      {error && <p className="error">{error}</p>}
      {boards === null && <p className="muted">Загружаем…</p>}
      {boards?.length === 0 && (
        <p className="muted">
          {canEdit ? 'Пока ни одной. Создайте первую.' : 'В этой организации пока нет досок.'}
        </p>
      )}

      <ul className="board-list">
        {boards?.map((b) => (
          <li key={b.id}>
            <button onClick={() => onOpen(b.id)}>{b.name}</button>
          </li>
        ))}
      </ul>

      {canEdit && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim()) return
            api
              .createBoard(name.trim())
              .then((b) => {
                setName('')
                load()
                onOpen(b.id)
              })
              .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось создать доску'))
          }}
        >
          <input
            value={name}
            placeholder="Название новой доски"
            onChange={(e) => setName(e.target.value)}
          />
          <button type="submit" disabled={!name.trim()}>
            Создать
          </button>
        </form>
      )}
    </div>
  )
}
