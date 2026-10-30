import type { Idea } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'
import { timeText } from '../../entities/feed/model.ts'
import { votersLine } from '../../entities/idea/model.ts'

/**
 * Одна идея в списке: название, слова автора, кто и когда, кому, и голос.
 *
 * Поддержавшие названы по именам, а не только числом: «+1» без имён
 * читается как счётчик, а поддержка людей — это люди. Кнопка —
 * переключатель (`aria-pressed`): свой голос снимается ею же.
 */
export function IdeaRow({ idea, busy, onVote }: { idea: Idea; busy: boolean; onVote: (on: boolean) => void }) {
  const s = t.ideas
  const who = votersLine(idea.voters)
  return (
    <article className="idea" aria-labelledby={`idea-${idea.id}`}>
      <div className="idea-main">
        <h3 className="idea-title" id={`idea-${idea.id}`}>
          {idea.title}
        </h3>
        {idea.body && <p className="idea-body">{idea.body}</p>}
        <p className="muted small idea-meta">
          <span>{s.by(idea.author ?? t.talk.noName, timeText(idea.createdAt))}</span>
          {idea.team && <span>{s.forTeam(idea.team)}</span>}
        </p>
      </div>
      <div className="idea-vote">
        <Button
          kind={idea.voted ? 'default' : 'quiet'}
          aria-pressed={idea.voted}
          aria-label={s.voteOf(idea.title)}
          busy={busy}
          onClick={() => onVote(!idea.voted)}
        >
          {idea.voted ? s.supported : s.support} · {idea.votes}
        </Button>
        <p className="muted small idea-voters">
          {idea.votes === 0
            ? s.nobodyYet
            : s.supportedBy(who.names) + (who.more > 0 ? s.andMore(who.more) : '')}
        </p>
      </div>
    </article>
  )
}
