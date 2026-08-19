import { useCallback, useEffect, useState } from 'react'
import { ApiError, VISIBILITY_NAMES, api } from '../shared/api/index.ts'
import { plural } from '../shared/lib/plural.ts'
import type { BoardInfo, Member, Principal, Team } from '../shared/api/index.ts'
import { BoardAccess } from '../features/access/BoardAccess.tsx'
import { EmptyState, Skeleton } from '../shared/ui/states.tsx'
import { ConfirmDialog } from '../shared/ui/Dialog.tsx'
import { useToast } from '../shared/ui/Toast.tsx'

/**
 * Вторая строка доски: ключ, кому видна и сколько работы.
 *
 * Видимость «своей команде» без названия подразделения ничего
 * не отвечает тому, кто состоит в нескольких: своей — это какой?
 * Название берётся из списка подразделений, уже загруженного
 * для панели доступа.
 */
function boardLine(b: BoardInfo, teams: Team[]): string {
  const parts = [b.key]
  if (b.visibility === 'team') {
    const team = teams.find((t) => t.id === b.teamId)
    parts.push(team ? `подразделению «${team.name}»` : VISIBILITY_NAMES.team.toLowerCase())
  } else if (b.visibility) {
    parts.push(VISIBILITY_NAMES[b.visibility].toLowerCase())
  }
  if (b.cards !== undefined) {
    parts.push(`${b.cards} ${plural(b.cards, 'карточка', 'карточки', 'карточек')}`)
  }
  return parts.join(' · ')
}

export function BoardList({
  principal,
  onOpen,
}: {
  principal: Principal
  onOpen: (id: string) => void
}) {
  const [boards, setBoards] = useState<BoardInfo[] | null>(null)
  const [name, setName] = useState('')
  // Ключ спрашивается, но не требуется: пустой означает «выведи
  // из названия». Поле стоит рядом с названием, потому что после
  // заведения ключ уже не сменить — он в номерах всех карточек.
  const [key, setKey] = useState('')
  const [error, setError] = useState<string | null>(null)
  // Отказ заведения показывается у формы, а не наверху списка: форма
  // стоит под досками, и сообщение над ними человек не увидит вовсе.
  const [failed, setFailed] = useState<{ text: string; aboutKey: boolean } | null>(null)
  const [openAccess, setOpenAccess] = useState<string | null>(null)
  const [archived, setArchived] = useState<BoardInfo[] | null>(null)
  // Какую доску спрашивают удалить и что набрали в подтверждение.
  const [toDelete, setToDelete] = useState<BoardInfo | null>(null)
  const [typed, setTyped] = useState('')
  // Люди и подразделения нужны только настройке доступа, поэтому берутся
  // один раз на список, а не по разу на каждую доску.
  const notify = useToast()
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
              {/* Ключ, видимость и объём — то же, что о доске написано
                  в дереве подразделений: «ПЛАТ · своей команде». Список
                  знал одно название, и выбирать приходилось по нему —
                  при том что «эту видят все или только мы» и есть
                  вопрос, ради которого в список заглядывают. */}
              <div className="member-who">
                <button onClick={() => onOpen(b.id)}>{b.name}</button>
                <span className="muted small">{boardLine(b, teams)}</span>
              </div>
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
        teams={teams}
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
            .then((r) => {
              setArchived(r.boards)
              // Отменить нечего, поэтому сообщение без действия —
              // но сказать, что случилось, обязано: строка исчезает
              // из архива молча, и это ровно то, чего человек боится
              // после «навсегда».
              notify({ text: `Доска «${board.name}» удалена навсегда.`, tone: 'warning' })
            })
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
        {/* В подсказке поля стоит слово «название», а не само название:
            барьер задуман как заминка, а подсказка-ответ прямо в поле,
            куда его надо переписать, эту заминку и отменяет. Само
            название названо выше, в первой строке диалога. */}
        <input
          value={typed}
          aria-label="Название доски для подтверждения"
          placeholder="Название доски"
          onChange={(e) => setTyped(e.target.value)}
        />
      </ConfirmDialog>

      {canEdit && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim()) return
            setFailed(null)
            api
              .createBoard(name.trim(), key.trim())
              .then((b) => {
                setName('')
                setKey('')
                load()
                onOpen(b.id)
              })
              .catch((e) => {
                // Про ключ отвечает код, а не текст: разбор текста
                // ломается на первой же правке формулировки.
                const code = e instanceof ApiError ? e.body?.code : undefined
                setFailed({
                  text: e instanceof Error ? e.message : 'Не удалось создать доску',
                  aboutKey: code === 'board_key_invalid' || code === 'board_key_taken',
                })
              })
          }}
        >
          <input
            value={name}
            placeholder="Название новой доски"
            onChange={(e) => setName(e.target.value)}
          />
          {/* Ключ приводится к заглавным при вводе, а не начертанием:
              `text-transform` в стиле переписал бы и подпись-подсказку,
              и «Ключ» в пустом поле кричал бы капслоком. */}
          <input
            className="key-input"
            value={key}
            maxLength={6}
            aria-label="Ключ доски — префикс номеров карточек"
            aria-invalid={failed?.aboutKey || undefined}
            aria-describedby="board-key-hint"
            placeholder="Ключ"
            onChange={(e) => setKey(e.target.value.toUpperCase())}
          />
          <button className="primary" type="submit" disabled={!name.trim()}>
            Создать
          </button>
        </form>
      )}

      {/* Отказ стоит вплотную к форме, а подсказка — под ним: между
          полем и объяснением, почему оно не приняло, ничего стоять
          не должно. */}
      {failed && <p className="error">{failed.text}</p>}
      {/* Правило сказано до отказа, а не после: ключ виден в каждом
          номере карточки и после заведения не меняется, так что узнать
          о нём из отказа — значит узнать поздно. */}
      {canEdit && (
        <p className="muted small" id="board-key-hint">
          Ключ — начало номеров карточек: ПОСТ-14. Пустой выведем из названия;
          сменить его потом уже нельзя.
        </p>
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
  teams,
  canEdit,
  onOpen,
  onRestore,
  onDelete,
}: {
  boards: BoardInfo[] | null
  teams: Team[]
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
            {/* Строка архива говорит о доске то же, что и строка списка:
                выбирать, какую вернуть и какую стереть, по одному
                названию — значит выбирать вслепую, а «навсегда» здесь
                рядом. */}
            <div className="member-who">
              <span>{b.name}</span>
              <span className="muted small">{boardLine(b, teams)}</span>
            </div>
            {/* Имя называет доску: в архиве этих кнопок столько же,
                сколько досок, и без названия они звучат одинаково. */}
            {canEdit && (
              <button
                className="link"
                aria-label={`Вернуть из архива: ${b.name}`}
                onClick={() => onRestore(b.id)}
              >
                Вернуть
              </button>
            )}
            {onDelete && (
              <button
                className="link link--danger"
                aria-label={`Удалить навсегда: ${b.name}`}
                onClick={() => onDelete(b)}
              >
                Удалить навсегда
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
