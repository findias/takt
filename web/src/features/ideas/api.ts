// Вызовы вкладки «Идеи» — здесь, а не в общем клиенте `api`: общий
// клиент едет в первую загрузку каждого экрана, а этот экран грузится
// отдельно (порог размера первой загрузки — web/e2e/perf.spec.ts).
import { request } from '../../shared/api/index.ts'
import type { Idea, IdeaDecision, IdeaDecisionKind } from '../../shared/api/index.ts'

export type IdeaList = { ideas: Idea[]; truncated: boolean }

export const loadIdeas = () => request<IdeaList>('GET', '/api/ideas')

export const createIdea = (title: string, body: string, teamId: string | null) =>
  request<Idea>('POST', '/api/ideas', { title, body, teamId })

/** Голос ставится и снимается; ответ — идея целиком, с числом и именами. */
export const voteIdea = (id: string, on: boolean) =>
  request<Idea>(on ? 'PUT' : 'DELETE', `/api/ideas/${id}/vote`)

export type DecideBody = { decision: IdeaDecisionKind; note: string; boardId?: string; intoIdeaId?: string }

/** Решение записывается; ответ — идея целиком с нынешним решением. */
export const decideIdea = (id: string, body: DecideBody) => request<Idea>('POST', `/api/ideas/${id}/decisions`, body)

export const loadDecisions = (id: string) =>
  request<{ decisions: IdeaDecision[] }>('GET', `/api/ideas/${id}/decisions`)
