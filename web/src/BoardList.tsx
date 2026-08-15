import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import type { BoardInfo, Member, Principal, Team } from './api'
import { BoardAccess } from './BoardAccess'

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
  const [openAccess, setOpenAccess] = useState<string | null>(null)
  // Люди и подразделения нужны только настройке доступа, поэтому берутся
  // один раз на список, а не по разу на каждую доску.
  const [people, setPeople] = useState<Member[]>([])
  const [teams, setTeams] = useState<Team[]>([])
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

  useEffect(() => {
    setOpenAccess(null)
    Promise.all([api.team(), api.listTeams()])
      .then(([org, t]) => {
        setPeople(org.members)
        setTeams(t.teams)
      })
      .catch(() => {
        setPeople([])
        setTeams([])
      })
  }, [principal.orgId])

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
            <div className="row row--between">
              <button onClick={() => onOpen(b.id)}>{b.name}</button>
              <button
                className="link"
                aria-expanded={openAccess === b.id}
                onClick={() => setOpenAccess((v) => (v === b.id ? null : b.id))}
              >
                Доступ
              </button>
            </div>
            {openAccess === b.id && (
              <BoardAccess
                boardId={b.id}
                people={people}
                teams={teams}
                canEdit={canEdit}
                onClose={() => setOpenAccess(null)}
              />
            )}
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
