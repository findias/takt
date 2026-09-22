import { useEffect, useId, useRef, useState } from 'react'
import { topLayer, useAnchored } from './anchored.ts'
import { helpUrl } from '../lib/help.ts'
import type { HelpTopic } from '../lib/help.ts'
import { t } from '../i18n/index.ts'
import type { Catalog } from '../i18n/index.ts'

export type HintTopic = keyof Catalog['hint'] & HelpTopic

/**
 * «?» у понятия (ROADMAP 30.4) — раскрывашка, а не всплывающая
 * подсказка: по нажатию две-три фразы и «Подробнее в справке».
 *
 * Наведение не годится: на телефоне его нет, а диктор его не читает.
 * Поэтому кнопка, и поэтому у неё имя целиком — «Что такое «Лимит
 * колонки»?»: знак вопроса без слов диктор прочтёт как «вопрос».
 *
 * Стоит у понятий, а не у каждой кнопки: «?» везде становится шумом
 * и перестаёт читаться. Текст объясняет, что это и зачем, — шаги живут
 * в справке, куда ведёт ссылка.
 *
 * Текст объявляется живым регионом, который смонтирован всегда: узел,
 * появившийся сразу с текстом, диктор пропускает. Само пояснение —
 * в верхнем слое, как меню: колонка карточек прокручивается сама
 * и обрезала бы его.
 */
export function Hint({ topic }: { topic: HintTopic }) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLSpanElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const bubbleRef = useRef<HTMLDivElement>(null)
  const id = useId()
  const box = useAnchored(open, buttonRef, bubbleRef, 'left', 'down')
  const hint = t.hint[topic]

  // Щелчок вне закрывает, как у меню; фокус при этом не трогаем —
  // человек уже щёлкнул туда, куда хотел.
  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    return () => document.removeEventListener('pointerdown', onPointer)
  }, [open])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== 'Escape' || !open) return
    // Escape закрывает пояснение, а не панель или диалог, в котором
    // оно стоит: закрыть надо то, что открыли последним.
    e.stopPropagation()
    setOpen(false)
    buttonRef.current?.focus()
  }

  return (
    <span className="hint" ref={rootRef} onKeyDown={onKeyDown}>
      <button
        type="button"
        className="hint-button"
        ref={buttonRef}
        aria-label={t.hints.whatIs(hint.term)}
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((v) => !v)}
      >
        ?
      </button>
      <span className="sr-only" role="status">
        {open ? hint.text : ''}
      </span>
      {open && (
        <div
          className="hint-bubble"
          id={id}
          ref={bubbleRef}
          popover={topLayer ? 'manual' : undefined}
          style={box ? { top: box.top, left: box.left } : { opacity: 0 }}
        >
          <strong className="hint-term">{hint.term}</strong>
          <p>{hint.text}</p>
          <a href={helpUrl(topic)} target="_blank" rel="noopener">
            {t.hints.more}
          </a>
        </div>
      )}
    </span>
  )
}
