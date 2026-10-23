import { useEffect, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { environmentLabel, setEnvironmentLabel } from '../../shared/lib/environment.ts'

/**
 * Полоса с меткой среды (ROADMAP 36.3) — на каждом экране, включая
 * вход. Без настройки на сервере ничего не рисует: установке с одной
 * средой лишнего видеть незачем.
 *
 * Тон нейтральный, один на все среды: какая из них «опасная», знает
 * только тот, кто ставит, а вторая настройка ради цвета — лишняя.
 */
export function EnvironmentBar() {
  const [label, setLabel] = useState(environmentLabel())
  useEffect(() => {
    api
      .authMethods()
      .then((m) => {
        // `?.` не для красоты: при выкладке старый сервер поля не знает.
        setEnvironmentLabel(m.environment?.enabled ? (m.environment.label ?? '') : '')
        setLabel(environmentLabel())
      })
      // Отказ — вход сам скажет о недоступном сервере; полосе молчать.
      .catch(() => undefined)
  }, [])
  if (!label) return null
  return <div className="stand-bar stand-bar--env">{t.ui.environment(label)}</div>
}
