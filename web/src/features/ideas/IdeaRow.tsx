import { useState } from 'react'
import type { BoardInfo, Idea, IdeaDecision } from '../../shared/api/index.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'
import { boardPath, navigate } from '../../shared/router/index.ts'
import { timeText } from '../../entities/feed/model.ts'
import { votersLine } from '../../entities/idea/model.ts'
import { DecisionForm } from './DecisionForm.tsx'
import { loadDecisions } from './api.ts'

/**
 * Одна идея в списке: название, слова автора, кто и когда, кому,
 * решение и голос.
 *
 * Поддержавшие названы по именам, а не только числом: «+1» без имён
 * читается как счётчик, а поддержка людей — это люди. Кнопка —
 * переключатель (`aria-pressed`): свой голос снимается ею же.
 */
export function IdeaRow({
  idea,
  busy,
  boards,
  others,
  onVote,
  onDecided,
}: {
  idea: Idea
  busy: boolean
  boards: BoardInfo[]
  others: Idea[]
  onVote: (on: boolean) => void
  onDecided: (idea: Idea) => void
}) {
  const s = t.ideas
  const who = votersLine(idea.voters)
  const [deciding, setDeciding] = useState(false)
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
        {idea.decision && <DecisionLine decision={idea.decision} />}
        {idea.decision && <History idea={idea} />}
        {idea.canDecide && !deciding && (
          <Button kind="quiet" className="idea-decide-open" onClick={() => setDeciding(true)}>
            {s.decide}
          </Button>
        )}
        {deciding && (
          <DecisionForm
            idea={idea}
            boards={boards}
            others={others}
            onDecided={(next) => {
              setDeciding(false)
              onDecided(next)
            }}
            onCancel={() => setDeciding(false)}
          />
        )}
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

/** Решение словами: слово, куда оно ведёт, пояснение, кто и когда. */
function DecisionLine({ decision }: { decision: IdeaDecision }) {
  const s = t.ideas
  return (
    <div className="idea-decision stack stack--tight">
      <div className="row row--tight">
        <span className={`mark idea-decision-mark idea-decision-mark--${decision.kind}`}>{s.kinds[decision.kind]}</span>
        {decision.kind === 'taken' &&
          (decision.card ? (
            <button
              className="link small"
              onClick={() => navigate(boardPath(decision.card!.boardId, decision.card!.id))}
            >
              {decision.card.done
                ? s.cardDone(decision.card.number, decision.card.boardName)
                : s.cardAt(decision.card.number, decision.card.boardName, decision.card.column)}
            </button>
          ) : (
            <span className="muted small">{s.cardGone}</span>
          ))}
        {decision.kind === 'duplicate' && decision.into && <span className="small">→ {decision.into.title}</span>}
        <span className="muted small">{s.decidedBy(decision.author ?? t.talk.noName, timeText(decision.at))}</span>
      </div>
      {decision.note && <p className="idea-decision-note">{decision.note}</p>}
    </div>
  )
}

/** История решений — по требованию: одна запись у большинства идей,
 *  и грузить историю всем ради редких двух — лишний запрос. */
function History({ idea }: { idea: Idea }) {
  const s = t.ideas
  const [history, setHistory] = useState<IdeaDecision[] | null>(null)
  const [failed, setFailed] = useState(false)
  const open = () => {
    if (history) return
    loadDecisions(idea.id)
      .then((r) => setHistory(r.decisions))
      .catch(() => setFailed(true))
  }
  return (
    <details className="idea-history" onToggle={(e) => e.currentTarget.open && open()}>
      <summary className="small">{history ? s.history(history.length) : s.historyClosed}</summary>
      {failed && <p className="muted small">{s.historyFailed}</p>}
      {!failed && history === null && <p className="muted small">{s.historyLoading}</p>}
      {history && (
        <ul className="idea-history-list">
          {history.map((d) => (
            <li key={d.id}>
              <DecisionLine decision={d} />
            </li>
          ))}
        </ul>
      )}
    </details>
  )
}
