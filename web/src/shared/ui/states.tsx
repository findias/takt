import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Button } from './Button.tsx'

/**
 * Состояния экрана: загрузка, ошибка, пустота.
 *
 * У каждого экрана их должно быть пять — пусто, загрузка, ошибка,
 * частичная загрузка, обычное, — и пропуск любого превращается
 * в «ничего не происходит». До сих пор на все случаи было одно слово
 * «Загружаем…», а ошибка выглядела как пустой экран.
 */

/**
 * Показывать не раньше, чем через задержку.
 *
 * Мигание хуже ожидания: заглушка, мелькнувшая на сто миллисекунд,
 * читается как сбой отрисовки. Двести — обычный порог, за которым
 * человек начинает подозревать, что «не нажалось».
 */
export function useDelayed(delay = 200): boolean {
  const [ready, setReady] = useState(false)
  useEffect(() => {
    const timer = window.setTimeout(() => setReady(true), delay)
    return () => window.clearTimeout(timer)
  }, [delay])
  return ready
}

/**
 * Заглушка в форме будущего содержимого.
 *
 * Не спиннер: крутящийся кружок сообщает «жди», а полосы на месте
 * будущих строк — «сейчас здесь будет список», и человек успевает
 * привыкнуть к раскладке до того, как она наполнится.
 */
export function Skeleton({ lines = 3, className }: { lines?: number; className?: string }) {
  const ready = useDelayed()
  if (!ready) return null
  return (
    <div className={className ? `skeleton ${className}` : 'skeleton'} aria-hidden="true">
      {Array.from({ length: lines }, (_, i) => (
        <span key={i} className="skeleton-line" />
      ))}
    </div>
  )
}

/** Доска целиком: три колонки с карточками-заглушками. */
export function BoardSkeleton() {
  const ready = useDelayed()
  if (!ready) return null
  return (
    <div className="columns" aria-hidden="true">
      {[0, 1, 2].map((i) => (
        <div className="column" key={i}>
          <Skeleton lines={1} className="skeleton--title" />
          <Skeleton lines={i === 0 ? 3 : 2} className="skeleton--cards" />
        </div>
      ))}
    </div>
  )
}

/**
 * Ошибка.
 *
 * С причиной и с кнопкой: «что-то пошло не так» без причины
 * не помогает никому, а без кнопки человеку остаётся перезагружать
 * страницу — то есть чинить наугад.
 */
export function ErrorState({
  what,
  error,
  onRetry,
}: {
  /** Что не получилось: «загрузить доску», «прочитать команду». */
  what: string
  error: string
  onRetry?: () => void
}) {
  return (
    <div className="state state--error" role="alert">
      <p className="state-title">Не удалось {what}</p>
      <p className="muted small">{error}</p>
      {onRetry && (
        <Button kind="primary" onClick={onRetry}>
          Повторить
        </Button>
      )}
    </div>
  )
}

/**
 * Пустота.
 *
 * Объясняет, а не констатирует: «нет данных» — это сообщение
 * о состоянии базы, а человеку нужно знать, что делать дальше.
 */
export function EmptyState({
  title,
  children,
  action,
}: {
  title: string
  children?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="state" role="note">
      <p className="state-title">{title}</p>
      {children && <p className="muted small">{children}</p>}
      {action}
    </div>
  )
}
