import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { AppNotification } from '../../shared/api/index.ts'
import { topLayer, useAnchored } from '../../shared/ui/anchored.ts'
import { BellIcon } from '../../shared/ui/icons.tsx'
import { boardPath, navigate } from '../../shared/router/index.ts'
import { locale, t } from '../../shared/i18n/index.ts'

/**
 * Колокольчик в шапке (ROADMAP 29.3): счётчик непрочитанного и список
 * со ссылкой прямо на карточку.
 *
 * Счётчик живёт потоком по человеку, а не по доске: весть «перечитай»
 * приходит, где бы человек ни был. Сами уведомления в потоке не едут —
 * видимость доски перепроверяет чтение, и держать её в двух местах
 * значило бы однажды разойтись.
 *
 * Список — раскрывашка с обычными ссылками, а не меню: пункт ведёт
 * на карточку, и открыть его в новой вкладке — законное желание.
 */
export function Bell() {
  const [items, setItems] = useState<AppNotification[]>([])
  const [unread, setUnread] = useState(0)
  const [open, setOpen] = useState(false)
  const [failed, setFailed] = useState(false)
  // Объявление о новом — отдельной строкой в живом регионе: имя кнопки
  // диктор прочтёт, только когда на неё встанут.
  const [announce, setAnnounce] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const seen = useRef<number | null>(null)
  const panelId = useId()
  const box = useAnchored(open, buttonRef, panelRef, 'right', 'down')

  const load = useCallback(() => {
    api
      .notifications()
      .then((page) => {
        setItems(page.items)
        setUnread(page.unread)
        setFailed(false)
        if (seen.current !== null && page.unread > seen.current) setAnnounce(t.notifications.arrived)
        seen.current = page.unread
      })
      .catch(() => setFailed(true))
  }, [])

  useEffect(() => {
    load()
    // Там, где потока нет (разбор разметки в проверках), колокольчик
    // перечитывается при открытии — этого хватает.
    if (typeof EventSource === 'undefined') return
    const stream = new EventSource('/api/notifications/stream')
    stream.addEventListener('notifications', load)
    // Обрыв и переподключение EventSource делает сам; вернувшись,
    // перечитываем — пока его не было, могло прийти что угодно.
    stream.addEventListener('open', load)
    return () => stream.close()
  }, [load])

  // Щелчок вне закрывает, как у меню.
  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    return () => document.removeEventListener('pointerdown', onPointer)
  }, [open])

  const close = () => {
    setOpen(false)
    buttonRef.current?.focus()
  }

  const readAll = () => {
    setItems((list) => list.map((n) => ({ ...n, read: true })))
    setUnread(0)
    seen.current = 0
    api.readNotifications().catch(load)
  }

  const openOne = (n: AppNotification, e: React.MouseEvent) => {
    if (!n.read) {
      setItems((list) => list.map((x) => (x.id === n.id ? { ...x, read: true } : x)))
      setUnread((u) => Math.max(0, u - 1))
      seen.current = Math.max(0, (seen.current ?? 1) - 1)
      api.readNotifications([n.id]).catch(load)
    }
    // Средняя кнопка и Ctrl — новая вкладка, как у любой ссылки; иначе
    // переход внутри приложения, без перезагрузки.
    if (e.button === 0 && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
      e.preventDefault()
      setOpen(false)
      navigate(boardPath(n.boardId, n.cardId))
    }
  }

  return (
    <div
      className="bell"
      ref={rootRef}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && open) {
          e.stopPropagation()
          close()
        }
      }}
    >
      <button
        type="button"
        ref={buttonRef}
        className="btn btn--quiet bell-button"
        aria-label={t.notifications.button(unread)}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => {
          if (!open) load()
          setOpen((v) => !v)
        }}
      >
        <BellIcon />
        {unread > 0 && (
          <span className="bell-count" aria-hidden="true">
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>
      <span className="sr-only" role="status">
        {announce}
      </span>
      {open && (
        <div
          className="bell-panel"
          id={panelId}
          ref={panelRef}
          role="region"
          aria-label={t.notifications.title}
          popover={topLayer ? 'manual' : undefined}
          style={box ? { top: box.top, left: box.left } : { opacity: 0 }}
        >
          <div className="bell-head">
            <h2 className="section-title">{t.notifications.title}</h2>
            {unread > 0 && (
              <button type="button" className="link" onClick={readAll}>
                {t.notifications.readAll}
              </button>
            )}
          </div>
          {failed && <p className="error small">{t.notifications.failed}</p>}
          {!failed && items.length === 0 && <p className="muted small">{t.notifications.empty}</p>}
          <ul className="bell-list">
            {items.map((n) => (
              <li key={n.id} className={n.read ? 'bell-item' : 'bell-item bell-item--unread'}>
                <a href={boardPath(n.boardId, n.cardId)} onClick={(e) => openOne(n, e)}>
                  <span className="bell-what">{describe(n)}</span>
                  <span className="bell-card">
                    <span className="bell-number">{n.cardNumber}</span> {n.cardTitle}
                  </span>
                  <span className="bell-where muted small">
                    {n.boardName} · {ago(n.createdAt)}
                    {!n.read && <span className="sr-only">, {t.notifications.unreadMark}</span>}
                  </span>
                </a>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

function describe(n: AppNotification): string {
  const who = n.actorName ?? t.notifications.someone
  return t.notifications[n.reason](who)
}

/** «5 минут назад» на языке интерфейса. */
function ago(iso: string): string {
  const seconds = (new Date(iso).getTime() - Date.now()) / 1000
  const format = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' })
  const steps: [Intl.RelativeTimeFormatUnit, number][] = [
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, size] of steps) {
    if (Math.abs(seconds) >= size) return format.format(Math.round(seconds / size), unit)
  }
  return format.format(0, 'minute')
}
