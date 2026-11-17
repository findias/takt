import { Menu } from '../../shared/ui/Menu.tsx'
import { ArchiveIcon, LinkIcon, MoreIcon, MoveIcon, TrashIcon } from '../../shared/ui/icons.tsx'
import { useCopy } from '../../shared/ui/CopyButton.tsx'
import { cardLink } from '../../shared/router/index.ts'
import { t } from '../../shared/i18n/index.ts'

export function CardPanelActions({
  boardId,
  cardId,
  title,
  canEdit,
  onArchive,
  onDelete,
  onMoveToBoard,
}: {
  boardId: string
  cardId: string
  title: string
  canEdit: boolean
  onArchive: (cardId: string) => void
  onDelete?: (cardId: string, title: string) => void
  onMoveToBoard?: (cardId: string, title: string) => void
}) {
  const copy = useCopy()
  return (
    <Menu
      label={t.cardView.actions(title)}
      items={[
        {
          label: t.panel.copyLink,
          icon: <LinkIcon />,
          onSelect: () => copy(cardLink(boardId, cardId), t.cardView.linkWhat),
        },
        ...(canEdit && onMoveToBoard ? [{
          label: t.panel.moveToBoard,
          icon: <MoveIcon />,
          onSelect: () => onMoveToBoard(cardId, title),
        }] : []),
        ...(canEdit ? [{
          label: t.cardView.archive,
          icon: <ArchiveIcon />,
          danger: true,
          onSelect: () => onArchive(cardId),
        }] : []),
        // Подтверждение удаления и отмена архивации остаются у родителя.
        ...(canEdit && onDelete ? [{
          label: t.cardView.deleteForever,
          icon: <TrashIcon />,
          danger: true,
          onSelect: () => onDelete(cardId, title),
        }] : []),
      ]}
    >
      <MoreIcon />
    </Menu>
  )
}
