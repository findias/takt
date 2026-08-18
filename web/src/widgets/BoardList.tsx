import { useCallback, useEffect, useState } from 'react'
import { api } from '../shared/api/index.ts'
import type { BoardInfo, Member, Principal, Team } from '../shared/api/index.ts'
import { BoardAccess } from '../features/access/BoardAccess.tsx'
import { EmptyState, Skeleton } from '../shared/ui/states.tsx'
import { ConfirmDialog } from '../shared/ui/Dialog.tsx'

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
  // Какую доску спрашивают удалить и что набрали в подтверждение.
  const [toDelete, setToDelete] = useState<BoardInfo | null>(null)
  const [typed, setTyped] = useState('')
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
      {boards === null && <Skeleton lines={3} />}
      {boards?.length === 0 && (
        <EmptyState title="Досок пока нет">
          {canEdit
            ? 'Доска — это колонки и карточки: заведите первую внизу, остальное появится по ходу дела.'
            : 'В этой организации ещё не завели ни одной доски. Заводит их тот, кто может изменять данные.'}
        </EmptyState>
      )}

      <ul className="board-list">
        {boards?.map((b) => (
          <li key={b.id}>
            <div className="row row--between">
              <button onClick={() => onOpen(b.id)}>{b.name}</button>
              <div className="row row--tight">
                {/* Имя называет доску: «Доступ» стоит в каждой строке
                    списка, и без названия они звучат одинаково. */}
                <button
                  className="link"
                  aria-expanded={openAccess === b.id}
                  aria-label={`Доступ к доске «${b.name}»`}
                  onClick={() => setOpenAccess((v) => (v === b.id ? null : b.id))}
                >
                  Доступ
                </button>
                {canEdit && (
                  <button
                    className="link"
                    aria-label={`Убрать доску «${b.name}» в архив`}
                    title="Карточки и история сохранятся"
                    onClick={() => {
                      api
                        .archiveBoard(b.id)
                        .then(() => {
                          load()
                          // Показываем архив сразу: доска, исчезнувшая
                          // из списка без следа, читается как потеря.
                          return api.archivedBoards().then((r) => setArchived(r.boards))
                        })
                        .catch((e) =>
                          setError(e instanceof Error ? e.message : 'Не удалось убрать доску'),
                        )
                    }}
                  >
                    В архив
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
        // Удалять насовсем может один владелец: действие необратимо,
        // и уносит оно работу целой команды.
        onDelete={
          principal.role === 'owner'
            ? (board) => setToDelete(board)
            : undefined
        }
      />

      {/* Название набирают руками, а не просто подтверждают. Вопрос
          «вы уверены?» отвечают не читая; на вопрос «наберите название»
          нельзя ответить, не посмотрев, что именно удаляешь. */}
      <ConfirmDialog
        open={toDelete !== null}
        title="Удалить доску навсегда?"
        confirmLabel="Удалить навсегда"
        danger
        confirmDisabled={typed.trim() !== toDelete?.name}
        onCancel={() => {
          setToDelete(null)
          setTyped('')
        }}
        onConfirm={() => {
          const board = toDelete
          setToDelete(null)
          setTyped('')
          if (!board) return
          api
            .deleteBoard(board.id, board.name)
            .then(() => api.archivedBoards())
            .then((r) => setArchived(r.boards))
            .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось удалить доску'))
        }}
      >
        <p>
          Доска «{toDelete?.name}» исчезнет вместе со всеми карточками, колонками, итерациями,
          обсуждениями и историей работы. Вернуть будет нечем.
        </p>
        <p className="muted small">
          В журнале действий останется запись о том, кто её удалил. Наберите название доски, чтобы
          подтвердить.
        </p>
        <input
          value={typed}
          aria-label="Название доски для подтверждения"
          placeholder={toDelete?.name}
          onChange={(e) => setTyped(e.target.value)}
        />
      </ConfirmDialog>

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
          <button className="primary" type="submit" disabled={!name.trim()}>
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
  onDelete,
}: {
  boards: BoardInfo[] | null
  canEdit: boolean
  onOpen: () => void
  onRestore: (id: string) => void
  /** Пусто — удалять насовсем нельзя: так у всех, кроме владельца. */
  onDelete?: (board: BoardInfo) => void
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
            {onDelete && (
              <button className="link link--danger" onClick={() => onDelete(b)}>
                Удалить навсегда
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
