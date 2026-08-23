import { useCallback, useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { plural } from '../lib/plural.ts'

/**
 * Поле формы: подпись, подсказка, отказ.
 *
 * Компонент назван в наборе давно, а в коде его не было — и каждая
 * форма собирала эту связку заново. Итог переписи: тринадцать полей
 * жили одним `placeholder` без имени вовсе (диктор читает такое поле
 * как «поле ввода», а зрячий теряет подпись с первым же набранным
 * символом), а отказ во всех формах до одной стоял общей плашкой
 * внизу — при том, что правило «ошибка поля живёт у поля» записано
 * в правилах компонентов. Правило не нарушали от небрежности:
 * не нарушить его было дороже, чем нарушить.
 *
 * Что связывается здесь, а не в каждой форме:
 *
 * - подпись `label` — своим `for`, а не оборачиванием: обёртка ломается,
 *   как только у поля появляется сосед (кнопка «показать пароль»);
 * - подсказка и отказ — одним `aria-describedby`: диктор читает их
 *   следом за подписью, а не молчит о них;
 * - `aria-invalid` — только когда отказ есть на самом деле.
 *
 * Носитель отказа — `aria-describedby`, а не `aria-errormessage`:
 * второй читают не все дикторы, а указать на один и тот же узел обоими
 * значит получить у остальных двойное чтение.
 */
export function Field({
  label,
  hint,
  error = null,
  onFix,
  hiddenLabel = false,
  describedBy,
  className,
  children,
}: {
  label: string
  /** Требование, сказанное до отказа, а не после. */
  hint?: ReactNode
  /**
   * Подсказка, живущая вне поля.
   *
   * Бывает, что правило относится к форме, а не к полю: у заведения
   * доски оно объясняет разом и ключ, и что будет, если оставить его
   * пустым. Такую строку нельзя ставить внутрь поля — в ряду она
   * растягивает свою колонку и ломает ряд (видно на снимке, не в коде);
   * но связать её с полем всё равно нужно, иначе диктор её не прочитает.
   */
  describedBy?: string
  /**
   * Подпись только для диктора.
   *
   * Одно поле в ряду с кнопкой, которая называет действие целиком
   * («Завести доску», «Завести подписку»), обходится без подписи
   * на экране: она встала бы отдельной строкой ради слова, которое
   * уже написано рядом. Но имя у поля должно быть всё равно — и связка
   * с отказом тоже, иначе отказ снова уедет общей плашкой вниз.
   */
  hiddenLabel?: boolean
  error?: string | null
  /** Правка отвергнутого поля стирает отказ: см. `useFormErrors`. */
  onFix?: () => void
  className?: string
  children: (bind: {
    id: string
    'aria-describedby': string | undefined
    'aria-invalid': true | undefined
  }) => ReactNode
}) {
  const id = useId()
  const hintId = `${id}-hint`
  const errorId = `${id}-error`
  const described = [describedBy ?? null, hint ? hintId : null, error ? errorId : null]
    .filter(Boolean)
    .join(' ')

  return (
    <div
      className={['form-field', className].filter(Boolean).join(' ')}
      // Правка стирает отказ, и слушается это на обёртке: `input`
      // всплывает, а значит поле остаётся обычным полем — ему
      // не приходится знать про форму, в которой оно стоит.
      onInput={error && onFix ? onFix : undefined}
    >
      <label htmlFor={id} className={hiddenLabel ? 'sr-only' : undefined}>
        {label}
      </label>
      {children({
        id,
        'aria-describedby': described || undefined,
        'aria-invalid': error ? true : undefined,
      })}
      {hint && (
        <p className="form-hint" id={hintId}>
          {hint}
        </p>
      )}
      {/* Узел отказа стоит всегда, даже пустым: пустой абзац без строк
          не занимает высоты, поэтому раскладка от него не прыгает,
          а `aria-describedby` не приходится пересобирать в тот же миг,
          когда фокус уезжает на это поле. */}
      <p className="form-error" id={errorId}>
        {error}
      </p>
    </div>
  )
}

/**
 * Отказ, относящийся ко всей форме, а не к полю.
 *
 * «Неверная почта или пароль» — про оба поля сразу и намеренно: сказать,
 * какое из двух не подошло, значит рассказать, существует ли такая
 * почта. Такому отказу места у поля нет.
 *
 * Живой регион смонтирован всегда и пустым: диктор объявляет изменение
 * существующей области, а появление узла с текстом пропускает. Это уже
 * знали на экране входа — и знание жило там одно.
 */
export function FormError({ children }: { children?: ReactNode }) {
  return (
    <p className="form-error form-error--summary" role="alert">
      {children}
    </p>
  )
}

export type FieldErrors = Record<string, string>

/**
 * Отказы формы: чьи они, когда появляются и куда уходит фокус.
 *
 * Момент проверки — правило, а не вкус. До первой отправки форма
 * молчит: ругаться на поле, которое человек ещё набирает, — значит
 * ругаться на каждый первый символ. На отправке проверяется всё,
 * фокус уходит на первое отвергнутое поле. Дальше — прощение раннее,
 * наказание позднее: правка стирает отказ сразу, а новый может
 * появиться только на следующей отправке.
 *
 * Проверки на уходе из поля здесь нет намеренно. Уход из поля не значит
 * «закончил»: человек уходит в подсказку, в соседнее поле и обратно,
 * и на половине этих уходов поле законно пустое.
 */
export function useFormErrors() {
  const [errors, setErrors] = useState<FieldErrors>({})
  const [formError, setFormError] = useState<string | null>(null)
  const form = useRef<HTMLFormElement>(null)
  // Фокус переносится после отрисовки: до неё `aria-invalid` ещё не
  // проставлен, и искать по нему нечего.
  const [moveFocus, setMoveFocus] = useState(0)

  useEffect(() => {
    if (!moveFocus) return
    const first = form.current?.querySelector<HTMLElement>('[aria-invalid="true"]')
    first?.focus()
  }, [moveFocus])

  const forget = useCallback((name: string) => {
    setErrors((was) => {
      if (!(name in was)) return was
      const now = { ...was }
      delete now[name]
      return now
    })
  }, [])

  const report = useCallback((found: FieldErrors, summary: string | null = null) => {
    setErrors(found)
    setFormError(summary)
    if (Object.keys(found).length > 0) setMoveFocus((n) => n + 1)
  }, [])

  return {
    /** Ставится на `<form>`: по нему ищется поле, которому отдать фокус. */
    ref: form,
    formError,
    /** Раскладывается в `Field`: `<Field label="Почта" {...form.field('email')}>`. */
    field: (name: string) => ({
      error: errors[name] ?? null,
      onFix: () => forget(name),
    }),
    report,
    /** Отказ, у которого нет своего поля. */
    reportForm: useCallback((summary: string | null) => {
      setErrors({})
      setFormError(summary)
    }, []),
    clear: useCallback(() => {
      setErrors({})
      setFormError(null)
    }, []),
    /**
     * Проверка формы средствами платформы, но своими словами.
     *
     * Правила остаются там, где им место, — в разметке поля
     * (`required`, `type`, `minLength`), поэтому диктор знает о них
     * до отправки, а не только из отказа. От платформы берётся
     * `ValidityState`, но не её пузырьки: они не стилизуются, исчезают
     * при переключении вкладки, показываются по одному и переводятся
     * не нами. Отсюда `noValidate` на форме — не отказ от проверки,
     * а отказ от чужого способа о ней рассказывать.
     */
    check: useCallback((element: HTMLFormElement): FieldErrors => {
      const found: FieldErrors = {}
      for (const control of element.elements) {
        if (!isControl(control) || !control.name || control.disabled) continue
        if (control.validity.valid) continue
        found[control.name] = messageFor(control)
      }
      return found
    }, []),
  }
}

const плуралСимвол = (n: number) => plural(n, 'символ', 'символа', 'символов')

type Control = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement

function isControl(node: Element): node is Control {
  return (
    node instanceof HTMLInputElement ||
    node instanceof HTMLTextAreaElement ||
    node instanceof HTMLSelectElement
  )
}

/**
 * Отказ словами, а не кодом состояния.
 *
 * Порядок ветвей — от частого к редкому. «Заполните» без имени поля:
 * сообщение стоит вплотную к полю и читается диктором сразу за его
 * подписью, так что повторять подпись значит сказать её дважды.
 */
function messageFor(control: Control): string {
  const v = control.validity
  if (v.valueMissing) return 'Заполните'
  if (v.typeMismatch) {
    if (control instanceof HTMLInputElement && control.type === 'url') {
      return 'Адрес начинается с http:// или https://'
    }
    return 'Похоже, в адресе опечатка: нужен вид имя@домен'
  }
  // Длина называется числами, а не правилом: правило уже сказано
  // подсказкой под полем, и повторить его слово в слово значит
  // поставить одну и ту же строку дважды — серой и красной. Видно это
  // только на снимке заполненной формы, а не в разметке.
  if (v.tooShort && 'minLength' in control) {
    const надо = control.minLength
    const есть = control.value.length
    return `Сейчас ${есть} ${плуралСимвол(есть)}, нужно ${надо}`
  }
  if (v.tooLong && 'maxLength' in control) {
    const надо = control.maxLength
    const есть = control.value.length
    return `Сейчас ${есть} ${плуралСимвол(есть)}, можно ${надо}`
  }
  if (v.rangeUnderflow && 'min' in control) return `Не меньше ${control.min}`
  if (v.rangeOverflow && 'max' in control) return `Не больше ${control.max}`
  if (v.stepMismatch) return 'Нужно целое число'
  if (v.patternMismatch) return control.title || 'Не подходит по виду'
  return 'Не подходит'
}
