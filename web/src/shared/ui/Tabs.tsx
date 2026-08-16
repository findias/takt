import { useId, useRef } from 'react'

/**
 * Вкладки.
 *
 * Нужны там, где на одном экране лежат разные по смыслу вещи, и одна
 * из них растёт сама. Панель карточки была свитком в восемь разделов,
 * последним из которых шла история переходов: она длиннее всего
 * остального вместе и растёт с каждым движением карточки, поэтому
 * вытесняла вниз то, ради чего карточку открывали.
 *
 * Переключение — сразу по выбору, без Enter. Правило WAI-ARIA требует
 * ручной активации только там, где переключение дорого; здесь оно стоит
 * одного запроса, и лишний шаг был бы платой ни за что.
 */

export type TabDef = {
  id: string
  label: string
}

/**
 * Общий корень идентификаторов для полосы вкладок и её панелей.
 *
 * Заводится в том же компоненте, где живут и Tabs, и TabPanel: иначе
 * useId выдаст им разные корни, aria-controls повиснет в никуда,
 * и скринридер перестанет связывать вкладку с содержимым.
 */
export function useTabIds(): string {
  return useId()
}

export function Tabs({
  base,
  tabs,
  active,
  onSelect,
  label,
}: {
  base: string
  tabs: TabDef[]
  active: string
  onSelect: (id: string) => void
  /** Чем эти вкладки управляют: «Разделы карточки», например. */
  label: string
}) {
  const ref = useRef<HTMLDivElement>(null)

  // Стрелки ходят по вкладкам, Home и End прыгают к краям — этого
  // требует WAI-ARIA от роли tablist, и без этого с клавиатуры
  // до дальней вкладки пришлось бы идти табом через всё содержимое.
  const onKeyDown = (e: React.KeyboardEvent) => {
    const step: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1 }
    const at = tabs.findIndex((t) => t.id === active)
    let next = -1
    if (e.key in step) next = (at + step[e.key] + tabs.length) % tabs.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = tabs.length - 1
    if (next < 0) return

    e.preventDefault()
    onSelect(tabs[next].id)
    // Фокус едет за выбором: иначе следующая стрелка пойдёт от прежней
    // вкладки, и переключение начнёт перескакивать.
    ref.current?.querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus()
  }

  return (
    <div className="tabs" role="tablist" aria-label={label} ref={ref} onKeyDown={onKeyDown}>
      {tabs.map((t) => {
        const selected = t.id === active
        return (
          <button
            key={t.id}
            id={tabId(base, t.id)}
            role="tab"
            type="button"
            aria-selected={selected}
            aria-controls={panelId(base, t.id)}
            // В полосе вкладок таб останавливается один раз, а не на
            // каждой вкладке: внутри полосы ходят стрелками.
            tabIndex={selected ? 0 : -1}
            className={`tab${selected ? ' tab--active' : ''}`}
            onClick={() => onSelect(t.id)}
          >
            {t.label}
          </button>
        )
      })}
    </div>
  )
}

/**
 * Содержимое выбранной вкладки. Отдельным компонентом, потому что связь
 * панели с вкладкой держится на совпадении идентификаторов, а совпадение,
 * собранное в двух местах руками, рано или поздно разъезжается.
 */
export function TabPanel({
  base,
  id,
  children,
}: {
  base: string
  id: string
  children: React.ReactNode
}) {
  return (
    <div
      className="panel-body"
      role="tabpanel"
      id={panelId(base, id)}
      aria-labelledby={tabId(base, id)}
      // Прокручиваемая область обязана получать фокус с клавиатуры:
      // иначе до содержимого длинной вкладки не добраться колесом
      // с клавиатуры вовсе.
      tabIndex={0}
    >
      {children}
    </div>
  )
}

const tabId = (base: string, id: string) => `${base}tab-${id}`
const panelId = (base: string, id: string) => `${base}panel-${id}`
