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
import { progressLabel, progressRatio } from '../../entities/card/model.ts'
import type { Card, Column, EstimateUnit, Label } from '../../shared/api/index.ts'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { EditableText } from '../../shared/ui/EditableText.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import {
  ArchiveIcon,
  EditIcon,
  MoreIcon,
  MoveIcon,
  OpenIcon,
  PeopleIcon,
  TagIcon,
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
  onLabel: (cardId: string, labelId: string, on: boolean) => void
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
  onLabel,
  columns,
  onMoveToColumn,
  onOpen,
  onMoveByKeyboard,
  onNavigate,
  onRename,
  onArchive,
}: CardProps) {
  const title = card?.title ?? '…'
  const ref = useRef<HTMLElement>(null)
  const [dragging, setDragging] = useState(false)
  const [edge, setEdge] = useState<Edge | null>(null)
  const [editing, setEditing] = useState(false)

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

          <div className="card-head">
            {/* Заголовок — кнопка: у нажимаемой карточки должна быть
                явная цель и для скринридера, и для клавиатуры. Двойного
                клика для переименования больше нет — он спорил
                с открытием; переименование осталось в меню и на «E». */}
            <button className="card-title" onClick={() => onOpen(cardId)}>
              {title}
            </button>
            {/* Кто делает — самое частое, о чём спрашивают доску после
                «что происходит». Инициалы читаются с одного взгляда
                и занимают двадцать пикселей. */}
            {shownAssignees.length > 0 && (
              <div className="avatars">
                {shownAssignees.map((id) => (
                  <Avatar key={id} name={people[id] ?? 'Кто-то'} />
                ))}
                {hiddenAssignees > 0 && (
                  <span className="avatar avatar--more" title={`и ещё ${hiddenAssignees}`}>
                    +{hiddenAssignees}
                  </span>
                )}
              </div>
            )}
          </div>
          {/* Метки — до трёх видимых. Дальше счётчик: четыре цветных
              чипа занимают строку целиком и перестают читаться. */}
          {shownLabels.length > 0 && (
            <div className="card-labels">
              {shownLabels.map((label) => (
                <span key={label.id} className={`chip chip--${label.tone}`}>
                  {label.name}
                </span>
              ))}
              {hiddenLabels > 0 && <span className="chip chip--more">+{hiddenLabels}</span>}
            </div>
          )}
          {card && (card.blocked || card.progress || aging) && (
            <div className="card-marks">
              {aging && (
                <span className="mark mark--aging" title="Возраст считается от начала работы">
                  {aging}
                </span>
              )}
              {card.blocked && (
                <span className="mark mark--blocked" title={card.blocked.reason}>
                  {/* Глиф прячем: скринридер прочитает ⛔ как «знак въезд
                      запрещён» — слово рядом надёжнее. */}
                  <span aria-hidden="true">⛔ </span>
                  Заблокирована: {card.blocked.reason}
                </span>
              )}
            </div>
          )}
          {/* Подзадачи — полосой, а не строчкой среди пометок: «0 из 1»
              в общем ряду читалось как ещё одна пометка, и по доске
              нельзя было понять, у каких задач работа разбита и как
              далеко она ушла. */}
          {card?.progress && card.progress.total > 0 && (
            <div className="card-progress">
              <div
                className="progress"
                role="progressbar"
                aria-valuenow={card.progress.done}
                aria-valuemin={0}
                aria-valuemax={card.progress.total}
                aria-label={`Подзадачи: готово ${progressLabel(card, unit)}`}
              >
                <div className="progress-fill" style={{ width: `${progressRatio(card) * 100}%` }} />
              </div>
              <span className="muted small">{progressLabel(card, unit)}</span>
            </div>
          )}

          <div className="card-actions">
            {/* Одно меню вместо ряда кнопок: три подписи в ширину колонки
                не помещались и обрезались до «Откры», «Переиме», «Удалит».
                Перенос стоит в нём же — это не удобство, а требование
                WCAG 2.5.7: клавиатурного эквивалента недостаточно, нужен
                путь, выполнимый одним нажатием. */}
            <Menu
              label={`Действия карточки «${title}»`}
              items={[
                { label: 'Открыть', icon: <OpenIcon />, onSelect: () => onOpen(cardId) },
                { label: 'Переименовать', icon: <EditIcon />, onSelect: () => setEditing(true) },
                // Один пункт на человека, и он же снимает: два списка
                // «назначить» и «снять» вдвое длиннее и заставляют
                // помнить, кто где.
                ...Object.entries(people).map(([id, name]) => ({
                  label: assignees.includes(id) ? `Снять: ${name}` : `Назначить: ${name}`,
                  icon: <PeopleIcon />,
                  onSelect: () => onAssign(cardId, id, !assignees.includes(id)),
                })),
                ...labels.map((label) => ({
                  label: cardLabels.includes(label.id)
                    ? `Снять метку «${label.name}»`
                    : `Метка «${label.name}»`,
                  icon: <TagIcon />,
                  onSelect: () => onLabel(cardId, label.id, !cardLabels.includes(label.id)),
                })),
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
 * Карточка перерисовывается только от своих изменений.
 *
 * Условие работоспособности не в самом memo, а в стабильности пропсов:
 * обработчики приходят готовыми и принимают cardId, а не замыкают его,
 * список меток карточки при их отсутствии — общая константа, а не новый
 * пустой массив на каждую отрисовку. Без этого memo не спасает ни от
 * чего и только добавляет сравнение.
 */
export const CardView = memo(CardViewInner)
