import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import { ApiError, VISIBILITY_NAMES, api } from '../shared/api/index.ts'
import type { BoardInfo, Member, Principal, Team } from '../shared/api/index.ts'
import { EmptyState, Skeleton } from '../shared/ui/states.tsx'
import { ConfirmDialog } from '../shared/ui/Dialog.tsx'
import { useToast } from '../shared/ui/Toast.tsx'
import { Field, FormError, useFormErrors } from '../shared/ui/Field.tsx'
import { ScreenError } from '../shared/ui/Field'
import { t } from '../shared/i18n/index.ts'
import { navigate } from '../shared/router/index.ts'

// Настройку доступа раскрывают у одной доски и изредка — тот же довод,
// что у панели доступа на самой доске: грузить её вместе со списком
// значит платить за неё при каждом открытии (П7).
const BoardAccess = lazy(() =>
  import('../features/access/BoardAccess.tsx').then((m) => ({ default: m.BoardAccess })),
)

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
    parts.push(team ? t.boards.visibleToTeam(team.name) : VISIBILITY_NAMES.team.toLowerCase())
  } else if (b.visibility) {
    parts.push(VISIBILITY_NAMES[b.visibility].toLowerCase())
  }
  if (b.cards !== undefined) {
    parts.push(t.card.cards(b.cards))
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
  const form = useFormErrors()
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
      .catch((e) => setError(e instanceof Error ? e.message : t.boards.loadFailed))
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
      <ScreenError>{error}</ScreenError>
      {boards === null && <Skeleton lines={3} />}
      {boards?.length === 0 && (
        <EmptyState title={t.boards.emptyTitle}>
          {canEdit
            ? t.boards.emptyEditor
            : t.boards.emptyReader}
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
                  aria-label={t.boards.accessTo(b.name)}
                  onClick={() => setOpenAccess((v) => (v === b.id ? null : b.id))}
                >
                  {t.boards.access}
                </button>
                {canEdit && (
                  <button
                    className="link link--remove"
                    aria-label={t.boards.archiveBoard(b.name)}
                    title={t.boards.archiveKeeps}
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
                          setError(e instanceof Error ? e.message : t.boards.archiveFailed),
                        )
                    }}
                  >
                    {t.boards.toArchive}
                  </button>
                )}
              </div>
            </div>
            {openAccess === b.id && (
              <Suspense fallback={<Skeleton lines={3} />}>
                <BoardAccess
                  boardId={b.id}
                  people={people}
                  teams={teams}
                  canEdit={canEdit}
                  onClose={() => setOpenAccess(null)}
                />
              </Suspense>
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
            .catch((e) => setError(e instanceof Error ? e.message : t.boards.restoreFailed))
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
        title={t.boards.deleteTitle}
        confirmLabel={t.boards.deleteForever}
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
              notify({ text: t.boards.deleted(board.name), tone: 'warning' })
            })
            .catch((e) => setError(e instanceof Error ? e.message : t.boards.deleteFailed))
        }}
      >
        <p>{t.boards.deleteBody(toDelete?.name ?? '')}</p>
        <p className="muted small">{t.boards.deleteAudit}</p>
        {/* В подсказке поля стоит слово «название», а не само название:
            барьер задуман как заминка, а подсказка-ответ прямо в поле,
            куда его надо переписать, эту заминку и отменяет. Само
            название названо выше, в первой строке диалога. */}
        <input
          value={typed}
          aria-label={t.boards.confirmName}
          placeholder={t.boards.boardName}
          onChange={(e) => setTyped(e.target.value)}
        />
      </ConfirmDialog>

      {canEdit && (
        <form
          className="form-row"
          ref={form.ref}
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            const found = form.check(e.currentTarget)
            if (Object.keys(found).length > 0) {
              form.report(found)
              return
            }
            form.clear()
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
                const text = e instanceof Error ? e.message : t.boards.createFailed
                if (code === 'board_key_invalid' || code === 'board_key_taken') {
                  form.report({ key: text })
                } else {
                  form.reportForm(text)
                }
              })
          }}
        >
          {/* Подписи нет на экране, но есть у поля: действие названо
              кнопкой в конце ряда, а имя нужно диктору и отказу. */}
          <Field label={t.boards.newBoardName} hiddenLabel {...form.field('name')}>
            {(bind) => (
              <input
                {...bind}
                name="name"
                value={name}
                required
                placeholder={t.boards.newBoardName}
                onChange={(e) => setName(e.target.value)}
              />
            )}
          </Field>
          {/* Ключ приводится к заглавным при вводе, а не начертанием:
              `text-transform` в стиле переписал бы и подпись-подсказку,
              и «Ключ» в пустом поле кричал бы капслоком.

              Правило про ключ стоит под формой, а не под полем: оно
              длиннее самого поля втрое и, поставленное внутрь, растянуло
              бы свою колонку и разорвало ряд на три строки. Связано
              с полем всё равно — `describedBy`. */}
          <Field
            label={t.boards.keyLabel}
            hiddenLabel
            describedBy="board-key-hint"
            {...form.field('key')}
          >
            {(bind) => (
              <input
                {...bind}
                name="key"
                className="key-input"
                value={key}
                maxLength={6}
                placeholder={t.boards.key}
                onChange={(e) => setKey(e.target.value.toUpperCase())}
              />
            )}
          </Field>
          <button className="primary" type="submit" aria-label={t.boards.createBoard}>
            {t.boards.create}
          </button>
        </form>
      )}

      {/* Правило сказано до отказа, а не после: ключ виден в каждом
          номере карточки и после заведения не меняется, так что узнать
          о нём из отказа — значит узнать поздно. */}
      {canEdit && (
        <p className="muted small" id="board-key-hint">
          {t.boards.keyHint}
        </p>
      )}
      {/* Переезд — ссылкой, а не кнопкой: он ведёт на свой экран, и открыть
          его в соседней вкладке — законное желание. */}
      {canEdit && (
        <p className="board-import">
          <a
            className="link link--alone"
            href="/import"
            onClick={(e) => {
              if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey) return
              e.preventDefault()
              navigate('/import')
            }}
          >
            {t.boards.importLink}
          </a>
        </p>
      )}
      {/* Отказ, у которого своего поля нет: «доска в архиве», «нет
          прав». Про ключ и про название говорят сами поля. */}
      <FormError>{form.formError}</FormError>
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
        {t.boards.showArchive}
      </button>
    )
  }
  if (boards.length === 0)
    return (
      <p className="muted small">{t.boards.archiveEmpty}</p>
    )

  return (
    <section className="stack">
      <h2 className="section-title">{t.boards.archive}</h2>
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
                aria-label={t.boards.restoreFrom(b.name)}
                onClick={() => onRestore(b.id)}
              >
                {t.boards.restore}
              </button>
            )}
            {onDelete && (
              <button
                className="link link--danger"
                aria-label={t.boards.deleteForeverOf(b.name)}
                onClick={() => onDelete(b)}
              >
                {t.boards.deleteForever}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
