import { ROLE_NAMES } from '../../shared/api/index.ts'
import type { Member, Role } from '../../shared/api/index.ts'
import { Menu } from '../../shared/ui/Menu.tsx'
import { MoreIcon } from '../../shared/ui/icons.tsx'
import { t } from '../../shared/i18n/index.ts'

/** Права остаются прежними: меню собирает доступные действия,
 *  но не делает владельца подразделения владельцем организации. */
export function MemberControls({ member, canManage, onRole, onRename, onEmail, onIssueLink, onRemove, onErase }: {
  member: Member
  canManage: boolean
  onRole: (id: string, role: Role) => void
  onRename: (member: Member) => void
  onEmail: (member: Member) => void
  onIssueLink: (member: Member) => void
  onRemove: (member: Member) => void
  onErase: (member: Member) => void
}) {
  if (member.kind === 'service') return <span className="role-chip">{t.team.key}</span>
  return (
    <div className="row member-controls">
      {canManage ? (
        <select value={member.role} aria-label={t.team.roleOf(member.name)}
          onChange={(e) => onRole(member.userId, e.target.value as Role)}>
          {(Object.keys(ROLE_NAMES) as Role[]).map((role) => <option key={role} value={role}>{ROLE_NAMES[role]}</option>)}
        </select>
      ) : <span className="role-chip">{ROLE_NAMES[member.role]}</span>}
      {(canManage || member.erasable) && <Menu label={t.team.actionsOf(member.name)} items={[
        ...(canManage && member.nameEditable ? [{ label: t.team.rename, onSelect: () => onRename(member) }] : []),
        ...(canManage && member.emailEditable ? [
          { label: t.team.email, onSelect: () => onEmail(member) },
          // Выпуск ссылки имеет те же права, что смена почты; способ
          // входа, управляемый провайдером, здесь не переопределяется.
          { label: t.passwordLink.issue, onSelect: () => onIssueLink(member) },
        ] : []),
        ...(canManage ? [{ label: t.team.remove, onSelect: () => onRemove(member) }] : []),
        { label: t.team.erase, danger: true, onSelect: () => onErase(member) },
      ]}><MoreIcon /></Menu>}
    </div>
  )
}
