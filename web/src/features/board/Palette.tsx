import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'

/**
 * Командная палитра.
 *
 * Одно окно, в котором ищут и карточку, и действие. Так устроено
 * у Linear и в GitHub, и причина не в моде: на доске в триста карточек
 * найти нужную мышью — это прокрутка и глаза, а тут четыре буквы
 * названия. Действия лежат в том же списке, потому что человек, набрав
 * «мет», одинаково может иметь в виду и карточку со словом «метка»,
 * и команду «сгруппировать по меткам».
 *
 * Открывается на Ctrl+K (и Cmd+K), закрывается по Escape. Это диалог,
 * поэтому у него `aria-modal`, ловушка фокуса от браузера и возврат
 * фокуса — всё то же, что у обычного диалога; отсюда и `<dialog>`.
 */
export type Command = {
  id: string
  title: string
  /** Короткая приписка справа: где это или что произойдёт. */
  hint?: string
  icon?: ReactNode
  run: () => void
}

export function Palette({
  open,
  commands,
  onClose,
}: {
  open: boolean
  commands: Command[]
  onClose: () => void
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const [text, setText] = useState('')
  const [active, setActive] = useState(0)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) {
      setText('')
      setActive(0)
      dialog.showModal()
    }
    if (!open && dialog.open) dialog.close()
  }, [open])

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    const onCloseEvent = () => onClose()
    dialog.addEventListener('close', onCloseEvent)
    return () => dialog.removeEventListener('close', onCloseEvent)
  }, [onClose])

  // Отбор подстрокой без учёта регистра — тот же поиск, что и на доске.
  // Ранжирования нет намеренно: список короткий, а выдумывать вес
  // совпадения значит объяснять человеку, почему «его» строка не первая.
  const found = useMemo(() => {
    const needle = text.trim().toLowerCase()
    const list = needle
      ? commands.filter((c) => `${c.title} ${c.hint ?? ''}`.toLowerCase().includes(needle))
      : commands
    return list.slice(0, 20)
  }, [commands, text])

  useEffect(() => setActive(0), [text])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => Math.min(i + 1, found.length - 1))
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      const command = found[active]
      if (command) {
        onClose()
        command.run()
      }
    }
  }

  return (
    <dialog className="palette" ref={ref} aria-label="Поиск и команды" onKeyDown={onKeyDown}>
      <input
        autoFocus
        className="palette-input"
        value={text}
        placeholder="Карточка или команда"
        aria-label="Поиск и команды"
        // Список — часть поля: так скринридер объявляет число найденного
        // и текущий вариант, не требуя уходить из поля ввода.
        role="combobox"
        aria-expanded
        aria-controls="palette-list"
        aria-activedescendant={found[active] ? `palette-${found[active].id}` : undefined}
        onChange={(e) => setText(e.target.value)}
      />

      <ul className="palette-list" id="palette-list" role="listbox" aria-label="Найденное">
        {found.map((command, i) => (
          <li key={command.id}>
            <button
              id={`palette-${command.id}`}
              role="option"
              aria-selected={i === active}
              className={i === active ? 'palette-item palette-item--active' : 'palette-item'}
              // Мышь ведёт выделение за собой: иначе клавиатура
              // и указатель показывают разное, и непонятно, что случится
              // по Enter.
              onMouseEnter={() => setActive(i)}
              onClick={() => {
                onClose()
                command.run()
              }}
            >
              {command.icon}
              <span className="palette-title">{command.title}</span>
              {command.hint && <span className="muted small">{command.hint}</span>}
            </button>
          </li>
        ))}
        {found.length === 0 && (
          <li className="muted small palette-empty">
            Ничего не нашлось. Палитра ищет по названиям карточек и команд.
          </li>
        )}
      </ul>
    </dialog>
  )
}

/** Подпись сокращения. На Mac ждут ⌘, на остальных — Ctrl; показать
 *  не то значит показать неправду. */
export function paletteHint(): string {
  const mac = typeof navigator !== 'undefined' && /Mac|iP(hone|ad)/.test(navigator.platform)
  return mac ? '⌘K' : 'Ctrl K'
}

/** Ctrl+K и Cmd+K. Отдельно от компонента: сокращение живёт на экране,
 *  а не в окне, которого ещё нет. */
export function usePaletteHotkey(onOpen: () => void): void {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 'k' || e.key === 'л')) {
        e.preventDefault()
        onOpen()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onOpen])
}
