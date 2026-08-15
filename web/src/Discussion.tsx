import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import type { Comment } from './api'
import { timeText } from './feedModel'

/**
 * Обсуждение карточки.
 *
 * Ветки глубиной в один уровень: ответ на ответ читать невозможно, и все,
 * кто пробовал, к этому пришли. Удалённая реплика остаётся на месте — на
 * неё ссылаются ответы, — но без текста: вырезав её, мы разорвали бы
 * ветку, в которой отвечали живым людям.
 */
export function Discussion({
  boardId,
  cardId,
  meId,
  canEdit,
}: {
  boardId: string
  cardId: string
  meId: string
  canEdit: boolean
}) {
  const [comments, setComments] = useState<Comment[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [replyTo, setReplyTo] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .comments(boardId, cardId)
      .then((r) => setComments(r.comments))
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось прочитать обсуждение'))
  }, [boardId, cardId])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  if (!comments) return <p className="muted small">Загружаем обсуждение…</p>

  const roots = comments.filter((c) => c.parentId === null)
  const repliesOf = (id: string) => comments.filter((c) => c.parentId === id)

  return (
    <section className="stack">
      <h3 className="section-title">Обсуждение</h3>
      {error && <p className="error">{error}</p>}

      {roots.length === 0 && <p className="muted small">Пока тихо.</p>}

      {roots.map((c) => (
        <div key={c.id} className="stack">
          <CommentRow
            comment={c}
            meId={meId}
            canEdit={canEdit}
            onEdit={(body) => act(api.editComment(c.id, body))}
            onDelete={() => act(api.deleteComment(c.id))}
            onReply={() => setReplyTo(replyTo === c.id ? null : c.id)}
          />
          <div className="replies">
            {repliesOf(c.id).map((r) => (
              <CommentRow
                key={r.id}
                comment={r}
                meId={meId}
                canEdit={canEdit}
                onEdit={(body) => act(api.editComment(r.id, body))}
                onDelete={() => act(api.deleteComment(r.id))}
              />
            ))}
            {replyTo === c.id && canEdit && (
              <NewComment
                placeholder="Ответить"
                onSend={(body) => {
                  act(api.addComment(boardId, cardId, body, c.id, []))
                  setReplyTo(null)
                }}
              />
            )}
          </div>
        </div>
      ))}

      {canEdit && (
        <NewComment
          placeholder="Написать в обсуждение"
          onSend={(body) => act(api.addComment(boardId, cardId, body, null, []))}
        />
      )}
    </section>
  )
}

function CommentRow({
  comment,
  meId,
  canEdit,
  onEdit,
  onDelete,
  onReply,
}: {
  comment: Comment
  meId: string
  canEdit: boolean
  onEdit: (body: string) => void
  onDelete: () => void
  onReply?: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(comment.body)
  const [was, setWas] = useState<string[] | null>(null)
  const mine = comment.authorId === meId

  if (comment.deleted) {
    return <p className="muted small comment comment--deleted">Реплика удалена</p>
  }

  return (
    <div className="comment">
      <div className="row row--between">
        <span className="small">
          <strong>{comment.author ?? 'без имени'}</strong>{' '}
          <span className="muted">{timeText(comment.createdAt)}</span>
        </span>
        <div className="row row--tight">
          {onReply && canEdit && (
            <button className="link" onClick={onReply}>
              Ответить
            </button>
          )}
          {mine && canEdit && !editing && (
            <>
              <button className="link" onClick={() => setEditing(true)}>
                Править
              </button>
              <button className="link" onClick={onDelete}>
                Удалить
              </button>
            </>
          )}
        </div>
      </div>

      {editing ? (
        <form
          className="stack"
          onSubmit={(e) => {
            e.preventDefault()
            if (!draft.trim()) return
            onEdit(draft.trim())
            setEditing(false)
          }}
        >
          <textarea
            className="description"
            rows={3}
            autoFocus
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
          />
          <div className="row row--tight">
            <button type="submit">Сохранить</button>
            <button
              type="button"
              className="link"
              onClick={() => {
                setDraft(comment.body)
                setEditing(false)
              }}
            >
              Отмена
            </button>
          </div>
        </form>
      ) : (
        <p className="comment-body">{comment.body}</p>
      )}

      {comment.editedAt && !editing && (
        <div className="row row--tight">
          <span className="muted small">изменено {timeText(comment.editedAt)}</span>
          {/* «Изменено» без прежнего текста бесполезно: спрашивают
              не «правил ли он», а «что там было написано до». */}
          <button
            className="link"
            onClick={() =>
              was
                ? setWas(null)
                : void api
                    .commentRevisions(comment.id)
                    .then((r) => setWas(r.revisions))
                    .catch(() => setWas([]))
            }
          >
            {was ? 'скрыть' : 'что было'}
          </button>
        </div>
      )}
      {was?.map((body, i) => (
        <p key={i} className="muted small comment-body comment--previous">
          {body}
        </p>
      ))}
    </div>
  )
}

function NewComment({
  placeholder,
  onSend,
}: {
  placeholder: string
  onSend: (body: string) => void
}) {
  const [body, setBody] = useState('')
  return (
    <form
      className="stack"
      onSubmit={(e) => {
        e.preventDefault()
        if (!body.trim()) return
        onSend(body.trim())
        setBody('')
      }}
    >
      <textarea
        className="description"
        rows={2}
        value={body}
        placeholder={placeholder}
        aria-label={placeholder}
        onChange={(e) => setBody(e.target.value)}
      />
      <button type="submit" disabled={!body.trim()}>
        Отправить
      </button>
    </form>
  )
}
