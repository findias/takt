import { useCallback, useState } from 'react'

/**
 * Ширина боковой панели.
 *
 * Одной ширины мало: карточку с длинным обсуждением или описанием
 * в 26rem читают построчно по три слова, а рядом с доской на широком
 * экране место есть. Ширину тянут за левую кромку панели.
 *
 * Хранится так же, как ширина колонки (`columnWidth.ts`): в браузере
 * смотрящего, одна на все доски. Панель одна и та же везде, и тянуть
 * её заново на каждой доске никто не станет.
 *
 * Единица — rem: ширина растёт вместе с текстом.
 */

/** Прежняя ширина — она же ширина, которую не трогали. */
export const PANEL_WIDTH_DEFAULT = 26
/** Уже — вкладки карточки перестают помещаться в строку. */
export const PANEL_WIDTH_MIN = 20
/** Шире панель на обычном мониторе закрывает доску целиком; для этого
 *  есть режим «Во весь экран». Сколько доске остаётся на узком окне,
 *  решает CSS: там предел зависит от ширины окна, а не записан числом. */
export const PANEL_WIDTH_MAX = 64
/** Шаг клавиатуры. */
export const PANEL_WIDTH_STEP = 2

const KEY = 'panel-side-width'

export function clampPanelWidth(rem: number): number {
  if (!Number.isFinite(rem)) return PANEL_WIDTH_DEFAULT
  return Math.min(PANEL_WIDTH_MAX, Math.max(PANEL_WIDTH_MIN, rem))
}

/** Записанное вне пределов возвращается в пределы: пределы могли
 *  смениться после того, как ширину записали. */
export function readPanelWidth(): number | null {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw === null) return null
    const rem = Number(raw)
    return Number.isFinite(rem) ? clampPanelWidth(rem) : null
  } catch {
    return null
  }
}

/** `null` — вернуть исходную: запись убирается, а не пишется умолчание. */
export function writePanelWidth(rem: number | null): void {
  try {
    if (rem === null) localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, String(clampPanelWidth(rem)))
  } catch {
    // Переполненное или закрытое хранилище: ширина переживёт сессию,
    // но не перезагрузку.
  }
}

export function usePanelWidth(): [number | null, (rem: number | null) => void] {
  const [width, setWidth] = useState(readPanelWidth)
  const change = useCallback((rem: number | null) => {
    writePanelWidth(rem)
    setWidth(rem === null ? null : clampPanelWidth(rem))
  }, [])
  return [width, change]
}
