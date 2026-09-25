import { memo, useEffect, useRef, useState } from 'react'
import { combine } from '@atlaskit/pragmatic-drag-and-drop/combine'
import { setCustomNativeDragPreview } from '@atlaskit/pragmatic-drag-and-drop/element/set-custom-native-drag-preview'
import { preserveOffsetOnSource } from '@atlaskit/pragmatic-drag-and-drop/element/preserve-offset-on-source'
import { draggable, dropTargetForElements } from '@atlaskit/pragmatic-drag-and-drop/element/adapter'
import {
  attachClosestEdge,
  extractClosestEdge,
} from '@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge'
import type { Edge } from '@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge'
import { ageLabel, agingLabel } from '../../entities/board/model.ts'
import {
  PRIORITIES,
  PRIORITY_NAMES,
  dueIsBurning,
  blockUntilLabel,
  dueLabel,
  priorityLabel,
  priorityShort,
  blockedParts,
  blockedPartsLabel,
  progressLabel,
  progressRatio,
  directPartsLabel,
  deepStuckLabel,
  unitLabel, epicTone } from '../../entities/card/model.ts'
import type { Related } from '../../entities/card/model.ts'
import { labelTitle, chipClass } from '../../entities/label/model.ts'
import { LabelPickerButton } from './LabelPicker.tsx'
import type { BoardLabel, Card, Column, EstimateUnit, Priority } from '../../shared/api/index.ts'
import { AVATAR_SMALL, Avatar, AvatarMore } from '../../shared/ui/Avatar.tsx'
import { EditableText } from '../../shared/ui/EditableText.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import { SubtaskRow } from './SubtaskRow.tsx'
import { boardPath, navigate } from '../../shared/router/index.ts'
import {
  ArchiveIcon,
  BlockedIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  EditIcon,
  MoreIcon,
  MoveIcon,
  PlusIcon,
  PeopleIcon,
  TagIcon,
  TrashIcon,
} from '../../shared/ui/icons.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Карточка на доске.
 *
 * Вынесена из экрана доски по двум причинам, и вторая важнее первой.
 * Первая — размер: доска на полторы тысячи строк не читается целиком,
 * а значит правится вслепую. Вторая — перерисовки: на доске в пятьсот
 * карточек любое изменение одной из них перерисовывало все пятьсот,
 * и отдельный файл даёт место, где это можно и остановить (memo),
 * и померить.
 */
type CardProps = {
  /** Может ли смотрящий менять доску. У наблюдателя карточка
   *  не показывает ни меню, ни флажка выделения: сервер их всё равно
   *  отвергает, а меню, ведущее к отказу, — обещание, которого
   *  интерфейс не держит. */
  canEdit: boolean
  cardId: string
  columnId: string
  card: Card | undefined
  unit: EstimateUnit
  /** Обещание доски: с ним сравнивается возраст карточки. */
  sleDays: number | null
  flash: boolean
  /** userId → имя: карточка хранит идентификатор, показать надо имя. */
  people: Record<string, string>
  /** Кто делает: идентификаторы в порядке назначения. */
  assignees: string[]
  /** on = назначить, off = снять: исполнителей несколько, и «назначить
   *  никому» больше не имеет смысла. */
  onAssign: (cardId: string, userId: string, on: boolean) => void
  labels: BoardLabel[]
  boardId: string
  cardLabels: string[]
  /** Родительская задача, если карточка — чья-то подзадача. */
  parent?: { id: string; title: string; onThisBoard: boolean }
  /** Итерация, к которой карточка отнесена сейчас. Название, а не
   *  идентификатор: карточка показывает, а не ищет. */
  iteration?: string
  /** Итерация кончилась, а карточка не сделана. */
  iterationLate?: boolean
  iterationEnd?: string
  /** Менялась другими после прошлого захода смотрящего: кто и когда. */
  changed?: string
  /** Подзадачи этой карточки. Раскрываются по кнопке прямо на доске:
   *  до этого разбиение работы было видно только числом «0 из 3»,
   *  а чтобы узнать, на что именно она разбита, карточку приходилось
   *  открывать. */
  subtasks: Related[]
  /** Кого эта карточка держит и кто держит её. Обе стороны связи
   *  «блокирует»: без первой не видно, что работа держит чужую,
   *  без второй — почему она сама стоит. */
  holds: Related[]
  waitsFor: Related[]
  onLabel: (cardId: string, labelId: string, on: boolean) => void
  /** Нажатие на метку эпика — отбор доски по нему (этап 33.3). */
  onEpic: (epicId: string) => void
  /** Выделена ли карточка для действия над многими сразу. */
  selected: boolean
  /** `extend` — shift-щелчок: взять всё между прошлым флажком и этим. */
  onSelect: (cardId: string, on: boolean, extend?: boolean) => void
  /** Уровень приоритета. Порядок карточек в колонке он не трогает:
   *  уровень говорит, что важнее, порядок — что взято следующим. */
  onPrioritise: (cardId: string, priority: Priority) => void
  onBlock: (cardId: string, reason: string) => void
  onUnblock: (cardId: string) => void
  /** Отметить работу сделанной, не двигая её по доске. Нужно частям:
   *  «согласовать с юристами» не ездит по колонкам, а вопрос «сделано
   *  ли» про неё задают. Поток от отметки не меняется. */
  onMarkDone: (cardId: string, done: boolean) => void
  /** Завести часть этой работы. Доска — эта же: части у соседей
   *  заводят в панели, там же выбирают чью. */
  onSubtask: (parentCardId: string, title: string) => void
  columns: Column[]
  onMoveToColumn: (cardId: string, columnId: string) => void
  /** Открыть карточку. Идентификатор аргументом, а не в замыкании:
   *  замыкание делало бы обработчик своим у каждой карточки. */
  onOpen: (cardId: string) => void
  onMoveByKeyboard: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  /** Перейти к соседней карточке — стрелка без модификатора. */
  onNavigate: (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => void
  onRename: (cardId: string, title: string) => void
  onArchive: (cardId: string) => void
  /** Удалить насовсем. Пусто — значит нельзя: право владельца выражено
   *  отсутствием обработчика, а не спрятанным пунктом меню, который
   *  ответит отказом. */
  onDelete?: (cardId: string, title: string) => void
}

function CardViewInner({
  cardId,
  columnId,
  card,
  unit,
  sleDays,
  flash,
  people,
  assignees,
  onAssign,
  labels,
  boardId,
  cardLabels,
  parent,
  iteration,
  iterationLate,
  iterationEnd,
  changed,
  subtasks,
  holds,
  waitsFor,
  onLabel,
  onEpic,
  selected,
  onSelect,
  onPrioritise,
  onBlock,
  onUnblock,
  onMarkDone,
  onSubtask,
  columns,
  onMoveToColumn,
  onOpen,
  onMoveByKeyboard,
  onNavigate,
  onRename,
  canEdit,
  onArchive,
  onDelete,
}: CardProps) {
  const title = card?.title ?? '…'
  const ref = useRef<HTMLElement>(null)
  const [dragging, setDragging] = useState(false)
  const [edge, setEdge] = useState<Edge | null>(null)
  const [editing, setEditing] = useState(false)
  // Причина блокировки пишется на самой карточке: чего ждём — вопрос
  // к работе, а не к её карточке, и уходить за ним в панель значит
  // терять доску из виду ради одной строки.
  const [blocking, setBlocking] = useState(false)
  // Заведение части прямо с доски: работу разбивают тогда же, когда
  // на неё смотрят, а не отдельным заходом в панель.
  const [adding, setAdding] = useState(false)
  // Раскрытие подзадач — состояние самой карточки и живёт с ней: это
  // ответ на «что здесь внутри», заданный один раз и здесь же, а не
  // настройка, которую человек ждёт увидеть завтра такой же.
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const element = ref.current
    if (!element) return
    // Наблюдатель карточку не таскает: перенос кончился бы отказом
    // сервера, а карточка, вернувшаяся на место после броска, читается
    // как поломка, а не как «вам нельзя».
    if (!canEdit) return
    const data = { kind: 'card', cardId, columnId }
    return combine(
      draggable({
        element,
        getInitialData: () => data,
        // Своё превью вместо браузерного. Браузер тащит полупрозрачный
        // снимок всего узла — вместе с раскрытым меню и рамкой фокуса,
        // если они были; получается мутный прямоугольник, по которому
        // не видно, что именно летит. Тут летит сама карточка,
        // уменьшенная и повёрнутая на пару градусов: наклон отличает
        // «взятое в руку» от «лежащего на доске».
        onGenerateDragPreview: ({ nativeSetDragImage, location }) => {
          setCustomNativeDragPreview({
            nativeSetDragImage,
            // Превью держится там, где карточку взяли: иначе она
            // прыгает под курсор углом и ощущается вырванной.
            getOffset: preserveOffsetOnSource({ element, input: location.current.input }),
            render: ({ container }) => {
              const copy = element.cloneNode(true) as HTMLElement
              copy.classList.add('card--preview')
              copy.style.width = `${element.offsetWidth}px`
              container.append(copy)
            },
          })
        },
        onDragStart: () => setDragging(true),
        onDrop: () => setDragging(false),
      }),
      dropTargetForElements({
        element,
        canDrop: ({ source }) => source.data.kind === 'card',
        getData: ({ input, element }) =>
          attachClosestEdge(data, { input, element, allowedEdges: ['top', 'bottom'] }),
        onDrag: ({ self }) => setEdge(extractClosestEdge(self.data)),
        onDragLeave: () => setEdge(null),
        onDrop: () => setEdge(null),
      }),
    )
  }, [cardId, columnId])

  // Возраст против обещания доски — единственный случай, когда карточка
  // получает третью метку. Считается на отрисовке: хранить «просрочена»
  // значит завести поле, которое устаревает само по себе.
  const aging = card ? agingLabel(card, sleDays) : null
  // Сколько идёт — у каждой начатой карточки, тихо. Неначатая
  // не стареет, она ждёт, и числа у неё нет.
  const age = card ? ageLabel(card) : null
  // Двое — обычный случай, четверо — уже толпа: показываем троих
  // и число остальных, как и с метками. Порядок — назначения: первым
  // стоит тот, кто взялся первым.
  const shownAssignees = assignees.slice(0, 3)
  const hiddenAssignees = assignees.length - shownAssignees.length
  const own = labels.filter((l) => cardLabels.includes(l.id))
  // Первая метка переносит выбор из верхней строки в строку меток:
  // кнопка «+ метка» исчезает, а фокус обязан вернуться туда, откуда
  // открывали, — на новую кнопку, а не на `body`.
  const labelsFrom = useRef(false)
  useEffect(() => {
    if (!labelsFrom.current || own.length === 0) return
    labelsFrom.current = false
    ref.current?.querySelector<HTMLElement>('.field.card-labels')?.focus()
  }, [own.length])

  /**
   * Одна тревога на карточку.
   *
   * Предупреждающих пометок стало четыре — блокировка, горящий срок,
   * возраст сверх обещания, верхний уровень, — и все они кричали одним
   * цветом. На доске, где предупреждающим помечено полкартинки,
   * предупреждение не работает: когда красное всё, красное не значит
   * ничего.
   *
   * Пометки сравнимы между собой, и порядок очевиден: заблокированную
   * разблокируют раньше, чем обсуждают её возраст, а обещанное наружу
   * важнее собственного обещания доски. Показывается старшая,
   * остальные живут в панели и в отборах.
   */
  const due = card?.dueOn ? dueLabel(card.dueOn) : null
  // Часть, которая стоит, останавливает и целое: разбили работу, одна
  // часть упёрлась — задача не идёт. Раньше об этом знала только сама
  // часть, а с доски работа выглядела идущей.
  const stuckParts = blockedParts(subtasks)
  // И глубже прямых частей: задача внука, упёршаяся под фичей, стоит
  // и эпик (этап 32.3). Счёт приносит сервер — внуков на доске может
  // не быть вовсе.
  const deepStuck = card ? deepStuckLabel(card, stuckParts.length) : null
  // Срок блокировки — часть той же тревоги, а не новое поле: бюджет
  // полей карточки жёсткий. Меньше суток до срока — пометка меняет вид:
  // это и есть уведомление продукта, который никуда не пишет.
  const until = card?.blocked?.until ? blockUntilLabel(card.blocked.until) : null
  const alarm = card?.blocked
    ? {
        kind: 'blocked',
        text: t.cardView.blockedFor(card.blocked.reason),
        title: card.blocked.reason,
      }
    : stuckParts.length > 0 || deepStuck
      ? {
          kind: 'blocked',
          text: deepStuck ?? blockedPartsLabel(stuckParts) ?? '',
          title: [...stuckParts.map((s) => s.title), card?.subtree?.stuckTitle]
            .filter(Boolean)
            .join(', '),
        }
      : // Горит, а не «подходит»: тревогу занимает только сегодняшнее
      // и просроченное. Срок через два-три дня остаётся тихой
      // пометкой — иначе он вытесняет с доски старение, которое
      // и есть главный сигнал канбана.
      due && dueIsBurning(card?.dueOn ?? null)
      ? { kind: 'due', text: t.cardView.due(due.text), title: t.cardView.commitmentDate }
      : aging
        ? { kind: 'aging', text: aging, title: t.cardView.ageFromStart }
        : null
  // Кто делает части — объединение по ним же, без повторов и в их
  // порядке. Считается из своих подзадач, а не из снимка доски:
  // частей у карточки единицы, а знание о снимке сломало бы memo.
  const subtaskAssignees = [...new Set(subtasks.flatMap((s) => s.assignees))]

  const onKeyDown = (e: React.KeyboardEvent) => {
    // Клавиши карточки работают, только когда выделена сама карточка.
    // Внутри неё есть и поля, и меню: стрелка в открытом меню обязана
    // ходить по пунктам, а «е» в оценке — печататься, а не открывать
    // переименование. Раньше это было написано только в комментарии.
    if (e.target !== e.currentTarget) return

    const arrows: Record<string, 'left' | 'right' | 'up' | 'down'> = {
      ArrowLeft: 'left',
      ArrowRight: 'right',
      ArrowUp: 'up',
      ArrowDown: 'down',
    }
    const direction = arrows[e.key]

    // Со стрелками разница между «перенести» и «перейти» — модификатор:
    // так же устроены все списки, в которых можно и ходить, и двигать.
    if (direction) {
      e.preventDefault()
      if (e.ctrlKey || e.metaKey) onMoveByKeyboard(cardId, direction)
      else onNavigate(cardId, direction)
      return
    }

    if (e.ctrlKey || e.metaKey || e.altKey) return

    // Буквы работают только тогда, когда выделена сама карточка: иначе
    // они перехватывали бы ввод в поле переименования, которое живёт
    // внутри неё же.
    if (e.key === 'Enter') {
      e.preventDefault()
      onOpen(cardId)
      return
    }
    if (e.key === 'e' || e.key === 'у') {
      e.preventDefault()
      setEditing(true)
    }
  }

  /**
   * Клик по карточке открывает её.
   *
   * Так устроены все доски, которыми пользуются: карточка выглядит как
   * то, что можно нажать, — значит по нажатию должна открываться.
   * До сих пор открывали через меню, и это первое, обо что спотыкался
   * каждый, кто видел доску впервые.
   *
   * Не открываем в трёх случаях, и каждый из них настоящий: нажали
   * на кнопку внутри карточки (у неё своё действие), выделяли текст
   * мышью (человек читал, а не переходил), карточку тащат.
   */
  const onClick = (e: React.MouseEvent) => {
    if (dragging || e.defaultPrevented) return
    const target = e.target as HTMLElement
    if (target.closest('button, a, input, textarea, select, [role="menu"]')) return
    const selection = window.getSelection()
    if (selection && !selection.isCollapsed) return
    onOpen(cardId)
  }

  // Кто делает — правится нажатием по самой стопке. Пункт на человека,
  // и он же снимает: два списка «назначить» и «снять» вдвое длиннее
  // и заставляют помнить, кто где. Назначенные стоят в строке названия,
  // пустое «+ кто» — значком среди кнопок по наведению.
  const assigneePicker = canEdit ? (
    <Menu
      label={
        assignees.length > 0
          ? t.cardView.assigneesOf(assignees.map((id) => people[id] ?? t.common.someone).join(', '))
          : t.cardView.noAssignees
      }
      className={assignees.length === 0 ? 'btn btn--icon btn--quiet card-slot' : 'field'}
      align="right"
      items={Object.entries(people).map(([id, name]) => ({
        label: name,
        checked: assignees.includes(id),
        onSelect: () => onAssign(cardId, id, !assignees.includes(id)),
      }))}
    >
      {assignees.length === 0 ? (
        <PeopleIcon />
      ) : (
        <span className="avatars">
          {shownAssignees.map((id) => (
            <Avatar key={id} name={people[id] ?? t.common.someone} />
          ))}
          {hiddenAssignees > 0 && <AvatarMore count={hiddenAssignees} />}
        </span>
      )}
    </Menu>
  ) : null

  return (
    <article
      ref={ref}
      // Отмеченная сделанной приглушается и зачёркивается — тем же
      // способом, что строка части: одним цветом состояние отличать
      // нельзя. Отдельной пометки в ряду не заводим: бюджет пометок
      // карточки занят блокировкой и прогрессом, а «сделана» — это
      // состояние названия, а не ещё одно свойство работы.
      className={`card${dragging ? ' card--dragging' : ''}${edge ? ` card--edge-${edge}` : ''}${flash ? ' card--flash' : ''}${selected ? ' card--selected' : ''}${card?.doneAt ? ' card--done' : ''}`}
      tabIndex={0}
      data-card={cardId}
      role="group"
      // Подсказка читается скринридером при переходе на карточку —
      // это единственное место, где о сокращениях можно сказать тому,
      // кто не видит экрана.
      aria-label={t.cardView.aria(title)}
      onKeyDown={onKeyDown}
      onClick={onClick}
    >
      {editing ? (
        <EditableText
          value={title}
          autoFocus
          label={t.cardView.titleLabel}
          onSave={(next) => {
            onRename(cardId, next)
            setEditing(false)
          }}
          onCancel={() => setEditing(false)}
          className="card-title"
        />
      ) : (
        <>
          {/* Первая строка — номер и название (шаг 2 нового дизайна):
              номер отдельной строкой над названием стоил строки на каждой
              карточке доски ради подписи, которую читают изредка. Чья это
              часть и эпик — во второй строке, рядом с метками. */}
          <div className="card-meta">
            {card && (
              // Не кнопка и не ссылка: номер выделяют и копируют,
              // а нажатие на карточку и так её открывает.
              <span className="card-number">{card.number}</span>
            )}
            {/* Точка, а не рамка и не фон: рамку уже занимают выделение
                и место броска, фон — сделанная. Кто и когда — в подсказке
                и диктору; точка гаснет, как только карточку открыли. */}
            {changed && (
              <span className="card-changed" title={changed}>
                <span className="sr-only">{changed}</span>
              </span>
            )}

            {/* Заголовок — кнопка: у нажимаемой карточки должна быть
                явная цель и для скринридера, и для клавиатуры. Двойного
                клика для переименования больше нет — он спорил
                с открытием; переименование осталось в меню и на «E». */}
            <button className="card-title" onClick={() => onOpen(cardId)}>
              {title}
            </button>
            {/* Кто делает — справа от названия, как в макете плотной
                доски: «что за работа» и «кого спрашивать» читают вместе. */}
            {assignees.length > 0 && assigneePicker}
          </div>

          {/* Вторая строка — всё, что о карточке известно, кроме её
              названия (новый дизайн доски, шаг 2: «плотная карточка
              в две строки»). Прежде номер, название, метки и пометки
              шли четырьмя рядами, и колонка вмещала вдвое меньше работы.
              Строка есть всегда: исполнители и меню стоят в ней справа,
              и появление «…» по наведению не меняет высоту карточки. */}
          <div className="card-line">
            {/* Метка эпика (этап 33.3): цвет из эпика, одинаковый у всех его
                задач, название обрезается, полное — в подсказке. Нажатие
                отбирает доску по эпику: «что ещё идёт ради него». */}
            {card?.epic && (
              <button
                type="button"
                className={`chip chip--${epicTone(card.epic.id)} card-epic`}
                title={card.epic.title}
                aria-label={t.cardView.epicFilter(card.epic.title)}
                onClick={(e) => {
                  e.stopPropagation()
                  if (card.epic) onEpic(card.epic.id)
                }}
              >
                {card.epic.title}
              </button>
            )}
            {/* Родитель-эпик уже назван меткой — второй раз строкой его
                не повторяем. */}
            {parent && parent.id !== card?.epic?.id && (
              <span className="card-parent">
                {/* Стрелка объясняет связь глазу, а диктору не говорит
                    ничего: без слова он читал два названия подряд, будто
                    у карточки два имени. Слово то же, что в панели, —
                    подзадача, а не «часть». */}
                <span aria-hidden="true">↳ </span>
                <span className="sr-only">{t.cardView.subtaskOf}</span>
                {parent.onThisBoard ? (
                  <button className="link" onClick={() => onOpen(parent.id)}>
                    {parent.title}
                  </button>
                ) : (
                  // Родитель на чужой доске: назвать можем, открыть — нет.
                  <span className="muted">{parent.title}</span>
                )}
              </span>
            )}

            {/* Метки — текстом (просьба владельца 22.09.2026: «текст меток
                должен отображаться»). Прежде стояли точки: цвет читается
                быстрее, но что за метка, по точке не узнать, а на доске
                после переноса меток десятки. Три — и «+N»: строка чипов
                не должна вырастать в абзац. Нажатие правит метки. */}
            {own.length > 0 &&
              (canEdit ? (
                <LabelPickerButton
                  label={t.cardView.labelsOf(own.map((l) => l.name).join(', '))}
                  className="field card-labels"
                  align="left"
                  boardId={boardId}
                  labels={labels}
                  hung={cardLabels}
                  canEdit={canEdit}
                  onToggle={(labelId, on) => onLabel(cardId, labelId, on)}
                >
                  <LabelChips labels={own} />
                </LabelPickerButton>
              ) : (
                <div className="card-labels">
                  <LabelChips labels={own} />
                </div>
              ))}
            {card &&
              (alarm ||
                iteration ||
                due ||
                card.priority !== 'medium' ||
                card.estimate !== null) && (
              <div className="card-marks">
                {/* Старшая тревога — одна и всегда первой: она отвечает
                    на вопрос «почему эта работа не идёт», а он важнее
                    остальных. */}
                {alarm && (
                  <span
                    className={`mark mark--alarm${alarm.kind === 'aging' ? ' mark--aging' : ''}`}
                    title={alarm.title}
                  >
                    {alarm.text}
                  </span>
                )}
                {/* Срок — своей строкой под тревогой, а не её хвостом:
                    пометка держит две строки, и хвост уходил в многоточие
                    ровно там, где он нужен. */}
                {until && (
                  <span className={`card-block-until${until.soon ? ' card-block-until--ending' : ''}`}>
                    {until.expired ? until.text : t.cardView.liftsIn(until.text)}
                  </span>
                )}

                {/* Приоритет — решение человека, и регистр цвета у него
                    свой: тёмное поставил кто-то, кирпичное случилось
                    само. Легенды для этого не нужно.
                    Средний уровень не пишется вовсе — ни словом,
                    ни местом под него: умолчание у каждой второй карточки
                    не информация. Такой карточке уровень ставят из меню
                    «…» и из панели. */}
                {card.priority !== 'medium' && (
                  <Menu
                    // Видимое слово стоит в имени первым: голосовое
                    // управление ищет по тому, что человек прочёл
                    // (WCAG 2.5.3). Полное имя рядом — чтобы диктору
                    // было понятно, о какой шкале речь.
                    label={t.cardView.priorityOf(
                      priorityShort(card.priority),
                      priorityLabel(card.priority).toLowerCase(),
                    )}
                    className="field"
                    align="left"
                    items={PRIORITIES.map((level) => ({
                      label: PRIORITY_NAMES[level],
                      checked: card.priority === level,
                      onSelect: () => onPrioritise(cardId, level),
                    }))}
                  >
                    <span className={`priority-mark priority-mark--${card.priority}`}>
                      {priorityShort(card.priority)}
                    </span>
                  </Menu>
                )}

                {/* Срок, который ещё не жмёт, — тихая пометка: он отвечает
                    на «к чему это привязано», а не «почему это горит».
                    Не показывается только тогда, когда он сам стал
                    тревогой: повторять его дважды в одном ряду незачем.
                    А вот у заблокированной карточки срок остаётся здесь,
                    даже горящий: тревога занята блокировкой, но знать,
                    что при этом горит дата, важно именно ей. */}
                {due && alarm?.kind !== 'due' && (
                  <span className="card-due" title={t.cardView.commitmentDate}>
                    {t.cardView.dueWord} {due.text}
                  </span>
                )}

                {/* Итерация — тоже про «к чему привязано». */}
                {iteration && (
                  <span
                    className={iterationLate ? 'mark mark--alarm' : 'mark mark--quiet'}
                    title={iterationLate && iterationEnd ? t.cardView.iterationLate(iterationEnd) : t.cardView.iteration}
                  >
                    {iteration}
                  </span>
                )}

                {/* Оценка — цифра, и тихая: она нужна в разговоре
                    о загрузке, а не при поиске работы глазами. Единица
                    одна на всю доску, и повторять её триста раз незачем
                    — она в подсказке. */}
                {card.estimate !== null && (
                  <span
                    className="card-estimate"
                    title={t.cardView.estimateOf(card.estimate, unitLabel(card.estimate, unit))}
                  >
                    {card.estimate}
                  </span>
                )}

              </div>
            )}
            <span className="card-line-end">
              {/* Сколько идёт — последним и у самого края: это число
                  ищут взглядом по колонке, сравнивая карточки между
                  собой, а не читают в строке слева направо. У края
                  оно выстраивается в столбик само.

                  Не показывается, когда превышение уже стало тревогой:
                  «11 дн.» рядом с «Идёт 11 дн. — дольше обещанных 3»
                  — это одно и то же число дважды. */}
              {age && alarm?.kind !== 'aging' && (
                <span className="card-age" title={t.cardView.runningSinceStart}>
                  {age}
                </span>
              )}
              {/* Кнопки, которые нужны по наведению, — «+ метка»,
                  флажок выделения и «…», — ложатся поверх правого нижнего угла
                  карточки, а не в строку: место под ними в строке съедало
                  треть ширины и уносило возраст и людей на третий ряд.
                  Поверх — значит, высота карточки от наведения не меняется,
                  и соседние карточки не уезжают из-под курсора. */}
              <span className="card-tools">
                {/* Никого нет — «+ кто» значком по наведению, как «+ метка». */}
                {assignees.length === 0 && assigneePicker}
                {/* Метки, которых ещё нет, заводятся отсюда же — значком
                    рядом с меню: он появляется по наведению вместе с «…»,
                    и место под ним занято всегда. Текстом «+ метка» в начале
                    второй строки он сдвигал вправо всё, что в ней стоит. */}
                {canEdit && own.length === 0 && (
                  <LabelPickerButton
                    label={t.cardView.noLabels}
                    className="btn btn--icon btn--quiet card-slot"
                    align="right"
                    boardId={boardId}
                    labels={labels}
                    hung={cardLabels}
                    canEdit={canEdit}
                    onToggle={(labelId, on) => {
                      labelsFrom.current = on
                      onLabel(cardId, labelId, on)
                    }}
                  >
                    <TagIcon />
                  </LabelPickerButton>
                )}
                {/* Флажок выделения. Виден по наведению и пока выделение
                    идёт — на доске в пятьсот карточек пятьсот флажков
                    читаются как разлинованный список, а не как работа.
                    Родной флажок, а не своя картинка: он умеет пробел,
                    читается диктором и уже растянут до цели нажатия
                    общим правилом. */}
                {canEdit && (
                <input
                  type="checkbox"
                  className="card-check"
                  checked={selected}
                  aria-label={t.cardView.select(title)}
                  // Выделение снимается с нажатия, а не с изменения: shift
                  // живёт в событии мыши, а `change` у флажка модификаторов
                  // не несёт вовсе — на этом диапазон и не работал. Пробел
                  // с клавиатуры тоже приходит нажатием, только без shift,
                  // и остаётся обычным переключением.
                  onClick={(e) => onSelect(cardId, e.currentTarget.checked, e.shiftKey)}
                  // Управляемому полю нужен обработчик изменения, иначе React
                  // ругается на «поле только для чтения»; сама правка идёт
                  // выше, по нажатию.
                  onChange={() => {}}
                />
                )}
                {/* Одно меню вместо ряда кнопок: три подписи в ширину колонки
                    не помещались и обрезались до «Откры», «Переиме», «Удалит».
                    Осталось в нём то, у чего на карточке нет своего места:
                    люди, метки и уровень ушли к самим людям, меткам
                    и уровню. Перенос стоит здесь — это не удобство,
                    а требование WCAG 2.5.7: клавиатурного эквивалента
                    недостаточно, нужен путь, выполнимый одним нажатием.

                    Стоит меню в верхней строке, а не отдельным рядом внизу,
                    и это не про красоту. Ряд, появляющийся по наведению,
                    менял высоту карточки — и соседние карточки уезжали
                    из-под курсора между нажатием и отпусканием: попасть
                    по флажку соседа было нельзя. Здесь строка уже занята
                    и её высота от наведения не зависит. */}
                {canEdit && (
                <Menu
                  label={t.cardView.actions(title)}
                  className="btn btn--icon btn--quiet card-slot"
                  items={[
                    { label: t.cardView.rename, icon: <EditIcon />, onSelect: () => setEditing(true) },
                    // Верх шкалы переключается прямо с доски: «это горит»
                    // говорят чаще, чем меняют что-либо ещё, а вся шкала
                    // живёт в панели.
                    card?.priority === 'highest'
                      ? {
                          label: t.cardView.backToMedium,
                          icon: <ClockIcon />,
                          onSelect: () => onPrioritise(cardId, 'medium'),
                        }
                      : {
                          label: t.cardView.toHighest,
                          icon: <ClockIcon />,
                          onSelect: () => onPrioritise(cardId, 'highest'),
                        },
                    // Отметка о готовности доступна не только частям:
                    // связь могут снять, и снимать отметку после этого
                    // было бы неоткуда. Слово «сделана» — про работу,
                    // а не про переезд: колонка карточки не меняется.
                    card?.doneAt
                      ? {
                          label: t.cardView.unmarkDone,
                          icon: <CheckIcon />,
                          onSelect: () => onMarkDone(cardId, false),
                        }
                      : {
                          label: t.cardView.markDone,
                          icon: <CheckIcon />,
                          onSelect: () => onMarkDone(cardId, true),
                        },
                    {
                      label: t.cardView.addSubtask,
                      icon: <PlusIcon />,
                      onSelect: () => {
                        setOpen(true)
                        setAdding(true)
                      },
                    },
                    card?.blocked
                      ? {
                          label: t.cardView.unblock,
                          icon: <BlockedIcon />,
                          onSelect: () => onUnblock(cardId),
                        }
                      : {
                          // Причину пишут словами: список готовых
                          // формулировок отвечает не на тот вопрос — важно,
                          // чего ждём именно здесь.
                          label: t.cardView.block,
                          icon: <BlockedIcon />,
                          onSelect: () => setBlocking(true),
                        },
                    ...columns
                      .filter((c) => c.id !== columnId)
                      .map((c) => ({
                        label: t.cardView.moveTo(c.name),
                        icon: <MoveIcon />,
                        onSelect: () => onMoveToColumn(cardId, c.id),
                      })),
                    {
                      label: t.cardView.archive,
                      icon: <ArchiveIcon />,
                      danger: true,
                      onSelect: () => onArchive(cardId),
                    },
                    // Необратимое стоит последним и спрашивает подтверждение
                    // — в отличие от архивации, которая не спрашивает ничего
                    // и предлагает вернуть.
                    ...(onDelete
                      ? [
                          {
                            label: t.cardView.deleteForever,
                            icon: <TrashIcon />,
                            danger: true,
                            onSelect: () => onDelete(cardId, title),
                          },
                        ]
                      : []),
                  ]}
                >
                  <MoreIcon />
                </Menu>
                )}
              </span>
            </span>
          </div>

          {/* Обе стороны зависимости — одинаковыми строками. Раньше
              держащая сообщала только число, и «какую именно задачу
              она держит» приходилось выяснять, открыв карточку:
              то есть ровно тем способом, от которого эта строка
              и должна избавлять. */}
          <Dependency label={t.cardView.waits} cards={waitsFor} onOpen={onOpen} />
          <Dependency label={t.cardView.holds} cards={holds} onOpen={onOpen} />

          {blocking && (
            <EditableText
              value=""
              autoFocus
              label={t.cardView.blockReason}
              placeholder={t.cardView.waitingFor}
              onSave={(reason) => {
                // Пустая причина не блокирует: блокировка без причины
                // не отличается от карточки, которая просто стоит.
                if (reason.trim()) onBlock(cardId, reason.trim())
                setBlocking(false)
              }}
              onCancel={() => setBlocking(false)}
              className="card-block-reason"
            />
          )}
          {/* Подзадачи — полосой, а не строчкой среди пометок: «0 из 1»
              в общем ряду читалось как ещё одна пометка, и по доске
              нельзя было понять, у каких задач работа разбита и как
              далеко она ушла. */}
          {card?.progress && card.progress.total > 0 && (
            <div className="card-progress">
              {/* Полоса — она же кнопка раскрытия, если подзадачи видны
                  отсюда. Отдельная кнопка рядом с полосой означала бы две
                  цели нажатия про одно и то же в ширину колонки; здесь
                  сама мера разбиения и есть путь внутрь него. */}
              {subtasks.length > 0 ? (
                <button
                  className="card-progress-toggle"
                  aria-expanded={open}
                  aria-controls={`subtasks-${cardId}`}
                  // Мера разбиения переезжает в имя кнопки, а сама полоса
                  // становится картинкой. Вложить progressbar внутрь
                  // кнопки нельзя: спецификация ARIA объявляет содержимое
                  // кнопки представлением, и роль внутри неё пропадает —
                  // мера просто перестала бы читаться вслух.
                  aria-label={t.cardView.subtasksToggle(progressLabel(card, unit) ?? '', open)}
                  onClick={() => setOpen(!open)}
                >
                  {open ? <ChevronDownIcon /> : <ChevronRightIcon />}
                  <span className="progress" aria-hidden="true">
                    <span
                      className="progress-fill"
                      style={{ transform: `scaleX(${progressRatio(card)})` }}
                    />
                  </span>
                  {/* Полоса одна и мерит всё поддерево (этап 33.4); прямые
                      части — в подсказке, для того, кто спросит. */}
                  <span className="muted small" aria-hidden="true" title={directPartsLabel(card, unit) ?? undefined}>
                    {progressLabel(card, unit)}
                  </span>
                </button>
              ) : (
                <>
                  <div
                    className="progress"
                    role="progressbar"
                    aria-valuenow={Math.round(progressRatio(card) * 100)}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-label={t.cardView.subtasksDone(progressLabel(card, unit) ?? '')}
                  >
                    <div
                      className="progress-fill"
                      style={{ transform: `scaleX(${progressRatio(card)})` }}
                    />
                  </div>
                  <span className="muted small" title={directPartsLabel(card, unit) ?? undefined}>
                    {progressLabel(card, unit)}
                  </span>
                </>
              )}


              {/* Кто делает части — здесь же, у меры: подзадачи одной
                  карточки почти всегда лежат на разных людях, и до сих
                  пор доска отвечала «работа разбита», молча о том, кого
                  спрашивать. Аватар отвечает на это без раскрытия. */}
              {subtaskAssignees.length > 0 && (
                <span className="avatars" title={t.cardView.partsOn}>
                  {subtaskAssignees.slice(0, 3).map((id) => (
                    <Avatar key={id} name={people[id] ?? t.common.someone} size={AVATAR_SMALL} />
                  ))}
                </span>
              )}
            </div>
          )}

          {/* Где работа эпика (этап 33.5): полоса говорит «сколько»,
              значки — «где и где стоит». Значок ведёт на доску команды
              с отбором по этому эпику: следующий вопрос после «у ПОСТ
              стоит одна» — «какая». Стоящее — словом, а не цветом:
              тревога на карточке одна, и её уже подняла полоса. */}
          {card?.teams && card.teams.length > 0 && (
            <ul className="card-teams" aria-label={t.cardView.teams}>
              {card.teams.map((s) => {
                const href = `${boardPath(s.boardId)}?epic=${card.id}`
                return (
                  <li key={s.boardId}>
                    <a
                      className={s.blocked > 0 ? 'mark team-share team-share--stuck' : 'mark team-share'}
                      href={href}
                      title={t.cardView.teamShareTitle(s.boardName, s.done, s.total, s.blocked)}
                      onClick={(e) => {
                        e.stopPropagation()
                        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
                        e.preventDefault()
                        navigate(href)
                      }}
                    >
                      {t.cardView.teamShare(s.boardKey || s.boardName, s.done, s.total, s.blocked)}
                    </a>
                  </li>
                )
              })}
            </ul>
          )}

          {/* Раскрытые подзадачи. Список, а не карточки: карточка этой же
              доски уже лежит в своей колонке, и второй её показ рядом
              с родителем читался бы как вторая задача. Здесь видно, что
              за работа, готова ли она и чья она, если чужая. */}
          {(subtasks.length > 0 || adding) && (
            <ul className="subtasks" id={`subtasks-${cardId}`} hidden={!open}>
              {subtasks.map((s) => (
                <SubtaskRow
                  key={s.id}
                  subtask={s}
                  people={people}
                  assignees={s.assignees}
                  replies={s.replies}
                  onOpen={onOpen}
                  onMarkDone={onMarkDone}
                />
              ))}

              {/* Завести часть можно там же, где на части смотрят.
                  Последней строкой списка, а не отдельным углом
                  карточки: список и есть то, к чему её добавляют. */}
              <li className="subtask subtask--new">
                {adding ? (
                  <EditableText
                    value=""
                    autoFocus
                    label={t.cardView.subtaskName}
                    placeholder={t.cardView.whatToDo}
                    onSave={(title) => {
                      if (title.trim()) onSubtask(cardId, title.trim())
                      setAdding(false)
                    }}
                    onCancel={() => setAdding(false)}
                  />
                ) : (
                  <button className="link" onClick={() => setAdding(true)}>
                    {t.cardView.newSubtask}
                  </button>
                )}
              </li>
            </ul>
          )}

        </>
      )}
    </article>
  )
}

/**
 * Строка зависимости: кого карточка держит или кого ждёт.
 *
 * Первый назван и проходим, остальные сосчитаны: в ширину колонки
 * помещается одно имя, а вопрос «к кому идти» почти всегда про первого.
 * Полный список — в подсказке и в панели.
 */
function Dependency({
  label,
  cards,
  onOpen,
}: {
  label: string
  cards: Related[]
  onOpen: (cardId: string) => void
}) {
  if (cards.length === 0) return null
  const first = cards[0]

  return (
    <div className="card-waits">
      <span className="muted small">{label}: </span>
      {first.onThisBoard ? (
        <button className="link" onClick={() => onOpen(first.id)}>
          {first.title}
        </button>
      ) : first.boardId ? (
        // Связанная работа на чужой видимой доске открывается там, на
        // своей доске: связь через доски должна проходиться так же, как
        // своя (владелец 25.09.2026), а не только называться.
        <a
          className="link"
          href={boardPath(first.boardId, first.id)}
          title={first.where}
          onClick={(e) => {
            e.preventDefault()
            navigate(boardPath(first.boardId!, first.id))
          }}
        >
          {first.title}
        </a>
      ) : (
        // Доска не видна: назвать можем, открыть — нет.
        <span className="muted small" title={first.where}>
          {first.title}
        </span>
      )}
      {cards.length > 1 && (
        <span
          className="muted small"
          title={cards
            .slice(1)
            .map((c) => c.title)
            .join(', ')}
        >
          {t.cardView.andMore(cards.length - 1)}
        </span>
      )}
    </div>
  )
}

/**
 * Карточка перерисовывается только от своих изменений.
 *
 * Условие работоспособности не в самом memo, а в стабильности пропсов:
 * обработчики приходят готовыми и принимают cardId, а не замыкают его,
 * список меток карточки при их отсутствии — общая константа, а не новый
 * пустой массив на каждую отрисовку. Без этого memo не спасает ни от
 * чего и только добавляет сравнение.
 */
export const CardView = memo(CardViewInner)

/** Чипы меток карточки: до трёх и «+N». */
function LabelChips({ labels }: { labels: BoardLabel[] }) {
  const shown = labels.slice(0, 3)
  const more = labels.length - shown.length
  return (
    <>
      {shown.map((label) => (
        <span key={label.id} className={`${chipClass(label)} chip--card`} title={labelTitle(label)}>
          {label.name}
        </span>
      ))}
      {more > 0 && (
        <span className="chip chip--more chip--card" title={labels.slice(3).map((l) => l.name).join(', ')}>
          +{more}
        </span>
      )}
    </>
  )
}
