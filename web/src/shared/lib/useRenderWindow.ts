import { useEffect, useRef, useState } from 'react'

/**
 * Окно отрисовки длинного списка.
 *
 * Не виртуализация: показанное остаётся в разметке, а не подменяется
 * распорками. Разница важна — виртуализация ломает `Ctrl+F`, врёт
 * диктору про размер списка (`aria-setsize`) и теряет фокус
 * на размонтированных узлах; здесь же список просто дорисовывается
 * по мере прокрутки и назад не сворачивается.
 *
 * Приём был написан в колонке доски и повторён в таблице — а два
 * экземпляра одного правила однажды расходятся. Отсюда общий хук:
 * разметку каждый делает свою (в колонке хвост — абзац, в таблице —
 * строка), а правило одно.
 *
 * Запас в четыреста пикселей: строки должны появляться до того, как
 * человек доскроллит до пустоты, иначе список выглядит оборвавшимся.
 */
export function useRenderWindow<E extends HTMLElement>(total: number, chunk = 100) {
  const [limit, setLimit] = useState(chunk)
  const tail = useRef<E>(null)

  useEffect(() => {
    const element = tail.current
    if (!element || total <= limit) return
    const watcher = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setLimit((l) => l + chunk)
      },
      { rootMargin: '400px' },
    )
    watcher.observe(element)
    return () => watcher.disconnect()
  }, [chunk, limit, total])

  return {
    limit,
    tail,
    /** Сколько ещё не показано. Ноль — хвоста нет. */
    rest: Math.max(0, total - limit),
    /** Показать следующую порцию: хвост принимает фокус с клавиатуры,
     *  и прокрутки при этом может не случиться вовсе. */
    more: () => setLimit((l) => l + chunk),
  }
}
