import { PRIORITIES, PRIORITY_NAMES, cardsLabel } from '../../entities/card/model.ts'
import type { Column, Label, Priority } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import { ArchiveIcon, ClockIcon, MoveIcon, PeopleIcon, TagIcon } from '../../shared/ui/icons.tsx'

/**
 * Полоса действий над выделенными карточками.
 *
 * Появляется снизу, когда выделено хоть что-то, и исчезает вместе
 * с выделением: место под неё, занятое постоянно, отнимало бы строку
 * у доски ради того, чем пользуются раз в день.
 *
 * Действий пять, и все пять про разбор бэклога: это работа, ради
 * которой полоса и заведена. Десяток карточек, которым надо поставить
 * один уровень, одну метку, одного исполнителя и перенести в одну
 * колонку, по одной — это десяток открытий панели.
 *
 * Сначала здесь было два действия, и довод против метки с исполнителем
 * звучал так: «назначить всех на всё» — способ испортить работу одним
 * нажатием. Довод неверен ровно в той части, где он молчит про отмену:
 * у каждого действия здесь одна отмена на всю пачку, и испорченное
 * возвращается тем же одним нажатием.
 *
 * Полоса не делает ничего сама: она зовёт по одному обработчику
 * на действие, а очередь операций и уведомление с отменой живут
 * в слое данных.
 */
export function BulkBar({
  count,
  columns,
  labels,
  people,
  onMove,
  onPrioritise,
  onLabel,
  onAssign,
  onArchive,
  onClear,
}: {
  count: number
  columns: Column[]
  labels: Label[]
  /** userId → имя. */
  people: Record<string, string>
  onMove: (columnId: string) => void
  onPrioritise: (priority: Priority) => void
  onLabel: (labelId: string) => void
  onAssign: (userId: string) => void
  onArchive: () => void
  onClear: () => void
}) {
  return (
    // Живая область: полоса появляется внизу экрана, далеко от флажка,
    // по которому её вызвали, и тот, кто не видит экрана, иначе
    // не узнает, что выделение вообще к чему-то привело.
    <div className="bulk-bar" role="status" aria-label="Действия над выделенными">
      <span className="bulk-count">Выделено: {cardsLabel(count)}</span>

      <Menu
        label="Перенести выделенные"
        className="btn btn--primary"
        align="left"
        drop="up"
        // Пункт — одно название колонки: «Перенести» уже написано
        // на кнопке, и повторять его в каждом пункте значит читать
        // одно и то же слово четыре раза подряд.
        items={columns.map((column) => ({
          label: column.name,
          icon: <MoveIcon />,
          onSelect: () => onMove(column.id),
        }))}
      >
        <MoveIcon />
        Перенести
      </Menu>

      <Menu
        label="Приоритет выделенным"
        className="btn"
        align="left"
        drop="up"
        items={PRIORITIES.map((level) => ({
          label: PRIORITY_NAMES[level],
          icon: <ClockIcon />,
          onSelect: () => onPrioritise(level),
        }))}
      >
        <ClockIcon />
        Приоритет
      </Menu>

      {/* Метка ставится, а не переключается: «пометить десять карточек»
          — одно решение, а переключение дало бы половину помеченных
          и половину снятых, то есть результат, зависящий от того,
          что было раньше. */}
      {labels.length > 0 && (
        <Menu
          label="Пометить выделенные"
          className="btn"
          align="left"
          drop="up"
          items={labels.map((label) => ({
            label: label.name,
            icon: <TagIcon />,
            onSelect: () => onLabel(label.id),
          }))}
        >
          <TagIcon />
          Метка
        </Menu>
      )}

      <Menu
        label="Назначить на выделенные"
        className="btn"
        align="left"
        drop="up"
        items={Object.entries(people).map(([id, name]) => ({
          label: name,
          icon: <PeopleIcon />,
          onSelect: () => onAssign(id),
        }))}
      >
        <PeopleIcon />
        Назначить
      </Menu>

      {/* Убрать — обратимо и потому без вопроса: отмена предлагается
          уведомлением, одна на всю пачку. */}
      <Button kind="danger" icon={<ArchiveIcon />} onClick={onArchive}>
        В архив
      </Button>

      <Button kind="quiet" onClick={onClear}>
        Снять выделение
      </Button>
    </div>
  )
}
