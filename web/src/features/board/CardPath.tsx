import { useEffect, useState } from 'react'
import { request } from '../../shared/api/index.ts'
import { boardPath, navigate } from '../../shared/router/index.ts'
import { t } from '../../shared/i18n/index.ts'

export type PathStep = {
  id: string
  visible: boolean
  number?: string
  title?: string
  boardId?: string
  boardName?: string
}

const loadPath = (cardId: string) => request<{ path: PathStep[] }>('GET', `/api/cards/${cardId}/path`)

/** Сколько звеньев показывать целиком. Длиннее — середина сворачивается
 *  в «…», а вся цепочка остаётся в подсказке: пять уровней названиями
 *  карточек не помещаются над заголовком панели. */
const SHOWN = 3

/**
 * Путь до корня дерева над названием карточки (этап 32.2): «Эпик › Фича».
 *
 * Панель знает только прямого родителя, а эпик над ним часто лежит
 * на другой доске — путь спрашивается у сервера, одним запросом, когда
 * карточку открыли, и только если родитель есть: у самостоятельной
 * работы пути нет, и спрашивать о нём незачем.
 *
 * Звено этой доски открывает карточку здесь же, звено другой доски
 * подписано доской и ведёт туда ссылкой. Недоступное звено названо
 * «недоступной карточкой», а не пропадает: иначе путь выглядел бы
 * короче, чем он есть.
 */
export function CardPath({
  cardId,
  parentId,
  boardId,
  onOpenCard,
}: {
  cardId: string
  /** Прямой родитель; пусто — карточка сама корень. Ключ перезапроса:
   *  сменился родитель — сменился путь. */
  parentId: string | null
  boardId: string
  onOpenCard: (cardId: string) => void
}) {
  const [path, setPath] = useState<PathStep[]>([])

  useEffect(() => {
    if (!parentId) {
      setPath([])
      return
    }
    let current = true
    loadPath(cardId)
      .then((r) => current && setPath(r.path))
      // Молча: путь — подсказка к карточке, панель работает и без него.
      .catch(() => current && setPath([]))
    return () => {
      current = false
    }
  }, [cardId, parentId])

  if (path.length === 0) return null

  const name = (step: PathStep) => (step.visible ? (step.title ?? '') : t.board.unknownParent)
  const whole = path.map(name).join(' › ')
  const shown: (PathStep | null)[] =
    path.length > SHOWN ? [path[0], null, ...path.slice(-(SHOWN - 1))] : path

  return (
    <nav className="card-path small" aria-label={t.panel.path}>
      <ol>
        {shown.map((step, i) => (
          <li key={step ? step.id : `gap-${i}`}>
            {step === null ? (
              <span title={whole} aria-label={t.panel.pathHidden(path.length - SHOWN)}>
                …
              </span>
            ) : !step.visible ? (
              <span className="muted">{t.board.unknownParent}</span>
            ) : step.boardId === boardId ? (
              <button type="button" className="link" onClick={() => onOpenCard(step.id)}>
                {step.title}
              </button>
            ) : (
              <>
                {/* Ссылкой — чтобы открывалась и в новой вкладке; обычное
                    нажатие переходит без перезагрузки, как везде. */}
                <a
                  className="link"
                  href={boardPath(step.boardId ?? '', step.id)}
                  onClick={(e) => {
                    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
                    e.preventDefault()
                    navigate(boardPath(step.boardId ?? '', step.id))
                  }}
                >
                  {step.title}
                </a>
                <span className="muted card-path-board">· {step.boardName}</span>
              </>
            )}
          </li>
        ))}
      </ol>
    </nav>
  )
}
