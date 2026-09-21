import { Component } from 'react'
import type { ErrorInfo, ReactNode } from 'react'
import { t } from '../i18n/index.ts'

/**
 * Что видно, когда отрисовка упала.
 *
 * До этой границы любая ошибка в отрисовке давала белый экран: React
 * снимает всё дерево, если ошибку некому поймать. Это худший из отказов
 * — человеку нечего прочитать, некуда нажать и нечего прислать тому,
 * кто чинит. Случилось это на разошедшихся клиенте и сервере: снимок
 * приехал без поля, которого клиент ждал, и страница исчезла целиком.
 *
 * Граница не «чинит» ошибку и не притворяется, что ничего не было. Она
 * делает три вещи, и каждая нужна: называет, что сломалось; даёт кнопку,
 * которая возвращает в рабочее состояние; показывает текст ошибки,
 * который можно выделить и переслать. Последнее важнее, чем кажется:
 * «что-то пошло не так» без подробностей превращает починку в допрос.
 *
 * Классом, а не хуком: ловить ошибки отрисовки в React умеет только
 * класс, и другого способа нет до сих пор.
 */
type Props = { children: ReactNode }
type State = { error: Error | null }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // В консоль — целиком, вместе с деревом компонентов: на экране
    // столько показывать некому, а в консоли это первое, что открывают.
    console.error(t.ui.crashLog, error, info.componentStack)
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children

    return (
      <div className="centered">
        <div className="panel stack">
          <h1>{t.ui.crashTitle}</h1>
          <p>{t.ui.crashBody}</p>
          <p className="muted small">{t.ui.crashReport}</p>
          {/* Текст ошибки выделяется и копируется. Моноширинный,
              с переносом: сообщения бывают длинными, а обрезанное
              сообщение бесполезно ровно той частью, которую обрезали. */}
          <pre className="error-details">{error.message || String(error)}</pre>
          <div className="row">
            <button onClick={() => window.location.reload()}>{t.ui.reload}</button>
            {/* Возврат к списку досок — второй выход: если ломается
                один экран, остальные обычно живы. */}
            <button
              className="link"
              onClick={() => {
                window.location.href = '/'
              }}
            >
              {t.ui.toAllBoards}
            </button>
          </div>
        </div>
      </div>
    )
  }
}
