import { useCallback, useEffect, useRef, useState } from 'react'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { api } from '../../shared/api/index.ts'
import type { Comment } from '../../shared/api/index.ts'
import { timeText } from '../../entities/feed/model.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import { ScreenError } from '../../shared/ui/Field'
import { t } from '../../shared/i18n/index.ts'

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
  people,
}: {
  boardId: string
  cardId: string
  meId: string
  canEdit: boolean
  /** Кого можно позвать в обсуждение: люди доски, имя по идентификатору. */
  people: Record<string, string>
}) {
  // Себя не зовут: уведомления о своём действии не бывает, и пункт
  // в меню обещал бы то, чего не случится.
  const callable = Object.entries(people).filter(([id]) => id !== meId)
  const [comments, setComments] = useState<Comment[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [replyTo, setReplyTo] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .comments(boardId, cardId)
      .then((r) => setComments(r.comments))
      .catch((e) => setError(e instanceof Error ? e.message : t.talk.readFailed))
  }, [boardId, cardId])

  useEffect(load, [load])

  const [toDelete, setToDelete] = useState<string | null>(null)
  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : t.common.notDone))
  }

  if (!comments) return <p className="muted small">{t.talk.loading}</p>

  const roots = comments.filter((c) => c.parentId === null)
  const repliesOf = (id: string) => comments.filter((c) => c.parentId === id)

  return (
    <section className="stack">
      <h3 className="section-title">{t.talk.title}</h3>
      {/* Удалённую реплику не вернуть — значит, спрашивает. Прежде
          удалял с первого нажатия (разбор 21.09.2026). */}
      <ConfirmDialog
        open={toDelete !== null}
        title={t.talk.deleteTitle}
        confirmLabel={t.talk.deleteReply}
        danger
        onCancel={() => setToDelete(null)}
        onConfirm={() => {
          const id = toDelete
          setToDelete(null)
          if (id) act(api.deleteComment(id))
        }}
      >
        <p>{t.talk.deleteBody}</p>
      </ConfirmDialog>
      <ScreenError>{error}</ScreenError>

      {roots.length === 0 && <p className="muted small">{t.talk.quiet}</p>}

      {roots.map((c) => (
        <div key={c.id} className="stack">
          <CommentRow
            comment={c}
            meId={meId}
            canEdit={canEdit}
            onEdit={(body) => act(api.editComment(c.id, body))}
            onDelete={() => setToDelete(c.id)}
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
                onDelete={() => setToDelete(r.id)}
              />
            ))}
            {replyTo === c.id && canEdit && (
              <NewComment
                placeholder={t.talk.reply}
                people={callable}
                onSend={(body, mentions) => {
                  act(api.addComment(boardId, cardId, body, c.id, mentions))
                  setReplyTo(null)
                }}
              />
            )}
          </div>
        </div>
      ))}

      {canEdit && (
        <NewComment
          placeholder={t.talk.write}
          people={callable}
          onSend={(body, mentions) => act(api.addComment(boardId, cardId, body, null, mentions))}
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
    return <p className="muted small comment comment--deleted">{t.talk.deleted}</p>
  }

  return (
    <div className="comment">
      <div className="row row--between">
        <span className="small">
          <strong>{comment.author ?? t.talk.noName}</strong>{' '}
          <span className="muted">{timeText(comment.createdAt)}</span>
        </span>
        <div className="row row--tight">
          {onReply && canEdit && (
            <button className="link" onClick={onReply}>
              {t.talk.reply}
            </button>
          )}
          {mine && canEdit && !editing && (
            <>
              <button className="link" onClick={() => setEditing(true)}>
                {t.talk.edit}
              </button>
              <button className="link link--danger" onClick={onDelete}>
                {t.talk.delete}
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
            aria-label={t.talk.text}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
          />
          <div className="row row--tight">
            <button type="submit">{t.common.save}</button>
            <Button
              kind="quiet"
              type="button"
              onClick={() => {
                setDraft(comment.body)
                setEditing(false)
              }}
            >
              {t.common.cancel}
            </Button>
          </div>
        </form>
      ) : (
        <p className="comment-body">{comment.body}</p>
      )}

      {comment.editedAt && !editing && (
        <div className="row row--tight">
          <span className="muted small">{t.talk.edited} {timeText(comment.editedAt)}</span>
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
            {was ? t.talk.hide : t.talk.previous}
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

/**
 * Новая реплика с упоминаниями (ROADMAP 29.1).
 *
 * Позвать — кнопкой «@» с меню людей доски, а не угадыванием по тексту:
 * меню доступно с клавиатуры и диктору, а имя в тексте набирают как
 * угодно — с отчеством, без, в падеже. Выбор вставляет «@Имя» и помечает
 * человека упомянутым; стёр имя из текста — упоминание снимается само:
 * позвать того, о ком в реплике не осталось ни слова, было бы странно.
 */
function NewComment({
  placeholder,
  people,
  onSend,
}: {
  placeholder: string
  people: [string, string][]
  onSend: (body: string, mentions: string[]) => void
}) {
  const [body, setBody] = useState('')
  const [called, setCalled] = useState<string[]>([])
  const fieldRef = useRef<HTMLTextAreaElement>(null)
  const nameOf = (id: string) => people.find(([pid]) => pid === id)?.[1] ?? ''
  return (
    <form
      className="stack"
      onSubmit={(e) => {
        e.preventDefault()
        const text = body.trim()
        if (!text) return
        onSend(
          text,
          called.filter((id) => text.includes('@' + nameOf(id))),
        )
        setBody('')
        setCalled([])
      }}
    >
      <textarea
        ref={fieldRef}
        className="description"
        rows={2}
        value={body}
        placeholder={placeholder}
        aria-label={placeholder}
        onChange={(e) => setBody(e.target.value)}
      />
      <div className="row row--tight">
        <button type="submit" disabled={!body.trim()}>
          {t.talk.send}
        </button>
        {people.length > 0 && (
          <Menu
            label={t.talk.mention}
            className="btn btn--quiet"
            align="left"
            items={people.map(([id, name]) => ({
              id,
              label: name,
              onSelect: () => {
                setBody((b) => `${b}${b && !b.endsWith(' ') ? ' ' : ''}@${name} `)
                setCalled((list) => (list.includes(id) ? list : [...list, id]))
                // Меню вернёт фокус на свою кнопку; писать дальше удобнее
                // из поля — после вставленного имени.
                requestAnimationFrame(() => fieldRef.current?.focus())
              },
            }))}
          >
            {t.talk.mentionButton}
          </Menu>
        )}
      </div>
    </form>
  )
}
