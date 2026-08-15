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
  const [archived, setArchived] = useState<BoardInfo[] | null>(null)
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
              <div className="row row--tight">
                <button
                  className="link"
                  aria-expanded={openAccess === b.id}
                  onClick={() => setOpenAccess((v) => (v === b.id ? null : b.id))}
                >
                  Доступ
                </button>
                {canEdit && (
                  <button
                    className="link"
                    aria-label={`Убрать доску «${b.name}» в архив`}
                    onClick={() => {
                      api
                        .archiveBoard(b.id)
                        .then(() => {
                          load()
                          setArchived(null)
                        })
                        .catch((e) =>
                          setError(e instanceof Error ? e.message : 'Не удалось убрать доску'),
                        )
                    }}
                  >
                    Убрать
                  </button>
                )}
              </div>
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

      <Archive
        boards={archived}
        canEdit={canEdit}
        onOpen={() =>
          api
            .archivedBoards()
            .then((r) => setArchived(r.boards))
            .catch(() => setArchived([]))
        }
        onRestore={(id) =>
          api
            .restoreBoard(id)
            .then(() => {
              load()
              return api.archivedBoards()
            })
            .then((r) => setArchived(r.boards))
            .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось вернуть доску'))
        }
      />

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

/**
 * Архив досок.
 *
 * Убранная доска не удаляется: карточки и журнал переходов остаются, по ним
 * считается поток. Значит, список убранного обязан существовать — иначе
 * «убрать» ничем не отличалось бы от удаления, только выглядело мягче.
 */
function Archive({
  boards,
  canEdit,
  onOpen,
  onRestore,
}: {
  boards: BoardInfo[] | null
  canEdit: boolean
  onOpen: () => void
  onRestore: (id: string) => void
}) {
  if (boards === null) {
    return (
      <button className="link" onClick={onOpen}>
        Показать архив
      </button>
    )
  }
  if (boards.length === 0) return <p className="muted small">В архиве пусто.</p>

  return (
    <section className="stack">
      <h2 className="section-title">Архив</h2>
      <ul className="member-list">
        {boards.map((b) => (
          <li key={b.id}>
            <span>{b.name}</span>
            {canEdit && (
              <button className="link" onClick={() => onRestore(b.id)}>
                Вернуть
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
