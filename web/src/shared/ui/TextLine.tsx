import { useRef } from 'react'
import { useAutoGrow } from '../lib/useAutoGrow.ts'

/**
 * Строка текста, которая переносится (ROADMAP 37.3).
 *
 * Название карточки и причина блокировки — одна мысль, а не абзац:
 * перевод строки внутри них не нужен, и Enter сохраняет, как в обычном
 * поле. Но текст бывает длинным, и в однострочном поле он уезжал за край — видно
 * было начало фразы, а конец набирали вслепую. Здесь то же поведение
 * в многострочном поле: слова переносятся, высота растёт по тексту.
 * Вставленный текст с переводами строк склеивается пробелами — сохранять
 * их некуда, на карточке название идёт одной строкой с переносами.
 */
export function TextLine({
  value,
  onChange,
  onEnter,
  onEscape,
  onBlur,
  label,
  placeholder,
  className,
  autoFocus,
  maxLength,
}: {
  value: string
  onChange: (next: string) => void
  onEnter?: () => void
  onEscape?: () => void
  onBlur?: () => void
  label?: string
  placeholder?: string
  className?: string
  autoFocus?: boolean
  maxLength?: number
}) {
  const ref = useRef<HTMLTextAreaElement>(null)
  useAutoGrow(ref, value)
  return (
    <textarea
      ref={ref}
      rows={1}
      autoFocus={autoFocus}
      className={`text-line${className ? ` ${className}` : ''}`}
      aria-label={label}
      placeholder={placeholder}
      maxLength={maxLength}
      value={value}
      onChange={(e) => onChange(e.target.value.replace(/[\r\n]+/g, ' '))}
      onBlur={onBlur}
      onKeyDown={(e) => {
        // Набор иероглифов подтверждается Enter-ом — он не должен
        // сохранять недописанное.
        if (e.nativeEvent.isComposing) return
        if (e.key === 'Enter') {
          e.preventDefault()
          onEnter?.()
        }
        if (e.key === 'Escape') onEscape?.()
      }}
    />
  )
}
