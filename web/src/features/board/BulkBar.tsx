import { cardsLabel } from '../../entities/card/model.ts'
import type { Column } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import { ArchiveIcon, MoveIcon } from '../../shared/ui/icons.tsx'

/**
 * Полоса действий над выделенными карточками.
 *
 * Появляется снизу, когда выделено хоть что-то, и исчезает вместе
 * с выделением: место под неё, занятое постоянно, отнимало бы строку
 * у доски ради того, чем пользуются раз в день.
 *
 * Действий два — перенести и убрать в архив, — и это не начало списка,
 * а весь список. Массово делают ровно то, что делают одинаково:
 * разобрать очередь по колонкам и вымести сделанное. Исполнитель
 * и метка у каждой карточки свои, и «назначить всех на всё» — это
 * не ускорение работы, а способ испортить её одним нажатием.
 */
export function BulkBar({
  count,
  columns,
  onMove,
  onArchive,
  onClear,
}: {
  count: number
  columns: Column[]
  onMove: (columnId: string) => void
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
