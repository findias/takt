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
  UNIT_SHORT,
  estimateLabel,
  progressLabel,
  progressRatio,
} from '../../entities/card/model.ts'
import type { Related } from '../../entities/card/model.ts'
import type { Card, Column, EstimateUnit, Label } from '../../shared/api/index.ts'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { EditableText } from '../../shared/ui/EditableText.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import {
  ArchiveIcon,
  BlockedIcon,
  ChevronDownIcon,
  ChevronRightIcon,
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
  /** null снимает оценку. «Не оценена» и «оценена в ноль» — разные
   *  вещи: первое выкидывает карточку из веса, второе обещает, что
   *  работы в ней нет. */
  onEstimate: (cardId: string, estimate: number | null) => void
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
  onEstimate,
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
  const shownLabels = own.slice(0, 3)
  const hiddenLabels = own.length - shownLabels.length

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
      className={`card${dragging ? ' card--dragging' : ''}${edge ? ` card--edge-${edge}` : ''}${flash ? ' card--flash' : ''}`}
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
          {(card || parent) && (
            <div className="card-meta">
              {card && (
                // Не кнопка и не ссылка: номер выделяют и копируют,
                // а нажатие на карточку и так её открывает.
                <span className="card-number">{card.number}</span>
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
            </div>
          )}

          {/* Заголовок — кнопка: у нажимаемой карточки должна быть
              явная цель и для скринридера, и для клавиатуры. Двойного
              клика для переименования больше нет — он спорил
              с открытием; переименование осталось в меню и на «E». */}
          <button className="card-title" onClick={() => onOpen(cardId)}>
            {title}
          </button>
          {/* Метки правятся нажатием по самим меткам, а не пунктами
              в общем меню: там они шли лентой вперемешку с людьми
              и переносом, и найти среди них нужную метку было дольше,
              чем добавить её из панели.
              Видимых меток — до трёх, дальше счётчик: четыре цветных
              чипа занимают строку целиком и перестают читаться. */}
          {labels.length > 0 && (
            <Menu
              label={
                own.length > 0
                  ? `Метки: ${own.map((l) => l.name).join(', ')}`
                  : 'Метки: ни одной'
              }
              className={`card-field card-labels${own.length === 0 ? ' card-slot' : ''}`}
              align="left"
              items={labels.map((label) => ({
                label: label.name,
                checked: cardLabels.includes(label.id),
                onSelect: () => onLabel(cardId, label.id, !cardLabels.includes(label.id)),
              }))}
            >
              {shownLabels.map((label) => (
                <span key={label.id} className={`chip chip--${label.tone}`}>
                  {label.name}
                </span>
              ))}
              {hiddenLabels > 0 && <span className="chip chip--more">+{hiddenLabels}</span>}
              {own.length === 0 && <span className="chip chip--more">метки</span>}
            </Menu>
          )}
          {card && (card.blocked || aging) && (
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
            </div>
          )}

          {/* Раскрытые подзадачи. Список, а не карточки: карточка этой же
              доски уже лежит в своей колонке, и второй её показ рядом
              с родителем читался бы как вторая задача. Здесь видно, что
              за работа, готова ли она и чья она, если чужая. */}
          {subtasks.length > 0 && (
            <ul className="subtasks" id={`subtasks-${cardId}`} hidden={!open}>
              {subtasks.map((s) => (
                <li key={s.id} className={s.done ? 'subtask subtask--done' : 'subtask'}>
                  {s.onThisBoard ? (
                    <button className="link" onClick={() => onOpen(s.id)}>
                      {s.title}
                    </button>
                  ) : (
                    // Чужая доска: назвать можем, открыть отсюда — нет.
                    <span>{s.title}</span>
                  )}
                  {!s.onThisBoard && <span className="muted small">{s.where}</span>}
                </li>
              ))}
            </ul>
          )}

          <div className="card-foot">
            {/* Кто делает — самое частое, о чём спрашивают доску после
                «что происходит», и правится это нажатием по самой
                стопке. Пункт на человека, и он же снимает: два списка
                «назначить» и «снять» вдвое длиннее и заставляют помнить,
                кто где. */}
            <Menu
              label={
                assignees.length > 0
                  ? `Исполнители: ${assignees.map((id) => people[id] ?? 'Кто-то').join(', ')}`
                  : 'Исполнителей нет'
              }
              className={`card-field${assignees.length === 0 ? ' card-slot' : ''}`}
              align="left"
              items={Object.entries(people).map(([id, name]) => ({
                label: name,
                checked: assignees.includes(id),
                onSelect: () => onAssign(cardId, id, !assignees.includes(id)),
              }))}
            >
              <span className="avatars">
                {shownAssignees.map((id) => (
                  <Avatar key={id} name={people[id] ?? 'Кто-то'} />
                ))}
                {hiddenAssignees > 0 && (
                  <span className="avatar avatar--more">+{hiddenAssignees}</span>
                )}
                {/* Пустая стопка — тоже поле, и по нему тоже нажимают:
                    иначе назначить первого исполнителя было бы не по
                    чему. */}
                {assignees.length === 0 && (
                  // Без класса `avatar`: это не человек, а место под
                  // него, и считать его аватаром не должны ни фильтр,
                  // ни проверка «есть ли исполнитель».
                  <span className="avatar--more" aria-hidden="true">
                    +
                  </span>
                )}
              </span>
            </Menu>

            {/* Оценка правится с доски, а не только из панели: её
                ставят пачкой на планировании, а открывать ради одного
                числа пятнадцать карточек подряд — пятнадцать лишних
                переходов. */}
            {card && (
              <Estimate
                value={card.estimate}
                unit={unit}
                onSave={(value) => onEstimate(cardId, value)}
              />
            )}

            {/* Одно меню вместо ряда кнопок: три подписи в ширину колонки
                не помещались и обрезались до «Откры», «Переиме», «Удалит».
                Осталось в нём то, у чего на карточке нет своего места:
                люди и метки ушли к самим людям и меткам. Перенос стоит
                здесь — это не удобство, а требование WCAG 2.5.7:
                клавиатурного эквивалента недостаточно, нужен путь,
                выполнимый одним нажатием. */}
            <Menu
              label={`Действия карточки «${title}»`}
              className="btn btn--icon btn--quiet card-slot"
              items={[
                { label: 'Переименовать', icon: <EditIcon />, onSelect: () => setEditing(true) },
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
        </>
      )}
    </article>
  )
}

/**
 * Оценка на карточке.
 *
 * Числом, а не шаговым вводом: шаг не знает, чему равен, — час, день
 * и очко растут по-разному, а дробная оценка («полдня») шагами
 * не набирается вовсе. Пустое поле снимает оценку.
 */
function Estimate({
  value,
  unit,
  onSave,
}: {
  value: number | null
  unit: EstimateUnit
  onSave: (value: number | null) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value === null ? '' : String(value))
  useEffect(() => setDraft(value === null ? '' : String(value)), [value])

  if (!editing) {
    const label = estimateLabel(value, unit)
    return (
      <button
        // Неоценённая карточка показывает место под оценку так же,
        // как пустая стопка исполнителей: тихо и только тогда, когда
        // на карточку смотрят. Иначе доска из пятисот неоценённых
        // карточек превращается в пятьсот приглашений что-то заполнить.
        className={`card-field chip chip--estimate${value === null ? ' card-slot' : ''}`}
        aria-label={label ? `Оценка: ${label}` : 'Оценка не поставлена'}
        onClick={() => setEditing(true)}
      >
        {label ?? 'оценка'}
      </button>
    )
  }

  const commit = () => {
    setEditing(false)
    const trimmed = draft.trim()
    if (trimmed === '') {
      if (value !== null) onSave(null)
      return
    }
    const parsed = Number(trimmed.replace(',', '.'))
    // Не число или не больше нуля — не оценка. Записать такое молча
    // значит соврать в сумме по человеку и в весе родителя, поэтому
    // поле возвращается к прежнему значению.
    if (!Number.isFinite(parsed) || parsed <= 0) {
      setDraft(value === null ? '' : String(value))
      return
    }
    if (parsed !== value) onSave(parsed)
  }

  return (
    <input
      autoFocus
      type="text"
      inputMode="decimal"
      className="card-estimate-input"
      aria-label={`Оценка карточки, ${UNIT_SHORT[unit]}`}
      // В пустом поле единица заменяет подпись: чип с ней исчезает
      // ровно тогда, когда в него начинают вводить.
      placeholder={UNIT_SHORT[unit]}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur()
        if (e.key === 'Escape') {
          setDraft(value === null ? '' : String(value))
          setEditing(false)
        }
      }}
    />
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
