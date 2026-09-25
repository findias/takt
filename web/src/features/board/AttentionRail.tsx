import { useState } from 'react'
import { t } from '../../shared/i18n/index.ts'
import { ChevronLeftIcon, ChevronRightIcon } from '../../shared/ui/icons.tsx'

/**
 * «Требует внимания» — справа от колонок (шаг 5 нового дизайна доски,
 * направление В).
 *
 * Доска отвечает на «что где лежит»; стоячая работа на ней видна, только
 * если пройти глазами каждую карточку. Панель собирает в один список то,
 * из-за чего работа не идёт: блокировки, работу дольше обещанного,
 * переполненные колонки и карточки кончившихся итераций. Каждая строка
 * ведёт туда, где с этим разбираются.
 *
 * Над доской места нет — органов там ровно столько, сколько позволено
 * (проверка «шапка доски не съедает экран»), — поэтому панель живёт
 * в поле доски и сворачивается в узкую полосу со счётчиком, как колонка.
 * Свёрнута ли она — удобство смотрящего, и помнит это его браузер.
 * Пусто — панели нет вовсе: «всё хорошо» не повод занимать треть экрана.
 */
export type AttentionItem =
  | { kind: 'blocked' | 'aging' | 'late'; cardId: string; number: string; title: string; note: string }
  | { kind: 'limit'; columnId: string; note: string }

const SHOWN = 12

export function AttentionRail({
  items,
  onOpenCard,
  onShowColumn,
  onFlow,
}: {
  items: AttentionItem[]
  onOpenCard: (cardId: string) => void
  onShowColumn: (columnId: string) => void
  onFlow: () => void
}) {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem('attention-collapsed') === '1'
    } catch {
      return false
    }
  })
  const toggle = (next: boolean) => {
    setCollapsed(next)
    try {
      localStorage.setItem('attention-collapsed', next ? '1' : '0')
    } catch {
      // без памяти браузера — просто не запомнится
    }
  }

  if (items.length === 0) return null

  if (collapsed) {
    return (
      <aside className="attention attention--collapsed" aria-label={t.attention.title}>
        <button className="btn btn--quiet attention-expand" onClick={() => toggle(false)} aria-label={t.attention.expand(items.length)}>
          <ChevronLeftIcon />
          <span className="attention-count">{items.length}</span>
        </button>
      </aside>
    )
  }

  const shown = items.slice(0, SHOWN)
  return (
    <aside className="attention" aria-label={t.attention.title}>
      <div className="attention-head">
        <h2 className="attention-title">{t.attention.title}</h2>
        <span className="count">{items.length}</span>
        <button className="btn btn--icon btn--quiet" onClick={() => toggle(true)} aria-label={t.attention.collapse}>
          <ChevronRightIcon />
        </button>
      </div>
      <ul className="attention-list">
        {shown.map((item) => (
          <li key={item.kind === 'limit' ? `limit:${item.columnId}` : `${item.kind}:${item.cardId}`}>
            <button
              className="attention-item"
              onClick={() => (item.kind === 'limit' ? onShowColumn(item.columnId) : onOpenCard(item.cardId))}
            >
              <span className={`mark mark--alarm${item.kind === 'aging' || item.kind === 'limit' ? ' mark--aging' : ''}`}>
                {t.attention[item.kind]}
              </span>
              <span className="attention-text">
                {item.kind === 'limit' ? (
                  item.note
                ) : (
                  <>
                    <span className="card-number">{item.number}</span> {item.title}
                    <span className="muted small attention-note">{item.note}</span>
                  </>
                )}
              </span>
            </button>
          </li>
        ))}
      </ul>
      {items.length > SHOWN && <p className="muted small">{t.attention.more(items.length - SHOWN)}</p>}
      <button className="link attention-flow" onClick={onFlow}>
        {t.attention.toFlow}
      </button>
    </aside>
  )
}
