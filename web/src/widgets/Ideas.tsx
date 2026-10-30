import { useEffect, useState } from 'react'
import { api } from '../shared/api/index.ts'
import type { BoardInfo, Idea, Principal, Team } from '../shared/api/index.ts'
import { setQuery, useQuery } from '../shared/router/index.ts'
import { ScreenError } from '../shared/ui/Field.tsx'
import { EmptyState, Skeleton } from '../shared/ui/states.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { Hint } from '../shared/ui/Hint.tsx'
import { t } from '../shared/i18n/index.ts'
import { parseIdeaSort, sortIdeas, visibleIdeas } from '../entities/idea/model.ts'
import { IdeaForm } from '../features/ideas/IdeaForm.tsx'
import { IdeaRow } from '../features/ideas/IdeaRow.tsx'
import { loadIdeas, voteIdea } from '../features/ideas/api.ts'
import type { IdeaList } from '../features/ideas/api.ts'

/**
 * Идеи от людей команды (ROADMAP 43, срез 1): список организации,
 * отдельно от досок. Подать и поддержать может каждый, в том числе
 * наблюдатель, — поэтому экран не смотрит на роль вовсе.
 *
 * Порядок — в адресе: «смотри, что поддержали» пересылают ссылкой.
 * Список — таблица людей и слов, а не доска: у идеи нет колонки.
 */
export function Ideas({ principal }: { principal: Principal }) {
  const query = useQuery()
  const sort = parseIdeaSort(query)
  const withDecided = query.get('decided') === '1'
  const s = t.ideas

  const [list, setList] = useState<IdeaList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [voteError, setVoteError] = useState<string | null>(null)
  const [teams, setTeams] = useState<Team[]>([])
  const [boards, setBoards] = useState<BoardInfo[]>([])
  const [open, setOpen] = useState(false)
  const [voting, setVoting] = useState<string | null>(null)

  useEffect(() => {
    let current = true
    loadIdeas()
      .then((r) => current && setList(r))
      .catch((e) => current && setError(e instanceof Error ? e.message : s.loadFailed))
    return () => {
      current = false
    }
  }, [s, principal.orgId])

  // Подразделения нужны только форме — и грузятся, когда её открыли.
  useEffect(() => {
    if (!open) return
    let current = true
    api
      .listTeams()
      .then((r) => current && setTeams(r.teams))
      .catch(() => {
        // Без списка форма всё равно работает: идея уйдёт всей организации.
      })
    return () => {
      current = false
    }
  }, [open])

  // Доски нужны только тому, кто решает, — и грузятся, когда есть
  // что решать; «уже есть» меняет голоса другой идеи, поэтому после
  // решения список перечитывается целиком.
  const canDecideAny = list?.ideas.some((i) => i.canDecide) ?? false
  useEffect(() => {
    if (!canDecideAny) return
    let current = true
    api
      .listBoards()
      .then((r) => current && setBoards(r.boards.filter((b) => b.writable !== false)))
      .catch(() => {
        // Без списка форма решения предложит пустой выбор доски и объяснит отказ сервера.
      })
    return () => {
      current = false
    }
  }, [canDecideAny])

  const replace = (idea: Idea) =>
    setList((was) => was && { ...was, ideas: was.ideas.map((i) => (i.id === idea.id ? idea : i)) })
  const reload = () =>
    loadIdeas()
      .then(setList)
      .catch(() => {})

  const vote = (idea: Idea, on: boolean) => {
    if (voting) return
    setVoting(idea.id)
    setVoteError(null)
    voteIdea(idea.id, on)
      .then(replace)
      .catch((e) => setVoteError(e instanceof Error ? e.message : s.voteFailed))
      .finally(() => setVoting(null))
  }

  const shown = list ? sortIdeas(visibleIdeas(list.ideas, withDecided), sort) : []
  const setFlag = (key: string, on: boolean, value = '1') => {
    const next = new URLSearchParams(query)
    if (on) next.set(key, value)
    else next.delete(key)
    setQuery(next, { replace: true })
  }

  return (
    <div className="stack ideas-screen">
      <div className="row ideas-head">
        <Button kind="primary" onClick={() => setOpen(true)} aria-expanded={open} disabled={open}>
          {s.suggest}
        </Button>
        <label className="row row--tight">
          <span className="small">{s.sort}</span>
          <select value={sort} onChange={(e) => setFlag('sort', e.target.value === 'votes', 'votes')}>
            <option value="new">{s.sortNew}</option>
            <option value="votes">{s.sortVotes}</option>
          </select>
        </label>
        <label className="row row--tight small">
          <input type="checkbox" checked={withDecided} onChange={(e) => setFlag('decided', e.target.checked)} />
          <span>{s.withDecided}</span>
        </label>
        {list && (
          <span className="muted small">
            {shown.length === list.ideas.length ? s.count(list.ideas.length) : s.shownOf(shown.length, list.ideas.length)}
          </span>
        )}
      </div>
      <p className="muted small">
        {s.hint} <Hint topic="ideas" />
      </p>
      {open && (
        <IdeaForm
          teams={teams}
          existing={list?.ideas ?? []}
          onCreated={(idea) => {
            setList((was) => (was ? { ...was, ideas: [idea, ...was.ideas] } : { ideas: [idea], truncated: false }))
            setOpen(false)
          }}
          onCancel={() => setOpen(false)}
          onSupport={(idea) => {
            setOpen(false)
            if (!idea.voted) vote(idea, true)
          }}
        />
      )}
      <ScreenError>{error ?? voteError}</ScreenError>
      {!list && !error && <Skeleton lines={4} />}
      {list && list.ideas.length === 0 && !open && (
        <EmptyState title={s.empty} action={<Button onClick={() => setOpen(true)}>{s.suggest}</Button>}>
          {s.emptyHelp}
        </EmptyState>
      )}
      {list && list.ideas.length > 0 && shown.length === 0 && <p className="muted">{s.allDecided}</p>}
      {shown.length > 0 && (
        <ul className="ideas-list" aria-label={s.listCaption}>
          {shown.map((idea) => (
            <li key={idea.id}>
              <IdeaRow
                idea={idea}
                busy={voting === idea.id}
                boards={boards}
                others={list!.ideas.filter((i) => i.id !== idea.id && i.decision?.kind !== 'duplicate')}
                onVote={(on) => vote(idea, on)}
                onDecided={(next) => {
                  replace(next)
                  if (next.decision?.kind === 'duplicate') void reload()
                }}
              />
            </li>
          ))}
        </ul>
      )}
      {list?.truncated && <p className="muted small">{s.truncated(list.ideas.length)}</p>}
    </div>
  )
}
