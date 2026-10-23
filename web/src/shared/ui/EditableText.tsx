import { useEffect, useMemo, useState } from 'react'
import { TextLine } from './TextLine.tsx'

/**
 * Текст, который правят на месте.
 *
 * Нажатие превращает надпись в поле — так правят названия колонок
 * и карточек. Отдельная форма редактирования для одной строки означает
 * лишний экран и потерю места на доске.
 *
 * Кнопка, а не div с обработчиком: править умеет и клавиатура,
 * и это должно быть слышно тому, кто читает экран с диктора.
 */
export function EditableText({
  value,
  onSave,
  onCancel,
  className,
  autoFocus,
  label,
  placeholder,
  multiline,
}: {
  value: string
  onSave: (next: string) => void
  onCancel?: () => void
  className?: string
  autoFocus?: boolean
  /** Имя поля для того, кто читает экран с диктора. Надписи рядом
   *  у правки на месте нет — поле стоит там, где только что стоял
   *  текст, и без имени диктор объявляет его просто «поле ввода». */
  label?: string
  /** Подсказка внутри пустого поля: у правки на месте пустота ничего
   *  не объясняет — прежнего текста уже не видно. */
  placeholder?: string
  /** Длинный текст — названия, причины: поле переносит слова и растёт
   *  (ROADMAP 37.3). Короткому — названию колонки — хватает строки. */
  multiline?: boolean
}) {
  const [editing, setEditing] = useState(Boolean(autoFocus))
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])

  const commit = useMemo(
    () => () => {
      const next = draft.trim()
      if (next && next !== value) onSave(next)
      else onCancel?.()
      setEditing(false)
    },
    [draft, value, onSave, onCancel],
  )

  if (!editing)
    return (
      <button className={`inline-edit ${className ?? ''}`} onClick={() => setEditing(true)}>
        {value}
      </button>
    )

  const cancel = () => {
    setDraft(value)
    setEditing(false)
    onCancel?.()
  }

  if (multiline)
    return (
      <TextLine
        autoFocus
        className={className}
        label={label}
        placeholder={placeholder}
        value={draft}
        onChange={setDraft}
        onBlur={commit}
        onEnter={commit}
        onEscape={cancel}
      />
    )

  return (
    <input
      autoFocus
      className={className}
      aria-label={label}
      placeholder={placeholder}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
        if (e.key === 'Escape') cancel()
      }}
    />
  )
}
