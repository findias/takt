import { useEffect, useState } from 'react'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { EstimateStepper } from '../../shared/ui/EstimateStepper.tsx'
import { TabPanel, Tabs, useTabIds } from '../../shared/ui/Tabs.tsx'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { Button } from '../../shared/ui/Button.tsx'
import { PlusIcon } from '../../shared/ui/icons.tsx'
import { LINK_KIND_NAMES, REF_KINDS, api } from '../../shared/api/index.ts'
import type {
  BoardEvent,
  BoardInfo,
  CardField,
  EstimateUnit,
  FieldValue,
  Iteration,
  BoardLabel,
  CardRef,
  LinkKind,
  Priority,
  RefKind,
  SourceHistory,
} from '../../shared/api/index.ts'
import { actorText, eventText, sourceName, timeText } from '../../entities/feed/model.ts'
import { Discussion } from './Discussion.tsx'
import type { BaseState } from '../../entities/board/model.ts'
import {
  PRIORITIES,
  PRIORITY_NAMES,
  UNIT_SHORT,
  dateWords,
  dueLabel,
  priorityLabel,
  candidatesForSubtask,
  cardDetails,
  progressLabel,
  progressRatio,
  refKindName,
} from '../../entities/card/model.ts'
import type { Related } from '../../entities/card/model.ts'
import { labelOrigin, chipClass } from '../../entities/label/model.ts'
import { BlockUntilEditor, fromLocalInput } from './BlockUntil.tsx'
import { LabelCombobox } from './LabelPicker.tsx'
import { locale, t } from '../../shared/i18n/index.ts'
import { Hint } from '../../shared/ui/Hint.tsx'

/**
 * Карточка целиком: описание, подзадачи, связи, блокировка.
 *
 * Подзадача может лежать на доске другой команды — ради этого связи и
 * вынесены в отдельную таблицу. Экран обязан это показывать: у подзадачи
 * видно, где она идёт и чья это команда, иначе «три из пяти» превращается
 * в число без смысла.
 */
/**
 * Разделы карточки.
 *
 * Разделение по смыслу, а не поровну: на «Работе» лежит всё, чем задачу
 * делают, — описание, исполнители, оценка, поля, подзадачи, связи.
 * Обсуждение и история вынесены не потому, что они менее важны, а потому
 * что растут сами: история прибавляется с каждым движением карточки
 * и в общем свитке вытесняла вниз то, ради чего карточку открывали.
 *
 * Обсуждение идёт первым и открывается по умолчанию. Карточку чаще
 * открывают, чтобы прочитать, о чём договорились, чем чтобы поправить
 * оценку: у поля и метки есть свой путь с доски, а у разговора — нет,
 * его читают только отсюда.
 */
type TabId = 'talk' | 'work' | 'history' | 'tasks'

// «Задачи» собирают в одно место всё, с чем эта работа связана: заявки
// сервис-деска, родителя, подзадачи и связи. Пока они стояли в хвосте
// «Работы», под метками и полями, до них листали весь свиток.
const TAB_IDS: TabId[] = ['talk', 'work', 'history', 'tasks']
const tabs = () => TAB_IDS.map((id) => ({ id, label: t.panel[id] }))

/** С чего открывается карточка. Первая вкладка — она же умолчание:
 *  выбранная не первая читается как «здесь уже что-то трогали». */
const FIRST_TAB: TabId = TAB_IDS[0]

export function CardPanel({
  base,
  boardId,
  cardId,
  unit,
  meId,
  canEdit,
  onClose,
  onDescribe,
  onEstimate,
  onOpenCard,
  onAssign,
  onLabel,
  onPrioritise,
  onDue,
  onSubtask,
  subtaskBoards,
  onLink,
  onUnlink,
  onBlock,
  onSetBlockUntil,
  onUnblock,
  onMarkDone,
  onIteration,
  onField,
  onAddRef,
  onRemoveRef,
}: {
  base: BaseState
  boardId: string
  cardId: string
  unit: EstimateUnit
  meId: string
  canEdit: boolean
  onClose: () => void
  onDescribe: (cardId: string, description: string) => void
  onEstimate: (cardId: string, estimate: number | null) => void
  /** Открыть другую карточку этой же доски — по связи. */
  onOpenCard: (cardId: string) => void
  /** on = назначить, off = снять. */
  onAssign: (cardId: string, userId: string, on: boolean) => void
  onLabel: (cardId: string, labelId: string, on: boolean) => void
  onPrioritise: (cardId: string, priority: Priority) => void
  /** null снимает обязательство. Пусто — не «дата неизвестна»,
   *  а «обязательства нет». */
  onDue: (cardId: string, dueOn: string | null) => void
  /** Завести новую подзадачу. Доска — только когда её ставят соседям:
   *  пусто означает «на этой», и в организации с одной доской спрашивать
   *  нечего. */
  onSubtask: (parentCardId: string, title: string, boardId?: string) => void
  /** Куда ещё можно поставить работу: доски организации, доступные
   *  на запись, кроме этой. Пустой список убирает выбор целиком. */
  subtaskBoards: BoardInfo[]
  onLink: (fromCard: string, toCard: string, kind: LinkKind) => void
  onUnlink: (fromCard: string, toCard: string, kind: LinkKind) => void
  /** Держащая карточка необязательна: «нет доступа к стенду» карточки
   *  не имеет, а «ждём согласования сметы» имеет — и по ней ходят. */
  onBlock: (cardId: string, reason: string, blockingCard?: string, until?: string) => void
  onSetBlockUntil: (cardId: string, until: string | null) => void
  onUnblock: (cardId: string) => void
  /** Отметить работу сделанной, не двигая её по доске. */
  onMarkDone: (cardId: string, done: boolean) => void
  /** null убирает карточку из текущей итерации. */
  onIteration: (cardId: string, iterationId: string | null) => void
  /** null снимает поле. */
  onField: (cardId: string, fieldId: string, value: string | number | boolean | null) => void
  onAddRef: (cardId: string, kind: RefKind, ref: string) => void
  onRemoveRef: (cardId: string, refId: string) => void
}) {
  const [mode, setMode] = usePanelMode()
  const [tab, setTab] = useState<TabId>(FIRST_TAB)
  const ids = useTabIds()
  // Какой частью сейчас объявляют блокировку. Причину всё равно пишут
  // словами: ссылка говорит, кого ждём, а чего именно от него ждут —
  // только слова.
  const [holder, setHolder] = useState<Related | null>(null)

  // Вкладка сбрасывается при переходе к другой карточке и намеренно
  // не запоминается, в отличие от режима панели: режим — про то, как
  // человеку удобно смотреть вообще, вкладка — про то, зачем он открыл
  // именно эту карточку. Заглянувший в историю одной задачи открывает
  // следующую не затем, чтобы снова читать историю.
  useEffect(() => setTab(FIRST_TAB), [cardId])

  const details = cardDetails(base, cardId)
  if (!details) return null
  const { card } = details
  const label = progressLabel(card, unit)
  // Кто держит эту работу. Ищется среди уже разобранных связей: держат
  // почти всегда свои части, а карточку с чужой доски снимок принёс
  // вместе со связью.
  const blockerId = card.blocked?.blockingCard
  const blocker = blockerId
    ? ([...details.subtasks, ...details.related].find((r) => r.id === blockerId) ?? null)
    : null
  // Кого держит сама эта карточка. Вопрос обратный к «кто держит нас»,
  // и без ответа на него часть выглядит обычной работой, хотя из-за неё
  // стоит задача. Ищется по доске: блокировка живёт на той карточке,
  // которая стоит, и знать о себе держащая не может.
  const holdingUp = Object.values(base.cards).filter((c) => c.blocked?.blockingCard === card.id)

  return (
    <Panel
      mode={mode}
      onMode={setMode}
      title={card.title}
      // Номер над названием: открыв карточку по ссылке из переписки,
      // первым делом сверяют, та ли это задача.
      eyebrow={<span className="card-number">{card.number}</span>}
      label={t.panel.cardLabel(card.number, card.title)}
      onClose={onClose}
    >
      <Tabs
        base={ids}
        tabs={tabs()}
        active={tab}
        onSelect={(id) => setTab(id as TabId)}
        label={t.panel.sections}
      />

      <TabPanel base={ids} id={tab}>
        {tab === 'work' && (
          <>
            {card.blocked ? (
              <div className="blocked">
                <div className="stack">
                  <strong>{t.panel.blocked}</strong>
                  <span className="small">{card.blocked.reason}</span>
                  {/* Кто держит — строкой с переходом: «ждём вот эту
                      работу» без пути к ней отправляет искать её
                      поиском по доске. */}
                  {blocker && (
                    <span className="small">
                      {t.panel.heldBy}{' '}
                      {blocker.onThisBoard ? (
                        <button className="link" onClick={() => onOpenCard(blocker.id)}>
                          {blocker.title}
                        </button>
                      ) : (
                        <span>
                          {blocker.title}
                          <span className="muted"> · {blocker.where}</span>
                        </span>
                      )}
                    </span>
                  )}
                  {/* Держащая уже сделана — значит блокировка пережила
                      свою причину. Само оно не снимется: время в блоке
                      считается из интервала, и закрывать его выведенным
                      признаком значит портить единственную честную меру.
                      Но сказать об этом обязаны. */}
                  {blocker?.done && (
                    <span className="small">{t.panel.holderDone}</span>
                  )}
                  <span className="muted small">
                    {t.panel.since(new Date(card.blocked.blockedAt).toLocaleString(locale()))}
                  </span>
                  <BlockUntilEditor
                    until={card.blocked.until}
                    canEdit={canEdit}
                    onChange={(until) => onSetBlockUntil(card.id, until)}
                  />
                </div>
                {/* «Снять» в панели стоит у блокировки, обязательства,
                    исполнителя и метки — четыре разных объекта, одно
                    слово. Глазами их различают по месту, с диктора они
                    звучат одинаково; имя называет объект. */}
                {canEdit && (
                  <button aria-label={t.panel.unblock} onClick={() => onUnblock(card.id)}>
                    {t.panel.lift}
                  </button>
                )}
              </div>
            ) : holder ? (
              // Блокировка частью: причину пишут здесь же, а не отдельным
              // шагом — иначе между «эта часть нас держит» и объяснением
              // почему стоял бы ещё один экран.
              <BlockForm
                holder={holder.title}
                onBlock={(reason, until) => {
                  onBlock(card.id, reason, holder.id, until)
                  setHolder(null)
                }}
                onCancel={() => setHolder(null)}
              />
            ) : (
              canEdit && (
                <BlockForm onBlock={(reason, until) => onBlock(card.id, reason, undefined, until)} />
              )
            )}

            {holdingUp.length > 0 && (
              <p className="small">
                {t.panel.holdingUp}{' '}
                {holdingUp.map((c, i) => (
                  <span key={c.id}>
                    {i > 0 && ', '}
                    <button className="link" onClick={() => onOpenCard(c.id)}>
                      {c.title}
                    </button>
                  </span>
                ))}
              </p>
            )}

            {/* Описание — первым: это ответ на вопрос «что тут вообще
                делать», и читают его прежде всех сроков и меток. Стояло
                шестым, ниже готовности, срока, приоритета, исполнителей
                и меток, — и находилось не сразу. */}
            <Description
              value={card.description}
              canEdit={canEdit}
              onSave={(text) => onDescribe(card.id, text)}
            />

            <DoneMark
              doneAt={card.doneAt}
              canEdit={canEdit}
              onChange={(done) => onMarkDone(card.id, done)}
            />

            <DuePicker
              value={card.dueOn}
              canEdit={canEdit}
              onChange={(next) => onDue(card.id, next)}
            />

            <PriorityPicker
              value={card.priority}
              canEdit={canEdit}
              onChange={(next) => onPrioritise(card.id, next)}
            />

            <IterationPicker
              iterations={base.iterations}
              current={base.cardIterations[card.id] ?? null}
              canEdit={canEdit}
              onChange={(id) => onIteration(card.id, id)}
            />

            <Assignees
              people={base.people}
              assignees={base.cardAssignees[card.id] ?? []}
              canEdit={canEdit}
              onAssign={(userId, on) => onAssign(card.id, userId, on)}
            />

            <Labels
              boardId={boardId}
              labels={base.labels}
              own={base.cardLabels[card.id] ?? []}
              canEdit={canEdit}
              onLabel={(labelId, on) => onLabel(card.id, labelId, on)}
            />

            <Estimate
              value={card.estimate}
              unit={unit}
              canEdit={canEdit}
              onSave={(value) => onEstimate(card.id, value)}
            />

            <Fields
              fields={base.fields}
              values={base.fieldValues[card.id] ?? []}
              canEdit={canEdit}
              onSet={(fieldId, value) => onField(card.id, fieldId, value)}
            />

          </>
        )}

        {/* Обсуждение и история заводятся только на своей вкладке.
            Это не только про место на экране: пока они лежали в общем
            свитке, открытие любой карточки стоило двух запросов,
            из которых чаще всего не нужен был ни один. */}
        {tab === 'talk' && (
          <Discussion
            boardId={boardId}
            cardId={card.id}
            meId={meId}
            canEdit={canEdit}
            people={base.people}
          />
        )}

        {tab === 'history' && (
          <History
            boardId={boardId}
            cardId={card.id}
            version={card.version}
            fields={base.fields}
          />
        )}

        {tab === 'tasks' && (
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
                {label && <span className="muted small">{label}</span>}
              </div>

              {label && (
                <div
                  className="progress"
                  role="progressbar"
                  aria-valuenow={card.progress?.done ?? 0}
                  aria-valuemin={0}
                  aria-valuemax={card.progress?.total ?? 0}
                  aria-label={t.panel.done(label)}
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
                  onHold={
                    canEdit && !card.blocked && !s.done
                      ? () => {
                          // Причину пишут в форме блокировки, а она
                          // на «Работе»: туда и переходим.
                          setHolder(s)
                          setTab('work')
                        }
                      : undefined
                  }
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
                  base={base}
                  details={details}
                  onPick={(toCard, kind) => onLink(card.id, toCard, kind)}
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
        )}
      </TabPanel>
    </Panel>
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
 * Итерация карточки.
 *
 * Предлагаются только открытые: закрытая итерация — утверждение о том, что
 * было сделано, и дописывать в неё задним числом нельзя. Текущая итерация
 * показывается всегда, даже закрытая, иначе карточка выглядела бы ничьей.
 */
function IterationPicker({
  iterations,
  current,
  canEdit,
  onChange,
}: {
  iterations: Iteration[]
  current: string | null
  canEdit: boolean
  onChange: (iterationId: string | null) => void
}) {
  const open = iterations.filter((i) => i.closedAt === null)
  const currentIteration = iterations.find((i) => i.id === current)
  if (open.length === 0 && !currentIteration) return null

  if (!canEdit) {
    return (
      <p className="muted small">
        {t.panel.iterationIs(currentIteration ? currentIteration.name : t.panel.iterationNone)}
      </p>
    )
  }

  // Карточку из закрытой итерации не вынуть и в другую не переложить:
  // закрытая итерация — утверждение о том, что было сделано. Раньше
  // список всё равно предлагал выбор, и он кончался двумя отказами
  // подряд — «итерация закрыта», а следом «карточка уже в другой
  // итерации», причём второй был лишь следствием первого. Дверь,
  // которой нет, не предлагаем: говорим словами, почему.
  if (currentIteration?.closedAt) {
    return (
      <p className="muted small">
        {t.panel.iterationClosed(currentIteration.name)}
      </p>
    )
  }

  return (
    <label className="row row--tight">
      <span className="muted small">{t.panel.iteration}</span>
      <select
        value={current ?? ''}
        aria-label={t.panel.iterationOf}
        onChange={(e) => onChange(e.target.value || null)}
      >
        <option value="">{t.panel.noIteration}</option>
        {open.map((i) => (
          <option key={i.id} value={i.id}>
            {i.name}
          </option>
        ))}
      </select>
    </label>
  )
}

/**
 * Оценка карточки.
 *
 * Пустое поле — «не оценена», и это не то же самое, что ноль: прогресс
 * родителя считается весом только когда оценены все подзадачи. Одна
 * неоценённая — и счёт возвращается к штукам, потому что вес ноль молча
 * выкинул бы работу из знаменателя.
 */
function Estimate({
  value,
  unit,
  canEdit,
  onSave,
}: {
  value: number | null
  unit: EstimateUnit
  canEdit: boolean
  onSave: (value: number | null) => void
}) {
  if (!canEdit) {
    return (
      <p className="muted small">
        {t.panel.estimateIs(value === null ? t.panel.estimateNone : `${value} ${UNIT_SHORT[unit]}`)}
      </p>
    )
  }

  // Шаговый ввод вместо числового поля: оценку почти всегда меняют
  // на единицу — «не два, а три», — и печатать другое число ради
  // этого значит делать три действия вместо одного. Набрать число
  // с клавиатуры по-прежнему можно: значение осталось полем.
  return (
    <div className="field-row">
      <span className="field-label">{t.panel.estimate}</span>
      <EstimateStepper value={value} unit={unit} onChange={onSave} />
    </div>
  )
}

/**
 * Свои поля карточки.
 *
 * Показываются все поля организации, а не только заполненные: иначе
 * заполнить новое поле можно было бы, только зная о его существовании
 * заранее. Пустое значение снимает поле — «поля нет» и «поле пустое»
 * это одно и то же.
 */
function Fields({
  fields,
  values,
  canEdit,
  onSet,
}: {
  fields: CardField[]
  values: FieldValue[]
  canEdit: boolean
  onSet: (fieldId: string, value: string | number | boolean | null) => void
}) {
  if (fields.length === 0) return null
  const current = new Map(values.map((v) => [v.fieldId, v.value]))

  return (
    <section className="stack">
      <h3 className="section-title">{t.panel.fields}</h3>
      {fields.map((field) => (
        <FieldRow
          key={field.id}
          field={field}
          value={current.get(field.id) ?? null}
          canEdit={canEdit}
          onSet={(value) => onSet(field.id, value)}
        />
      ))}
    </section>
  )
}

function FieldRow({
  field,
  value,
  canEdit,
  onSet,
}: {
  field: CardField
  value: string | number | boolean | null
  canEdit: boolean
  onSet: (value: string | number | boolean | null) => void
}) {
  const [draft, setDraft] = useState(value === null ? '' : String(value))
  useEffect(() => setDraft(value === null ? '' : String(value)), [value])

  if (!canEdit) {
    return (
      <p className="muted small">
        {field.name}: {value === null ? '—' : String(value)}
      </p>
    )
  }

  if (field.kind === 'checkbox') {
    return (
      <label className="row row--tight">
        <input
          type="checkbox"
          checked={value === true}
          onChange={(e) => onSet(e.target.checked ? true : null)}
        />
        <span className="small">{field.name}</span>
      </label>
    )
  }

  if (field.kind === 'select') {
    return (
      <label className="row row--tight">
        <span className="muted small">{field.name}</span>
        <select
          value={typeof value === 'string' ? value : ''}
          aria-label={field.name}
          onChange={(e) => onSet(e.target.value || null)}
        >
          <option value="">—</option>
          {field.options.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </label>
    )
  }

  const commit = () => {
    const trimmed = draft.trim()
    if (trimmed === '') {
      if (value !== null) onSet(null)
      return
    }
    if (field.kind === 'number') {
      const parsed = Number(trimmed.replace(',', '.'))
      if (!Number.isFinite(parsed)) {
        setDraft(value === null ? '' : String(value))
        return
      }
      if (parsed !== value) onSet(parsed)
      return
    }
    if (trimmed !== value) onSet(trimmed)
  }

  return (
    <label className="row row--tight">
      <span className="muted small">{field.name}</span>
      <input
        type={field.kind === 'date' ? 'date' : 'text'}
        inputMode={field.kind === 'number' ? 'decimal' : undefined}
        value={draft}
        aria-label={field.name}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
      />
    </label>
  )
}

function Description({
  value,
  canEdit,
  onSave,
}: {
  value: string
  canEdit: boolean
  onSave: (text: string) => void
}) {
  const [draft, setDraft] = useState(value)
  // Карточку могли изменить и не мы: описание перечитывается, когда
  // пришёл новый снимок, но не затирает то, что человек уже печатает.
  useEffect(() => setDraft(value), [value])

  // Своя подпись: без неё описание стояло безымянной рамкой сразу
  // за рамкой последнего своего поля и читалось как ещё одно поле —
  // тем более что подпись «Описание» была только в плейсхолдере,
  // а он исчезает от первой буквы.
  return (
    <section className="stack">
      <h3 className="section-title">{t.panel.description}</h3>
      {!canEdit ? (
        value ? (
          <p className="description">{value}</p>
        ) : (
          <p className="muted small">{t.panel.noDescription}</p>
        )
      ) : (
        <textarea
          className="description"
          rows={4}
          value={draft}
          placeholder={t.panel.descriptionPlaceholder}
          aria-label={t.panel.descriptionLabel}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => draft !== value && onSave(draft)}
        />
      )}
    </section>
  )
}

function BlockForm({
  onBlock,
  holder,
  onCancel,
}: {
  onBlock: (reason: string, until?: string) => void
  /** Название части, которая держит: форма открыта не «вообще», а про
   *  неё, и спрашивать надо про неё же. */
  holder?: string
  onCancel?: () => void
}) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [until, setUntil] = useState('')

  // Форма про часть открыта всегда: её открыло нажатие «Держит»,
  // и вторая кнопка «Заблокировать…» была бы вопросом, на который
  // уже ответили. Начальным состоянием это не задать — React держит
  // тот же узел формы, и состояние от прошлого показа переживает
  // появление части.
  if (!open && !holder) {
    return (
      <button className="link" onClick={() => setOpen(true)}>
        {t.panel.blockAsk}
      </button>
    )
  }

  return (
    <form
      // Двумя строками, когда блокирует часть: имя части длинное,
      // и в одном ряду с полем оно сжимает поле до трёх букв.
      className="stack"
      onSubmit={(e) => {
        e.preventDefault()
        if (!reason.trim()) return
        onBlock(reason.trim(), fromLocalInput(until) ?? undefined)
        setReason('')
        setUntil('')
        setOpen(false)
      }}
    >
      {holder && <span className="small">{t.panel.heldByAsk(holder)}</span>}
      <div className="row">
      <input
        autoFocus
        value={reason}
        placeholder={holder ? t.panel.waitingForPart : t.panel.waitingFor}
        aria-label={t.panel.blockReason}
        onChange={(e) => setReason(e.target.value)}
      />
      {/* Срок необязателен, и подпись это говорит: пустое поле —
          «пока не снимут», а не недоделка. */}
      <label className="row row--tight small">
        <span>{t.panel.liftsItself}</span>
        <input type="datetime-local" value={until} onChange={(e) => setUntil(e.target.value)} />
      </label>
      <Hint topic="block" />
      {/* Глагол называет то, что произойдёт. «Отметить» не отвечает
          на вопрос «что отметить» и в ряду с «Отмена» читается как её
          пара — два похожих слова, из которых первое ещё и приглушено,
          пока причина не набрана. */}
      <Button kind="primary" type="submit" disabled={!reason.trim()}>
        {t.panel.block}
      </Button>
      <Button
        kind="quiet"
        type="button"
        onClick={() => {
          setOpen(false)
          onCancel?.()
        }}
      >
        {t.common.cancel}
      </Button>
      </div>
    </form>
  )
}

/**
 * Кто делает.
 *
 * Список, а не одно поле: пара за одной задачей, смежник на день,
 * проверяющий — всё это обычная работа, и до сих пор про неё
 * приходилось врать, дописывая людей в описание, где их не найдёт
 * ни фильтр, ни отчёт.
 *
 * Порядок — назначения, а не алфавитный: первым стоит тот, кто взялся
 * первым, и это единственное, чем список отвечает на вопрос
 * «кто отвечает».
 */
function Assignees({
  people,
  assignees,
  canEdit,
  onAssign,
}: {
  people: Record<string, string>
  assignees: string[]
  canEdit: boolean
  onAssign: (userId: string, on: boolean) => void
}) {
  const free = Object.entries(people).filter(([id]) => !assignees.includes(id))

  return (
    <section className="stack">
      <h3 className="section-title">{t.panel.who}</h3>

      {assignees.length === 0 && (
        <p className="muted small">{t.panel.nobody}</p>
      )}

      {assignees.map((id) => (
        <div className="related" key={id}>
          <div className="row row--tight">
            <Avatar name={people[id] ?? t.common.someone} />
            <span>{people[id] ?? t.common.someone}</span>
          </div>
          {canEdit && (
            <button
              className="link"
              aria-label={t.panel.unassign(people[id] ?? t.panel.someoneLower)}
              onClick={() => onAssign(id, false)}
            >
              {t.panel.lift}
            </button>
          )}
        </div>
      ))}

      {canEdit && free.length > 0 && (
        <select
          value=""
          aria-label={t.panel.addAssignee}
          onChange={(e) => e.target.value && onAssign(e.target.value, true)}
        >
          <option value="">{t.panel.addAssigneePick}</option>
          {free.map(([id, name]) => (
            <option key={id} value={id}>
              {name}
            </option>
          ))}
        </select>
      )}
    </section>
  )
}

/**
 * Дата обязательства.
 *
 * Поля срока у всех подряд в системе нет намеренно: канбан обещает
 * распределение, а не дату, и срок, поставленный каждой карточке,
 * превращается в ритуал переноса дат. Но у части работы дата есть
 * на самом деле — релиз, демонстрация, договор, — и вопрос «успеваем
 * ли к четвергу» про неё задают вслух.
 *
 * Поэтому поле пустое по умолчанию и ничего не подсказывает: пусто
 * означает «обязательства нет», а не «дату забыли».
 */
/**
 * Отметка «сделана».
 *
 * Готовность, объявленная руками и не зависящая от колонки. Нужна
 * разбиению: части вида «согласовать с юристами» по доске не ездят,
 * а вопрос «сделано ли» про них задают — и до сих пор ответить на него
 * можно было только переездом в колонку финиша, то есть обрядом ради
 * счётчика.
 *
 * Поток она не подменяет, и об этом сказано прямо: цикл и пропускная
 * способность считаются точкой финиша, и человек, отметивший часть
 * сделанной, не должен гадать, поехали ли за ней метрики.
 */
function DoneMark({
  doneAt,
  canEdit,
  onChange,
}: {
  doneAt: string | null
  canEdit: boolean
  onChange: (done: boolean) => void
}) {
  const done = doneAt !== null
  if (!canEdit) {
    return (
      <p className="muted small">
        {done
          ? t.panel.markedDoneOn(new Date(doneAt).toLocaleDateString(locale()))
          : t.panel.notMarkedDone}
      </p>
    )
  }

  return (
    <div className="field-row">
      <span className="field-label">{t.panel.readiness}</span>
      <button
        type="button"
        role="checkbox"
        aria-checked={done}
        className="done-mark"
        onClick={() => onChange(!done)}
      >
        <span className={`subtask-box${done ? ' subtask-box--done' : ''}`} aria-hidden="true" />
        <span>{t.panel.doneMark}</span>
      </button>
      <span className="muted small">
        {done ? t.panel.since(new Date(doneAt).toLocaleDateString(locale())) : t.panel.doesNotMove}
      </span>
    </div>
  )
}

function DuePicker({
  value,
  canEdit,
  onChange,
}: {
  value: string | null
  canEdit: boolean
  onChange: (next: string | null) => void
}) {
  if (!canEdit) {
    return (
      <p className="muted small">
        {/* Здесь — и дата, и отсчёт. На доске дату убрали: там вопрос
            один, «успеваем ли». В панели спрашивают ещё и «на какое
            число мы это обещали», а отвечать на него, заставляя
            складывать дни в уме, — издевательство. */}
        {value
          ? t.panel.commitmentIs(dateWords(value), dueLabel(value).text)
          : t.panel.noCommitment}
      </p>
    )
  }

  return (
    <label className="field-row">
      <span className="field-label">{t.panel.commitment}</span>
      <input
        type="date"
        value={value ?? ''}
        aria-label={t.panel.commitmentDate}
        onChange={(e) => onChange(e.target.value || null)}
      />
      {value && (
        <button className="link" aria-label={t.panel.clearCommitment} onClick={() => onChange(null)}>
          {t.panel.lift}
        </button>
      )}
    </label>
  )
}

/**
 * Приоритет.
 *
 * Уровень говорит, что важнее. Порядок карточек в колонке он не трогает
 * и трогать не должен: порядок — решение команды о том, что взято
 * в работу следующим, и подменять его сортировкой значит спорить с тем,
 * что люди сами выставили руками.
 *
 * Четыре уровня, и середина есть намеренно: шкала без середины
 * заставляет выбирать сторону там, где выбирать нечего, и через месяц
 * половина доски оказывается «высокой».
 */
function PriorityPicker({
  value,
  canEdit,
  onChange,
}: {
  value: Priority
  canEdit: boolean
  onChange: (next: Priority) => void
}) {
  if (!canEdit) {
    return <p className="muted small">{t.panel.priorityIs(priorityLabel(value).toLowerCase())}</p>
  }

  return (
    <label className="field-row">
      <span className="field-label">{t.panel.priority}</span>
      <select value={value} onChange={(e) => onChange(e.target.value as Priority)}>
        {PRIORITIES.map((level) => (
          <option key={level} value={level}>
            {PRIORITY_NAMES[level]}
          </option>
        ))}
      </select>
    </label>
  )
}

/**
 * Метки карточки.
 *
 * До сих пор метку можно было повесить только с доски — из меню
 * карточки, а потом нажатием по самим меткам. Открывший карточку
 * за этим возвращался на доску: панель показывает всё о работе,
 * кроме того, чем она помечена.
 *
 * Метка принадлежит организации, подразделению или доске, и здесь это
 * написано рядом с каждой: две «Срочно» из разных мест иначе
 * не различить, а «почему этой нет на соседней доске» не на что
 * ответить. Висящие показываются все, даже убранные и чужие, — снять
 * их больше негде; предлагаются только действующие на доске.
 *
 * Нужной метки нет — её заводят тут же, в поле выбора: уход на «Команду»
 * посреди разговора о карточке стоил пяти шагов и потерянного места.
 */
function Labels({
  boardId,
  labels,
  own,
  canEdit,
  onLabel,
}: {
  boardId: string
  labels: BoardLabel[]
  own: string[]
  canEdit: boolean
  onLabel: (labelId: string, on: boolean) => void
}) {
  const hung = labels.filter((l) => own.includes(l.id))

  return (
    <section className="stack">
      <h3 className="section-title">{t.panel.labels}</h3>

      {/* Наблюдателю указывать не на что: он метки не вешает. Тому, кто
          правит, пустое место не нужно — поле ниже и есть действие. */}
      {hung.length === 0 && !canEdit && <p className="muted small">{t.panel.noLabels}</p>}

      {/* Строкой на метку, как у исполнителей рядом: крестик внутри
          чипа пришлось бы растить до цели нажатия в 24 пикселя,
          и чип с коротким словом раздулся бы вдвое. */}
      {hung.map((label) => (
        <div className="related" key={label.id}>
          <span className={chipClass(label)}>{label.name}</span>
          <span className="muted small related-grow">
            {labelOrigin(label)}
            {label.archived && t.panel.labelArchived}
            {!label.archived && !label.applies && t.panel.labelNotHere}
          </span>
          {canEdit && (
            <button
              className="link"
              aria-label={t.panel.removeLabel(label.name)}
              onClick={() => onLabel(label.id, false)}
            >
              {t.panel.lift}
            </button>
          )}
        </div>
      ))}

      {/* Пустое состояние больше не отправляет на «Команду»: метку,
          которой нет, заводят здесь же — набрав название. */}
      {canEdit && (
        <LabelCombobox
          boardId={boardId}
          labels={labels}
          hung={own}
          canEdit={canEdit}
          inputLabel={t.panel.hangOrCreate}
          quietWhenIdle
          onToggle={onLabel}
        />
      )}
    </section>
  )
}

/**
 * Завести подзадачу.
 *
 * Поле и Enter — как в колонке доски, и намеренно так же: подзадача
 * это обычная карточка, и заводиться она должна тем же движением.
 * Раньше единственным путём было «создать карточку на доске, потом
 * найти её в выпадающем списке и связать» — три действия и переход
 * туда-обратно там, где нужно записать мысль.
 *
 * Колонка не спрашивается: подзадача ложится в начало доски, а перенести
 * её можно потом — как любую карточку.
 */
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

/**
 * Связать с существующей карточкой. Предлагаются только карточки этой доски:
 * связать с чужой можно, но выбирать её здесь не из чего — для этого
 * нужен поиск по организации, а его ещё нет.
 */
function LinkPicker({
  base,
  details,
  onPick,
}: {
  base: BaseState
  details: ReturnType<typeof cardDetails>
  onPick: (toCard: string, kind: LinkKind) => void
}) {
  const [kind, setKind] = useState<LinkKind>('subtask')
  if (!details) return null
  const candidates = candidatesForSubtask(base, details)
  if (candidates.length === 0) return null

  return (
    <details className="link-picker">
      <summary className="muted small">{t.panel.linkExisting}</summary>
      <div className="row row--tight">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as LinkKind)}
          aria-label={t.panel.linkKind}
        >
          {(Object.keys(LINK_KIND_NAMES) as LinkKind[]).map((k) => (
            <option key={k} value={k}>
              {LINK_KIND_NAMES[k]}
            </option>
          ))}
        </select>
        <select
          value=""
          aria-label={t.panel.linkCard}
          onChange={(e) => e.target.value && onPick(e.target.value, kind)}
        >
          <option value="">{t.panel.pickCard}</option>
          {candidates.map((c) => (
            <option key={c.id} value={c.id}>
              {c.title}
            </option>
          ))}
        </select>
      </div>
    </details>
  )
}

/**
 * История карточки.
 *
 * Читается отдельным запросом, а не приходит в снимке: у доски событий
 * тысячи, а нужны они на одной карточке и по требованию. Перечитывается
 * при изменении карточки — версия для того и есть.
 */
function History({
  boardId,
  cardId,
  version,
  fields,
}: {
  boardId: string
  cardId: string
  version: number
  /** Своё поле в журнале названо по имени: без списка полей осталась бы
   *  ссылка `f-17`, а её читать нечем. */
  fields: CardField[]
}) {
  const [events, setEvents] = useState<BoardEvent[] | null>(null)
  const [failed, setFailed] = useState(false)
  const [before, setBefore] = useState<SourceHistory | null>(null)

  useEffect(() => {
    let alive = true
    api
      .boardEvents(boardId, cardId)
      .then((feed) => alive && setEvents(feed.events))
      .catch(() => alive && setFailed(true))
    // История там, откуда карточку перенесли, — своим запросом: у карточки,
    // заведённой здесь, её нет, и отказ не мешает показать нашу.
    api
      .sourceHistory(boardId, cardId)
      .then((h) => alive && setBefore(h))
      .catch(() => alive && setBefore(null))
    return () => {
      alive = false
    }
  }, [boardId, cardId, version])

  if (failed) return <p className="muted small">{t.panel.historyFailed}</p>
  if (!events) return <p className="muted small">{t.panel.historyLoading}</p>

  return (
    <section className="stack">
      <h3 className="section-title">{t.panel.history}</h3>
      <ul className="feed">
        {events.map((e) => (
          <li key={e.id}>
            <span>{eventText(e, fields)}</span>
            <span className="muted small">
              {actorText(e.actor)} · {timeText(e.at)}
            </span>
          </li>
        ))}
      </ul>
      {/* До переноса — отдельным блоком и по времени событий: это рассказ
          о том, что было там, и смешивать его с нашим журналом значило бы
          выдать чужие действия за сделанные здесь (ROADMAP 23.7). */}
      {before && (before.entries.length > 0 || before.pending) && (
        <>
          <h3 className="section-title">{t.panel.historyBefore(sourceName(before.source))}</h3>
          {before.pending && <p className="muted small">{t.panel.historyPending}</p>}
          <ul className="feed">
            {before.entries.map((e, i) => (
              <li key={i}>
                <span>{e.text}</span>
                <span className="muted small">
                  {e.author ? `${e.author} · ` : ''}
                  {timeText(e.at)}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
