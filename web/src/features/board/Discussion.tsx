import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import { useAutoGrow } from '../../shared/lib/useAutoGrow.ts'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { api } from '../../shared/api/index.ts'
import type { Comment } from '../../shared/api/index.ts'
import { timeText } from '../../entities/feed/model.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { Menu } from '../../shared/ui/Menu.tsx'
import { ScreenError } from '../../shared/ui/Field'
import { t } from '../../shared/i18n/index.ts'

/** Общая хронология, ответы сохраняют ссылку на исходную реплику. */
export function Discussion({
  boardId,
  cardId,
  meId,
  canEdit,
  people,
  revision = 0,
}: {
  boardId: string
  cardId: string
  meId: string
  canEdit: boolean
  people: Record<string, string>
  /** Версия снимка: чужая реплика приходит через уже открытый поток доски. */
  revision?: number
}) {
  const callable = Object.entries(people).filter(([id]) => id !== meId)
  const [comments, setComments] = useState<Comment[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [replyTo, setReplyTo] = useState<string | null>(null)
  const [toDelete, setToDelete] = useState<string | null>(null)
  const [away, setAway] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const contentRef = useRef<HTMLDivElement>(null)
  const fieldRef = useRef<HTMLTextAreaElement>(null)
  const follow = useRef(true)
  const request = useRef(0)
  const load = useCallback(async () => {
    const current = ++request.current
    try {
      const result = await api.comments(boardId, cardId)
      if (current === request.current) {
        setComments(result.comments)
        setError(null)
      }
    } catch (e) {
      if (current === request.current) setError(e instanceof Error ? e.message : t.talk.readFailed)
    }
  }, [boardId, cardId])
  useEffect(() => {
    void load()
    return () => { request.current++ }
  }, [load, revision])

  const bottom = useCallback(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
    follow.current = true
    setAway(false)
  }, [])
  // До отрисовки первого кадра открываем хвост; при чтении истории
  // сохраняем место. ResizeObserver учитывает рост поля и смену размера окна.
  useLayoutEffect(() => {
    if (follow.current) bottom()
  }, [comments, bottom])
  useEffect(() => {
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => { if (follow.current) bottom() })
    if (scrollRef.current) observer.observe(scrollRef.current)
    if (contentRef.current) observer.observe(contentRef.current)
    return () => observer.disconnect()
  }, [bottom])

  const act = async (p: Promise<unknown>) => {
    setError(null)
    try { await p; await load() }
    catch (e) { setError(e instanceof Error ? e.message : t.common.notDone) }
  }
  const byId = new Map(comments?.map((c) => [c.id, c]))
  const reply = replyTo ? byId.get(replyTo) : undefined
  return (
    <section className="discussion" aria-label={t.talk.title}>
      <ConfirmDialog
        open={toDelete !== null}
        title={t.talk.deleteTitle}
        confirmLabel={t.talk.deleteReply}
        danger
        onCancel={() => setToDelete(null)}
        onConfirm={() => {
          const id = toDelete
          setToDelete(null)
          if (id) void act(api.deleteComment(id))
        }}
      >
        <p>{t.talk.deleteBody}</p>
      </ConfirmDialog>
      <ScreenError>{error}</ScreenError>
      {error && !comments && <button onClick={() => void load()}>{t.talk.retry}</button>}
      <div className="discussion-messages" ref={scrollRef} role="log" aria-label={t.talk.title}
        tabIndex={0} onScroll={() => {
          const el = scrollRef.current!
          follow.current = el.scrollHeight - el.clientHeight - el.scrollTop <= 48
          setAway(!follow.current)
        }}>
        <div className="discussion-content" ref={contentRef}>
          {!comments && !error && <p className="muted small">{t.talk.loading}</p>}
          {comments?.length === 0 && <p className="muted small">{t.talk.quiet}</p>}
          {comments?.map((c) => (
            <article key={c.id} className={`discussion-message${c.authorId === meId ? ' discussion-message--mine' : ''}`}>
              {c.parentId && byId.has(c.parentId) && (
                <div className="discussion-quote small">
                  <strong>{byId.get(c.parentId)!.author ?? t.talk.noName}</strong>
                  <p>{byId.get(c.parentId)!.deleted ? t.talk.deleted : byId.get(c.parentId)!.body}</p>
                </div>
              )}
              <CommentRow comment={c} meId={meId} canEdit={canEdit}
                onEdit={(body) => void act(api.editComment(c.id, body))}
                onDelete={() => setToDelete(c.id)}
                onReply={c.parentId === null ? () => {
                  setReplyTo(c.id)
                  fieldRef.current?.focus({ preventScroll: true })
                } : undefined} />
            </article>
          ))}
        </div>
      </div>
      {away && <button className="btn btn--quiet discussion-latest" onClick={bottom}>{t.talk.latest}</button>}
      {canEdit && comments && (
        <div className="discussion-composer">
          {reply && <div className="discussion-reply">
            <div className="discussion-quote small"><strong>{t.talk.replyingTo} {reply.author ?? t.talk.noName}</strong>
              <p>{reply.deleted ? t.talk.deleted : reply.body}</p></div>
            <button className="link" onClick={() => { setReplyTo(null); fieldRef.current?.focus() }}>{t.common.cancel}</button>
          </div>}
          <NewComment fieldRef={fieldRef} placeholder={t.talk.write} people={callable}
            onSend={async (body, mentions) => {
              setError(null)
              try {
                await api.addComment(boardId, cardId, body, replyTo, mentions)
                follow.current = true
                setReplyTo(null)
                await load()
                return true
              } catch (e) {
                setError(e instanceof Error ? e.message : t.common.notDone)
                return false
              }
            }} />
        </div>
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
  const editRef = useRef<HTMLTextAreaElement>(null)
  useAutoGrow(editRef, draft)
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
            ref={editRef}
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
  fieldRef,
}: {
  placeholder: string
  people: [string, string][]
  onSend: (body: string, mentions: string[]) => Promise<boolean>
  fieldRef: RefObject<HTMLTextAreaElement | null>
}) {
  const [body, setBody] = useState('')
  const [called, setCalled] = useState<string[]>([])
  const [sending, setSending] = useState(false)
  const pending = useRef(false)
  useAutoGrow(fieldRef, body)
  const nameOf = (id: string) => people.find(([pid]) => pid === id)?.[1] ?? ''
  return (
    <form
      className="stack"
      aria-busy={sending}
      onSubmit={async (e) => {
        e.preventDefault()
        const text = body.trim()
        if (!text || pending.current) return
        pending.current = true
        setSending(true)
        try {
          if (await onSend(text, called.filter((id) => text.includes('@' + nameOf(id))))) {
            setBody('')
            setCalled([])
            fieldRef.current?.focus({ preventScroll: true })
          }
        } finally {
          pending.current = false
          setSending(false)
        }
      }}
    >
      <textarea
        ref={fieldRef}
        className="description"
        rows={2}
        value={body}
        placeholder={placeholder}
        aria-label={placeholder}
        readOnly={sending}
        onChange={(e) => setBody(e.target.value)}
      />
      <div className="row row--tight">
        <button type="submit" disabled={!body.trim() || sending}>
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
              disabled: sending,
              onSelect: () => {
                if (pending.current) return
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
