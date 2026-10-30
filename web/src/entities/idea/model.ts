// Вкладка «Идеи»: порядок списка и поиск похожего — чистые функции,
// проверяются без браузера.
import type { Idea } from '../../shared/api/index.ts'

export type IdeaSort = 'new' | 'votes'

/** Порядок — в адресе: ссылку «смотри, что поддержали» пересылают. */
export function parseIdeaSort(query: URLSearchParams): IdeaSort {
  return query.get('sort') === 'votes' ? 'votes' : 'new'
}

/** Новые — по дате; поддержанные — по голосам, при равенстве новее выше. */
export function sortIdeas(list: Idea[], sort: IdeaSort): Idea[] {
  const byDate = (a: Idea, b: Idea) => b.createdAt.localeCompare(a.createdAt)
  return [...list].sort(sort === 'votes' ? (a, b) => b.votes - a.votes || byDate(a, b) : byDate)
}

/**
 * Основы слов названия: без знаков, короче пяти букв не режем, длиннее —
 * первые пять. Грубее словаря, но ловит «шаблон — шаблоны — шаблона»,
 * а слова короче четырёх букв («для», «на», «чат») не считаются вовсе:
 * по ним похоже всё на всё.
 */
export function stems(title: string): Set<string> {
  const out = new Set<string>()
  for (const word of title.toLowerCase().split(/[^\p{L}\p{N}]+/u)) {
    if (word.length < 4) continue
    out.add(word.length > 5 ? word.slice(0, 5) : word)
  }
  return out
}

/**
 * Похожие идеи на набираемое название — чтобы поддержали существующую,
 * а не завели вторую такую же. Чем больше общих основ, тем выше;
 * при равенстве — поддержанная выше. Пусто, пока набрано слишком мало.
 */
export function similarIdeas(list: Idea[], title: string, limit = 3): Idea[] {
  const typed = stems(title)
  if (typed.size === 0) return []
  const scored: { idea: Idea; score: number }[] = []
  for (const idea of list) {
    let score = 0
    for (const stem of stems(idea.title)) if (typed.has(stem)) score++
    if (score > 0) scored.push({ idea, score })
  }
  scored.sort((a, b) => b.score - a.score || b.idea.votes - a.idea.votes)
  return scored.slice(0, limit).map((s) => s.idea)
}

/** «Анна, Борис и ещё 2»: первые имена целиком, остальное — числом. */
export function votersLine(voters: string[], shown = 3): { names: string; more: number } {
  return { names: voters.slice(0, shown).join(', '), more: Math.max(0, voters.length - shown) }
}

/** Решённая насовсем: взята, отклонена или названа двойником. «Позже» —
 *  всё ещё открытая: к ней вернутся, и прятать её из списка нельзя. */
export function isSettled(idea: Idea): boolean {
  const kind = idea.decision?.kind
  return kind === 'taken' || kind === 'declined' || kind === 'duplicate'
}

/** Список к показу: без решённых, если их не просили. */
export function visibleIdeas(list: Idea[], withDecided: boolean): Idea[] {
  return withDecided ? list : list.filter((i) => !isSettled(i))
}
