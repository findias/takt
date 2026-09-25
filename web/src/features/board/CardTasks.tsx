import { useEffect, useState } from 'react'
import { Button } from '../../shared/ui/Button.tsx'
import { PlusIcon } from '../../shared/ui/icons.tsx'
import { LINK_KIND_NAMES, REF_KINDS, request } from '../../shared/api/index.ts'
import type { BoardInfo, CardRef, LinkKind, RefKind } from '../../shared/api/index.ts'
import type { BaseState } from '../../entities/board/model.ts'
import { cardDetails, progressRatio, refKindName } from '../../entities/card/model.ts'
import type { CardDetails, Related } from '../../entities/card/model.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Вкладка «Задачи» карточки: заявки сервис-деска, родитель, подзадачи
 * и связи.
 *
 * Отдельным куском сборки, а не частью панели: вкладку открывают
 * по нажатию, а входной кусок обязан укладываться в порог размера
 * (perf.spec.ts, «сборка не разрастается»). С приходом заявок панель
 * его перешагнула. Кусок начинают качать, как только открыта карточка
 * (loadCardTasks в CardPanel), — к нажатию на вкладку он уже на месте.
 */
export function CardTasks({
  base,
  details,
  progress,
  canEdit,
  subtaskBoards,
  onOpenCard,
  onSubtask,
  onLink,
  onUnlink,
  onMarkDone,
  onAddRef,
  onRemoveRef,
  onHold,
}: {
  base: BaseState
  details: CardDetails
  /** Подпись прогресса разбиения; пусто — частей нет. */
  progress: string | null
  canEdit: boolean
  subtaskBoards: BoardInfo[]
  onOpenCard: (cardId: string) => void
  onSubtask: (parentCardId: string, title: string, boardId?: string) => void
  onLink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onUnlink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onMarkDone: (cardId: string, done: boolean) => void
  onAddRef: (cardId: string, kind: RefKind, ref: string) => void
  onRemoveRef: (cardId: string, refId: string) => void
  /** Часть держит саму задачу: форма блокировки — на «Работе». */
  onHold: (part: Related) => void
}) {
  const { card } = details
  return (
    <>
      <Refs
        refs={base.cardRefs[card.id] ?? []}
        canEdit={canEdit}
        onAdd={(kind, ref) => onAddRef(card.id, kind, ref)}
        onRemove={(refId) => onRemoveRef(card.id, refId)}
      />

      {details.parent && (
        <section className="stack">
          {/* Одно понятие — одно слово: связь называется парой
              «родительская задача» и «подзадача». «Часть задачи»
              было третьим словом на то же самое. */}
          <h3 className="section-title">{t.panel.parent}</h3>
          <RelatedRow
            related={details.parent}
            canEdit={canEdit}
            onOpen={onOpenCard}
            onRemove={() => onUnlink(details.parent!.id, card.id, 'subtask')}
          />
        </section>
      )}

      <section className="stack">
        <div className="row row--between">
          <h3 className="section-title">{t.panel.subtasks}</h3>
          {progress && <span className="muted small">{progress}</span>}
        </div>

        {progress && (
          <div
            className="progress"
            role="progressbar"
            aria-valuenow={card.progress?.done ?? 0}
            aria-valuemin={0}
            aria-valuemax={card.progress?.total ?? 0}
            aria-label={t.panel.done(progress)}
          >
            <div
              className="progress-fill"
              style={{ transform: `scaleX(${progressRatio(card)})` }}
            />
          </div>
        )}

        {details.subtasks.length === 0 && (
          <p className="muted small">{t.panel.noSubtasks}</p>
        )}
        {details.subtasks.map((s) => (
          <RelatedRow
            key={s.id}
            related={s}
            canEdit={canEdit}
            onOpen={onOpenCard}
            onRemove={() => onUnlink(card.id, s.id, 'subtask')}
            onMarkDone={onMarkDone}
            // Часть может держать саму задачу, и говорят об этом
            // отсюда: у родителя, где видно и остальные части.
            // У заблокированной задачи предлагать нечего — вторая
            // блокировка поверх открытой отказала бы.
            onHold={canEdit && !card.blocked && !s.done ? () => onHold(s) : undefined}
          />
        ))}

        {canEdit && (
          <NewSubtask
            boards={subtaskBoards}
            onCreate={(title, toBoard) => onSubtask(card.id, title, toBoard)}
          />
        )}

        {/* Связать существующую — отдельный путь и подписан отдельно:
            без подписи два ряда полей подряд читались как одно
            непонятное место. */}
        {canEdit && (
          <LinkPicker
            boardId={base.info.id}
            details={details}
            onPick={(picked, kind) =>
              // «Родитель» — та же связь «подзадача», только в обратную
              // сторону: выбранная карточка встаёт над этой.
              kind === 'parent' ? onLink(picked, card.id, 'subtask') : onLink(card.id, picked, kind)
            }
          />
        )}
      </section>

      {/* Связи стоят рядом с подзадачами, а не под историей, где
          они лежали раньше: история длиннее всего остального
          вместе, и раздел под ней не находил никто. */}
      {details.related.length > 0 && (
        <section className="stack">
          <h3 className="section-title">{t.panel.links}</h3>
          {details.related.map((r) => (
            <RelatedRow
              key={`${r.kind}-${r.id}`}
              related={r}
              canEdit={canEdit}
              showKind
              onOpen={onOpenCard}
              onRemove={() => onUnlink(card.id, r.id, r.kind)}
            />
          ))}
        </section>
      )}
    </>
  )
}

/**
 * Строка связанной карточки.
 *
 * Название — кнопка, если карточка на этой доске: связь должна
 * проходиться в обе стороны. Раньше из подзадачи было видно родителя,
 * но добраться до него можно было только поиском по доске — то есть
 * связь показывалась, но не работала.
 *
 * Карточку с чужой доски открыть отсюда нельзя: она живёт в другом
 * адресе, и «открытие», которое унесёт с текущей доски, — это не то,
 * чего ждут от строки в списке.
 */
function RelatedRow({
  related,
  canEdit,
  showKind,
  onOpen,
  onRemove,
  onMarkDone,
  onHold,
}: {
  related: Related
  canEdit: boolean
  showKind?: boolean
  onOpen?: (cardId: string) => void
  onRemove: () => void
  /** Отметить часть сделанной. Пусто — отметка отсюда невозможна: так
   *  у связей, которые не подзадачи, и у чужих карточек — их отмечают
   *  на своей доске. */
  onMarkDone?: (cardId: string, done: boolean) => void
  /** Объявить, что эта часть держит задачу. Пусто — предлагать нечего:
   *  задача уже заблокирована, часть сделана или прав нет. */
  onHold?: () => void
}) {
  // Флажок и галочка отвечают на один вопрос, поэтому вместе их нет:
  // где отметку можно поставить, состояние показывает сам флажок.
  const markable = Boolean(onMarkDone) && canEdit && related.onThisBoard
  const title = (
    <>
      {related.done && !markable && <span aria-hidden="true">✓ </span>}
      {related.done && <span className="sr-only">{t.panel.doneSr}</span>}
      {related.blocked && <span aria-hidden="true">⛔ </span>}
      {related.blocked && <span className="sr-only">{t.panel.blockedSr}</span>}
      {related.title}
    </>
  )

  return (
    <div className={`related${related.reachable ? '' : ' related--hidden'}`}>
      {markable && (
        <button
          type="button"
          role="checkbox"
          aria-checked={related.done}
          className="subtask-check"
          title={related.done ? t.panel.unmarkDone : t.panel.markDone}
          aria-label={t.panel.doneOf(related.title)}
          onClick={() => onMarkDone?.(related.id, !related.done)}
        >
          <span className={`subtask-box${related.done ? ' subtask-box--done' : ''}`} />
        </button>
      )}
      <div className="member-who">
        {related.onThisBoard && onOpen ? (
          <button className="link related-open" onClick={() => onOpen(related.id)}>
            {title}
          </button>
        ) : (
          <span>{title}</span>
        )}
        <span className="muted small">
          {showKind ? `${LINK_KIND_NAMES[related.kind]} · ` : ''}
          {related.where}
        </span>
        {/* Вторая строка — только про чужую работу: что с ней сейчас
            и когда её ждать. Своя видна на самой доске. */}
        {(related.stage || related.promise) && (
          <span className="muted small related-note">
            {[related.stage, related.promise].filter(Boolean).join(' · ')}
          </span>
        )}
      </div>
      {onHold && related.reachable && (
        // Слово то же, что на доске у держащей стороны зависимости:
        // «держит» там и «держит» здесь — про одно и то же.
        <button className="link" onClick={onHold}>
          {t.panel.holds}
        </button>
      )}
      {canEdit && related.reachable && (
        <button className="link link--remove" onClick={onRemove}>
          {t.panel.remove}
        </button>
      )}
    </div>
  )
}

/**
 * Завести подзадачу одним полем — и, если есть куда, на доске соседей.
 *
 * Постановка работы другой команде устроена тем же, чем всякая работа:
 * карточкой на их доске. Отдельной «заявки» нет намеренно — принятая
 * заявка превратилась бы в карточку, и две записи об одном деле были бы
 * обязаны совпадать, не будучи обязанными совпасть.
 *
 * Выбор доски не появляется, пока выбирать не из чего: в организации
 * с одной доской это был бы пункт с единственным ответом.
 */
function NewSubtask({
  boards,
  onCreate,
}: {
  boards: BoardInfo[]
  onCreate: (title: string, boardId?: string) => void
}) {
  const [title, setTitle] = useState('')
  const [boardId, setBoardId] = useState('')
  const target = boards.find((b) => b.id === boardId)

  return (
    <form
      className="stack stack--tight"
      onSubmit={(e) => {
        e.preventDefault()
        if (!title.trim()) return
        onCreate(title.trim(), boardId || undefined)
        setTitle('')
      }}
    >
      <div className="row row--tight">
        <input
          value={title}
          placeholder={t.panel.whatToDo}
          aria-label={t.panel.subtaskName}
          onChange={(e) => setTitle(e.target.value)}
        />
        {boards.length > 0 && (
          <select
            value={boardId}
            aria-label={t.panel.subtaskBoard}
            onChange={(e) => setBoardId(e.target.value)}
          >
            <option value="">{t.panel.onThisBoard}</option>
            {boards.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        )}
        <Button kind="primary" type="submit" icon={<PlusIcon />} disabled={!title.trim()}>
          {t.panel.subtask}
        </Button>
      </div>
      {/* Сказано до нажатия, а не после отказа: правила доски-получателя
          заказ не обходит, и это лучше знать заранее. */}
      {target && (
        <p className="muted small">{t.panel.goesTo(target.name)}</p>
      )}
    </form>
  )
}

/**
 * Заявки внешних систем, по которым идёт работа: RDS, ЗНО, ЗНИ, проблемы.
 *
 * Одно поле на все четыре вида, а не четыре поля: вид выбирают рядом,
 * и у работы с тремя ЗНО и одним ЗНИ не бывает пустых полей для
 * остального. Список — в порядке видов, внутри вида — как добавляли.
 *
 * Адрес открывается ссылкой, номер остаётся текстом: угадывать адрес
 * по номеру значило бы зашить сюда чужую систему.
 */
function Refs({
  refs,
  canEdit,
  onAdd,
  onRemove,
}: {
  refs: CardRef[]
  canEdit: boolean
  onAdd: (kind: RefKind, ref: string) => void
  onRemove: (refId: string) => void
}) {
  const [kind, setKind] = useState<RefKind>(REF_KINDS[0])
  const [draft, setDraft] = useState('')
  const ordered = [...refs].sort((a, b) => REF_KINDS.indexOf(a.kind) - REF_KINDS.indexOf(b.kind))

  return (
    <section className="stack">
      <h3 className="section-title">{t.panel.refs}</h3>

      {refs.length === 0 && <p className="muted small">{t.panel.noRefs}</p>}

      {ordered.map((r) => (
        <div className="related" key={r.id}>
          <div className="member-who">
            <span className="wrap-title">
              {/^https?:\/\//i.test(r.ref) ? (
                <a href={r.ref} target="_blank" rel="noopener noreferrer">
                  {r.ref}
                </a>
              ) : (
                r.ref
              )}
            </span>
            <span className="muted small">{refKindName(r.kind)}</span>
          </div>
          {canEdit && (
            <button
              className="link link--remove"
              aria-label={t.panel.removeRef(refKindName(r.kind), r.ref)}
              onClick={() => onRemove(r.id)}
            >
              {t.panel.remove}
            </button>
          )}
        </div>
      ))}

      {canEdit && (
        <form
          className="row row--tight"
          onSubmit={(e) => {
            e.preventDefault()
            if (!draft.trim()) return
            onAdd(kind, draft.trim())
            setDraft('')
          }}
        >
          <select
            value={kind}
            aria-label={t.panel.refKind}
            onChange={(e) => setKind(e.target.value as RefKind)}
          >
            {REF_KINDS.map((k) => (
              <option key={k} value={k}>
                {refKindName(k)}
              </option>
            ))}
          </select>
          <input
            value={draft}
            placeholder={t.panel.refPlaceholder}
            aria-label={t.panel.refPlaceholder}
            maxLength={500}
            onChange={(e) => setDraft(e.target.value)}
          />
          <Button kind="primary" type="submit" icon={<PlusIcon />} disabled={!draft.trim()}>
            {t.panel.refAdd}
          </Button>
        </form>
      )}
    </section>
  )
}

/** Карточка из поиска по организации (`GET /api/cards/search`). */
type FoundCard = {
  id: string
  number: string
  title: string
  boardId: string
  boardName: string
  boardLevel: 'team' | 'portfolio'
}

const searchCards = (q: string) =>
  request<{ cards: FoundCard[] }>('GET', `/api/cards/search?q=${encodeURIComponent(q)}`)

/** Вид связи в выборе: серверные виды и «Родитель» — подзадача наоборот. */
type PickKind = LinkKind | 'parent'

/**
 * Связать с существующей карточкой — с любой доски, которую видно.
 *
 * Прежде выбор предлагал только карточки этой доски, и задачу команды
 * нельзя было подвесить к эпику портфеля, а к эпику — готовую задачу
 * команды (замечено владельцем 25.09.2026): сервер такую связь делал
 * всегда, выбрать было не из чего. Теперь — поиск по номеру и названию
 * на всех видимых досках, и вид «Родитель», чтобы с задачи выбрать
 * то, что над ней.
 *
 * Поиск с задержкой в четверть секунды и с двух знаков: по одной букве
 * находится полорганизации, и выбирать из этого нечего.
 */
function LinkPicker({
  boardId,
  details,
  onPick,
}: {
  boardId: string
  details: ReturnType<typeof cardDetails>
  onPick: (picked: string, kind: PickKind) => void
}) {
  const [kind, setKind] = useState<PickKind>('subtask')
  const [query, setQuery] = useState('')
  const [found, setFound] = useState<FoundCard[] | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const q = query.trim()
    if (q.length < 2) {
      setFound(null)
      setFailed(false)
      return
    }
    let stale = false
    const timer = window.setTimeout(() => {
      searchCards(q)
        .then((r) => {
          if (stale) return
          setFound(r.cards)
          setFailed(false)
        })
        .catch(() => {
          if (!stale) setFailed(true)
        })
    }, 250)
    return () => {
      stale = true
      window.clearTimeout(timer)
    }
  }, [query])

  if (!details) return null
  // Сама карточка, её подзадачи и её родитель уже связаны — предлагать
  // их снова значит предлагать повтор.
  const taken = new Set<string>([details.card.id, ...details.subtasks.map((s) => s.id)])
  if (details.parent) taken.add(details.parent.id)
  const options = (found ?? []).filter((c) => !taken.has(c.id))

  return (
    <details className="link-picker">
      <summary className="muted small">{t.panel.linkExisting}</summary>
      <div className="row row--tight">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as PickKind)}
          aria-label={t.panel.linkKind}
        >
          {(Object.keys(LINK_KIND_NAMES) as LinkKind[]).map((k) => (
            <option key={k} value={k}>
              {LINK_KIND_NAMES[k]}
            </option>
          ))}
          <option value="parent">{t.panel.linkParent}</option>
        </select>
        <input
          type="search"
          value={query}
          placeholder={t.panel.findToLink}
          aria-label={t.panel.linkCard}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {failed && <p className="error small">{t.panel.searchToLinkFailed}</p>}
      {found !== null && !failed && options.length === 0 && (
        <p className="muted small">{t.panel.nothingToLink}</p>
      )}
      {options.length > 0 && (
        <ul className="link-results">
          {options.map((c) => (
            <li key={c.id}>
              <button
                type="button"
                className="link-result"
                onClick={() => {
                  onPick(c.id, kind)
                  setQuery('')
                }}
              >
                <span className="link-result-number">{c.number}</span>
                <span className="link-result-title">{c.title}</span>
                <span className="muted small">
                  {c.boardId === boardId ? t.panel.onThisBoard : c.boardName}
                  {c.boardLevel === 'portfolio' ? ` · ${t.panel.portfolioBoard}` : ''}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </details>
  )
}
