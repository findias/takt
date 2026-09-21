import { useEffect, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { Member, Team } from '../../shared/api/index.ts'
import { BoardAccess } from './BoardAccess.tsx'
import { Panel, usePanelMode } from '../../shared/ui/Panel.tsx'
import { t } from '../../shared/i18n/index.ts'

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
      title={t.boards.whoSees}
      label={t.boards.boardAccess}
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

