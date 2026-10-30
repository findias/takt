import { useEffect, useState } from 'react'
import { api } from '../shared/api/index.ts'
import type { BoardInfo, Idea, Principal, Team, TeamAdmin } from '../shared/api/index.ts'
import { setQuery, useQuery } from '../shared/router/index.ts'
import { ScreenError } from '../shared/ui/Field.tsx'
import { EmptyState, Skeleton } from '../shared/ui/states.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { Hint } from '../shared/ui/Hint.tsx'
import { t } from '../shared/i18n/index.ts'
import { levelIdeas, parseIdeaLevel, parseIdeaSort, sortIdeas, visibleIdeas, waitingIdeas } from '../entities/idea/model.ts'
import { buildTree, invitableTeams } from '../entities/team/model.ts'
import { IdeaForm } from '../features/ideas/IdeaForm.tsx'
import { IdeaRow } from '../features/ideas/IdeaRow.tsx'
import type { MoveTarget } from '../features/ideas/MoveForm.tsx'
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
  const waitingOnly = query.get('waiting') === '1'
  const level = parseIdeaLevel(query)
  const s = t.ideas
  const isOwner = principal.role === 'owner'

  const [list, setList] = useState<IdeaList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [voteError, setVoteError] = useState<string | null>(null)
  const [teams, setTeams] = useState<Team[]>([])
  const [admins, setAdmins] = useState<TeamAdmin[]>([])
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

  // Подразделения нужны отбору «кому» над списком, форме и переносу —
  // грузятся сразу. Без них экран всё равно работает: отбор предложит
  // только «всей организации», идея уйдёт туда же.
  useEffect(() => {
    let current = true
    api
      .listTeams()
      .then((r) => current && setTeams(r.teams))
      .catch(() => {})
    return () => {
      current = false
    }
  }, [principal.orgId])

  // Доски нужны только тому, кто решает, — и грузятся, когда есть
  // что решать; «уже есть» меняет голоса другой идеи, поэтому после
  // решения список перечитывается целиком.
  const canDecideAny = list?.ideas.some((i) => i.canDecide) ?? false
  // Владельцу подразделения переносить можно внутрь своей области —
  // какая она, знают записи администраторов; владельцу организации
  // они не нужны, и запроса нет.
  useEffect(() => {
    if (!canDecideAny || isOwner) return
    let current = true
    api
      .listAdmins()
      .then((r) => current && setAdmins(r.admins))
      .catch(() => {})
    return () => {
      current = false
    }
  }, [canDecideAny, isOwner])
  const tree = buildTree(teams)
  // Все подразделения в порядке дерева — для отбора и формы подачи.
  const levels = invitableTeams(tree, true, [])
  const adminRoots = admins.filter((a) => a.userId === principal.id).map((a) => a.teamId)
  // Автор своей нерешённой идеи переносит её куда угодно — как владелец;
  // остальным — своя область. Отказ всё равно за базой (0087).
  const targetsFor = (idea: Idea): MoveTarget[] =>
    invitableTeams(tree, isOwner || (idea.authorId === principal.id && idea.decision === null), adminRoots)
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

  const waiting = list ? waitingIdeas(list.ideas) : []
  const shown = list
    ? sortIdeas(levelIdeas(waitingOnly ? waiting : visibleIdeas(list.ideas, withDecided), level, teams), sort)
    : []

  // Из колокольчика приходят к одной идее: адрес несёт её в #idea-…,
  // а список приезжает позже адреса — прокрутить можно только теперь.
  useEffect(() => {
    if (!list || !location.hash.startsWith('#idea-')) return
    const target = document.getElementById(location.hash.slice(1))
    target?.scrollIntoView({ block: 'center' })
    target?.closest<HTMLElement>('.idea')?.focus()
  }, [list])
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
        <label className="row row--tight">
          <span className="small">{s.level}</span>
          <select value={level} onChange={(e) => setFlag('level', e.target.value !== 'all', e.target.value)}>
            <option value="all">{s.levelAll}</option>
            <option value="org">{s.wholeOrg}</option>
            {levels.map((tm) => (
              <option key={tm.id} value={tm.id}>
                {'\u00a0\u00a0'.repeat(tm.level) + tm.name}
              </option>
            ))}
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
      {/* Идеи, лежащие без ответа: разбор «когда накопится» не случается
          сам, и список говорит об этом первым, а не автор через месяц. */}
      {waiting.length > 0 && (
        <button
          type="button"
          className={waitingOnly ? 'mark mark--aging ideas-waiting ideas-waiting--on' : 'mark mark--aging ideas-waiting'}
          aria-pressed={waitingOnly}
          onClick={() => setFlag('waiting', !waitingOnly)}
        >
          {s.waiting(waiting.length)}
        </button>
      )}
      {waitingOnly && <p className="muted small">{s.waitingOnly}</p>}
      {open && (
        <IdeaForm
          teams={levels}
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
                targets={targetsFor}
                onVote={(on) => vote(idea, on)}
                onDecided={(next) => {
                  replace(next)
                  if (next.decision?.kind === 'duplicate') void reload()
                }}
                onMoved={replace}
              />
            </li>
          ))}
        </ul>
      )}
      {list?.truncated && <p className="muted small">{s.truncated(list.ideas.length)}</p>}
    </div>
  )
}
