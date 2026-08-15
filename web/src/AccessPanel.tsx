import { useEffect, useState } from 'react'
import { VISIBILITY_NAMES, api } from './api'
import type { BoardAccess as Access, Member, Team } from './api'
import { BoardAccess } from './BoardAccess'
import { Panel, usePanelMode } from './Panel'

/**
 * Кому видна доска — на самой доске, а не только в её списке.
 *
 * Раньше за этим приходилось уходить обратно в список досок: открыть
 * доску, понять, что её видят не те, вернуться, найти строку, раскрыть
 * настройку. Решение о видимости принимают, глядя на доску, — там оно
 * и должно приниматься.
 */
export function AccessPanel({
  boardId,
  canEdit,
  onClose,
  onChanged,
}: {
  boardId: string
  canEdit: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const [mode, setMode] = usePanelMode()
  // Люди и подразделения нужны только этой панели, поэтому и берутся
  // только при её открытии: на каждую доску это два лишних запроса,
  // которые почти никогда не понадобятся.
  const [people, setPeople] = useState<Member[]>([])
  const [teams, setTeams] = useState<Team[]>([])

  useEffect(() => {
    Promise.all([api.team(), api.listTeams()])
      .then(([org, t]) => {
        setPeople(org.members)
        setTeams(t.teams)
      })
      .catch(() => {
        setPeople([])
        setTeams([])
      })
  }, [])

  return (
    <Panel
      mode={mode}
      onMode={setMode}
      title="Кому видна доска"
      label="Доступ к доске"
      onClose={onClose}
    >
      <BoardAccess
        boardId={boardId}
        people={people}
        teams={teams}
        canEdit={canEdit}
        onChanged={onChanged}
      />
    </Panel>
  )
}

/** Короткая подпись для шапки: кто видит доску прямо сейчас. */
export function visibilityLabel(access: Access | null): string {
  if (!access) return 'Доступ'
  if (access.visibility === 'team') {
    return access.teamName ? `Видна: ${access.teamName}` : 'Видна: команде'
  }
  if (access.visibility === 'private') {
    return `Видна: ${access.members.length} поимённо`
  }
  return `Видна: ${VISIBILITY_NAMES.org.toLowerCase()}`
}
