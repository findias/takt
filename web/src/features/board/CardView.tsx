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
import { agingLabel } from '../../entities/board/model.ts'
import {
  PRIORITIES,
  PRIORITY_NAMES,
  priorityLabel,
  progressLabel,
  progressRatio,
  unitLabel,
} from '../../entities/card/model.ts'
import type { Related } from '../../entities/card/model.ts'
import type { Card, Column, EstimateUnit, Label, Priority } from '../../shared/api/index.ts'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { EditableText } from '../../shared/ui/EditableText.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import { SubtaskRow } from './SubtaskRow.tsx'
import {
  ArchiveIcon,
  BlockedIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  EditIcon,
  MoreIcon,
  MoveIcon,
  TrashIcon,
} from '../../shared/ui/icons.tsx'

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
  labels: Label[]
  cardLabels: string[]
  /** Родительская задача, если карточка — чья-то подзадача. */
  parent?: { id: string; title: string; onThisBoard: boolean }
  /** Подзадачи этой карточки. Раскрываются по кнопке прямо на доске:
   *  до этого разбиение работы было видно только числом «0 из 3»,
   *  а чтобы узнать, на что именно она разбита, карточку приходилось
   *  открывать. */
  subtasks: Related[]
  onLabel: (cardId: string, labelId: string, on: boolean) => void
  /** Выделена ли карточка для действия над многими сразу. */
  selected: boolean
  /** `extend` — shift-щелчок: взять всё между прошлым флажком и этим. */
  onSelect: (cardId: string, on: boolean, extend?: boolean) => void
  /** Уровень приоритета. Порядок карточек в колонке он не трогает:
   *  уровень говорит, что важнее, порядок — что взято следующим. */
  onPrioritise: (cardId: string, priority: Priority) => void
  onBlock: (cardId: string, reason: string) => void
  onUnblock: (cardId: string) => void
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
  cardLabels,
  parent,
  subtasks,
  onLabel,
  selected,
  onSelect,
  onPrioritise,
  onBlock,
  onUnblock,
  columns,
  onMoveToColumn,
  onOpen,
  onMoveByKeyboard,
  onNavigate,
  onRename,
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
  // Раскрытие подзадач — состояние самой карточки и живёт с ней: это
  // ответ на «что здесь внутри», заданный один раз и здесь же, а не
  // настройка, которую человек ждёт увидеть завтра такой же.
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const element = ref.current
    if (!element) return
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
  // Двое — обычный случай, четверо — уже толпа: показываем троих
  // и число остальных, как и с метками. Порядок — назначения: первым
  // стоит тот, кто взялся первым.
  const shownAssignees = assignees.slice(0, 3)
  const hiddenAssignees = assignees.length - shownAssignees.length
  const own = labels.filter((l) => cardLabels.includes(l.id))
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

  return (
    <article
      ref={ref}
      className={`card${dragging ? ' card--dragging' : ''}${edge ? ` card--edge-${edge}` : ''}${flash ? ' card--flash' : ''}${selected ? ' card--selected' : ''}`}
      tabIndex={0}
      data-card={cardId}
      role="group"
      // Подсказка читается скринридером при переходе на карточку —
      // это единственное место, где о сокращениях можно сказать тому,
      // кто не видит экрана.
      aria-label={`Карточка «${title}». Стрелки — переход, Ctrl со стрелками — перенос, Enter — открыть, E — переименовать.`}
      onKeyDown={onKeyDown}
      onClick={onClick}
    >
      {editing ? (
        <EditableText
          value={title}
          autoFocus
          label="Название карточки"
          onSave={(next) => {
            onRename(cardId, next)
            setEditing(false)
          }}
          onCancel={() => setEditing(false)}
          className="card-title"
        />
      ) : (
        <>
          {/* Номер и чья это часть — над названием, а не под ним:
              сначала «что это и где я», потом «что делать». Про родителя
              раньше можно было узнать, только открыв карточку, и
              подзадача на доске выглядела самостоятельной работой. */}
          <div className="card-meta">
            {/* Флажок выделения. Виден по наведению и пока выделение
                идёт — на доске в пятьсот карточек пятьсот флажков
                читаются как разлинованный список, а не как работа.
                Родной флажок, а не своя картинка: он умеет пробел,
                читается диктором и уже растянут до цели нажатия
                общим правилом. */}
            <input
              type="checkbox"
              className="card-check"
              checked={selected}
              aria-label={`Выделить «${title}»`}
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
            {card && (
              // Не кнопка и не ссылка: номер выделяют и копируют,
              // а нажатие на карточку и так её открывает.
              <span className="card-number">{card.number}</span>
            )}
            {/* Приоритет правится нажатием по нему самому — как
                исполнители и метки: спрашивают о нём не реже, а путь
                к нему был длиннее всех. Слово, а не значок:
                «наивысший» отвечает на вопрос, а красная точка требует,
                чтобы её сначала объяснили.
                Средний уровень словом не пишется — умолчание у каждой
                второй карточки это шум, — но место под него остаётся
                и показывается, когда на карточку смотрят. */}
            {card && (
              <Menu
                label={`Приоритет: ${priorityLabel(card.priority).toLowerCase()}`}
                className={`field priority-field${
                  card.priority === 'medium' ? ' field--empty' : ''
                }`}
                align="left"
                items={PRIORITIES.map((level) => ({
                  label: PRIORITY_NAMES[level],
                  checked: card.priority === level,
                  onSelect: () => onPrioritise(cardId, level),
                }))}
              >
                {card.priority === 'medium' ? (
                  '+ приоритет'
                ) : (
                  <span className={`priority-mark priority-mark--${card.priority}`}>
                    {priorityLabel(card.priority).toLowerCase()}
                  </span>
                )}
              </Menu>
            )}
            {parent && (
              <span className="card-parent">
                <span aria-hidden="true">↳ </span>
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

            {/* Метки на доске — точками, а чипами они стоят в панели.
                Это не «компактный вид», а разные вопросы: чип отвечает
                «что это за метка», точка — «одна ли это группа», и на
                доске в триста карточек глаз читает только цвет. Три чипа
                занимали строку целиком в каждой карточке.
                Правятся нажатием по самим точкам: путь к метке должен
                быть короче, чем поход в меню мимо людей и колонок. */}
            {labels.length > 0 && (
              <Menu
                label={
                  own.length > 0
                    ? `Метки: ${own.map((l) => l.name).join(', ')}`
                    : 'Метки: ни одной'
                }
                className={`field label-field${own.length === 0 ? ' field--empty' : ''}`}
                align="right"
                items={labels.map((label) => ({
                  label: label.name,
                  checked: cardLabels.includes(label.id),
                  onSelect: () => onLabel(cardId, label.id, !cardLabels.includes(label.id)),
                }))}
              >
                {own.length === 0 ? (
                  '+ метка'
                ) : (
                  <span className="label-dots">
                    {/* Больше четырёх точек не показываем: пятая уже
                        не различается глазом, а полный список есть
                        в подсказке кнопки и в панели. */}
                    {own.slice(0, 4).map((label) => (
                      <span
                        key={label.id}
                        className={`label-dot label-dot--${label.tone}`}
                        title={label.name}
                      />
                    ))}
                  </span>
                )}
              </Menu>
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
            <Menu
              label={`Действия карточки «${title}»`}
              className="btn btn--icon btn--quiet card-slot"
              items={[
                { label: 'Переименовать', icon: <EditIcon />, onSelect: () => setEditing(true) },
                // Верх шкалы переключается прямо с доски: «это горит»
                // говорят чаще, чем меняют что-либо ещё, а вся шкала
                // живёт в панели.
                card?.priority === 'highest'
                  ? {
                      label: 'Вернуть средний приоритет',
                      icon: <ClockIcon />,
                      onSelect: () => onPrioritise(cardId, 'medium'),
                    }
                  : {
                      label: 'Наивысший приоритет',
                      icon: <ClockIcon />,
                      onSelect: () => onPrioritise(cardId, 'highest'),
                    },
                card?.blocked
                  ? {
                      label: 'Снять блокировку',
                      icon: <BlockedIcon />,
                      onSelect: () => onUnblock(cardId),
                    }
                  : {
                      // Причину пишут словами: список готовых
                      // формулировок отвечает не на тот вопрос — важно,
                      // чего ждём именно здесь.
                      label: 'Заблокировать…',
                      icon: <BlockedIcon />,
                      onSelect: () => setBlocking(true),
                    },
                ...columns
                  .filter((c) => c.id !== columnId)
                  .map((c) => ({
                    label: `Перенести в «${c.name}»`,
                    icon: <MoveIcon />,
                    onSelect: () => onMoveToColumn(cardId, c.id),
                  })),
                {
                  label: 'Убрать в архив',
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
                        label: 'Удалить навсегда',
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
          </div>

          {/* Заголовок и кто делает — одна строка: «что за работа»
              и «кого спрашивать» читают вместе, и второй ряд ради
              стопки аватаров стоил бы четырёх пикселей на каждой
              карточке доски. */}
          <div className="card-head">
            {/* Заголовок — кнопка: у нажимаемой карточки должна быть
                явная цель и для скринридера, и для клавиатуры. Двойного
                клика для переименования больше нет — он спорил
                с открытием; переименование осталось в меню и на «E». */}
            <button className="card-title" onClick={() => onOpen(cardId)}>
              {title}
            </button>

            {/* Кто делает — правится нажатием по самой стопке. Пункт
                на человека, и он же снимает: два списка «назначить»
                и «снять» вдвое длиннее и заставляют помнить, кто где. */}
            <Menu
              label={
                assignees.length > 0
                  ? `Исполнители: ${assignees.map((id) => people[id] ?? 'Кто-то').join(', ')}`
                  : 'Исполнителей нет'
              }
              className={`field${assignees.length === 0 ? ' field--empty' : ''}`}
              align="right"
              items={Object.entries(people).map(([id, name]) => ({
                label: name,
                checked: assignees.includes(id),
                onSelect: () => onAssign(cardId, id, !assignees.includes(id)),
              }))}
            >
              {assignees.length === 0 ? (
                '+ кто'
              ) : (
                <span className="avatars">
                  {shownAssignees.map((id) => (
                    <Avatar key={id} name={people[id] ?? 'Кто-то'} />
                  ))}
                  {hiddenAssignees > 0 && (
                    <span className="avatar--more" title={`и ещё ${hiddenAssignees}`}>
                      +{hiddenAssignees}
                    </span>
                  )}
                </span>
              )}
            </Menu>
          </div>
          {card && (card.blocked || aging || card.estimate !== null) && (
            <div className="card-marks">
              {aging && (
                <span className="mark mark--aging" title="Возраст считается от начала работы">
                  {aging}
                </span>
              )}
              {card.blocked && (
                // Причина не правится нажатием по ней, в отличие
                // от остальных полей: блокировка — интервал, а не
                // пометка, и «переписать причину» означало бы вторую
                // блокировку поверх открытой, от которой время в блоке
                // посчиталось бы дважды. Ошибочную причину снимают
                // и ставят заново.
                <span className="mark mark--blocked" title={card.blocked.reason}>
                  {/* Глиф прячем: скринридер прочитает ⛔ как «знак въезд
                      запрещён» — слово рядом надёжнее. */}
                  <span aria-hidden="true">⛔ </span>
                  Заблокирована: {card.blocked.reason}
                </span>
              )}
              {/* Оценка — цифра, и тихая: она нужна в разговоре
                  о загрузке, а не при поиске работы глазами. Единица
                  одна на всю доску, и повторять её триста раз незачем
                  — она в подсказке. Правится оценка в панели: шагами,
                  потому что меняют её почти всегда на единицу. */}
              {card.estimate !== null && (
                <span
                  className="card-estimate"
                  title={`Оценка: ${card.estimate} ${unitLabel(card.estimate, unit)}`}
                >
                  {card.estimate}
                </span>
              )}
            </div>
          )}
          {blocking && (
            <EditableText
              value=""
              autoFocus
              label="Причина блокировки"
              placeholder="Чего ждём"
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
                  aria-label={`Подзадачи: готово ${progressLabel(card, unit)}. ${
                    open ? 'Скрыть подзадачи' : 'Показать подзадачи'
                  }`}
                  onClick={() => setOpen(!open)}
                >
                  {open ? <ChevronDownIcon /> : <ChevronRightIcon />}
                  <span className="progress" aria-hidden="true">
                    <span
                      className="progress-fill"
                      style={{ width: `${progressRatio(card) * 100}%` }}
                    />
                  </span>
                  <span className="muted small" aria-hidden="true">
                    {progressLabel(card, unit)}
                  </span>
                </button>
              ) : (
                <>
                  <div
                    className="progress"
                    role="progressbar"
                    aria-valuenow={card.progress.done}
                    aria-valuemin={0}
                    aria-valuemax={card.progress.total}
                    aria-label={`Подзадачи: готово ${progressLabel(card, unit)}`}
                  >
                    <div
                      className="progress-fill"
                      style={{ width: `${progressRatio(card) * 100}%` }}
                    />
                  </div>
                  <span className="muted small">{progressLabel(card, unit)}</span>
                </>
              )}

              {/* Кто делает части — здесь же, у меры: подзадачи одной
                  карточки почти всегда лежат на разных людях, и до сих
                  пор доска отвечала «работа разбита», молча о том, кого
                  спрашивать. Аватар отвечает на это без раскрытия. */}
              {subtaskAssignees.length > 0 && (
                <span className="avatars avatars--small" title="На кого разложены части">
                  {subtaskAssignees.slice(0, 3).map((id) => (
                    <Avatar key={id} name={people[id] ?? 'Кто-то'} />
                  ))}
                </span>
              )}
            </div>
          )}

          {/* Раскрытые подзадачи. Список, а не карточки: карточка этой же
              доски уже лежит в своей колонке, и второй её показ рядом
              с родителем читался бы как вторая задача. Здесь видно, что
              за работа, готова ли она и чья она, если чужая. */}
          {subtasks.length > 0 && (
            <ul className="subtasks" id={`subtasks-${cardId}`} hidden={!open}>
              {subtasks.map((s) => (
                <SubtaskRow
                  key={s.id}
                  subtask={s}
                  people={people}
                  assignees={s.assignees}
                  replies={s.replies}
                  onOpen={onOpen}
                />
              ))}
            </ul>
          )}

        </>
      )}
    </article>
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
