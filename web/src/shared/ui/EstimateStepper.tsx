/**
 * Шаговый ввод оценки.
 *
 * Оценку меняют на единицу почти всегда: «не два, а три». Числовое
 * поле требует на это выделить содержимое и напечатать другое число,
 * то есть три действия вместо одного. Кнопки рядом со значением
 * убирают лишние два и не отнимают клавиатуру — значение остаётся
 * полем ввода, в него можно напечатать 13 сразу.
 *
 * Пустая оценка — это `null`, а не ноль. Ноль означает «работы здесь
 * нет», null — «не оценивали»; в сумме по человеку и в мере разбиения
 * это разные вещи, и подменять одно другим нельзя. Поэтому «−»
 * с единицы уводит в null, а не в 0.
 *
 * Единица подписана словом со склонением, и правило склонения живёт
 * в модели карточки — там же, где оно нужно подписи прогресса. Второй
 * таблицы форм в интерфейсе быть не должно: однажды они разойдутся,
 * и где-то будет написано «2 очков».
 */

import { useEffect, useRef, useState } from 'react'
import { MinusIcon, PlusIcon } from './icons.tsx'
import { unitLabel } from '../../entities/card/model.ts'
import type { EstimateUnit } from '../api/index.ts'

export function EstimateStepper({
  value,
  unit,
  onChange,
  id,
}: {
  value: number | null
  unit: EstimateUnit
  /** null — снять оценку. */
  onChange: (next: number | null) => void
  id?: string
}) {
  // Нажатия идут очередью, а операция уходит одна.
  //
  // «Пять» набирают пятью нажатиями подряд, и отправлять пять правок
  // значит гонку с самим собой: ответ на первую приходит, когда
  // на экране уже третья, и возвращает её назад. Так и вышло —
  // три нажатия давали двойку. Значение поэтому живёт здесь, пока
  // человек жмёт, и уходит наружу, когда он остановился.
  const [draft, setDraft] = useState(value)
  const timer = useRef<number | undefined>(undefined)

  // Чужое изменение (или ответ сервера) подхватывается, пока своей
  // неотправленной правки нет.
  useEffect(() => {
    if (timer.current === undefined) setDraft(value)
  }, [value])

  const send = (next: number | null) => {
    setDraft(next)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      timer.current = undefined
      onChange(next)
    }, 400)
  }

  // Панель закрыли, не дождавшись паузы, — правку всё равно отправляем:
  // потерять её было бы хуже, чем отправить лишнюю.
  useEffect(() => {
    return () => {
      if (timer.current === undefined) return
      window.clearTimeout(timer.current)
      onChangeRef.current(draftRef.current)
    }
  }, [])

  const onChangeRef = useRef(onChange)
  const draftRef = useRef(draft)
  onChangeRef.current = onChange
  draftRef.current = draft

  const step = (delta: number) => {
    const next = (draft ?? 0) + delta
    send(next <= 0 ? null : next)
  }

  return (
    <span className="stepper">
      <button
        type="button"
        className="btn btn--quiet btn--icon"
        aria-label="Уменьшить оценку"
        disabled={draft === null}
        onClick={() => step(-1)}
      >
        <MinusIcon />
      </button>
      <input
        id={id}
        type="number"
        inputMode="decimal"
        min={0}
        step={1}
        // Пустая строка, а не 0: поле обязано отличать «не оценивали»
        // от «оценили нулём».
        value={draft === null ? '' : draft}
        placeholder="—"
        aria-label="Оценка"
        onChange={(e) => {
          const raw = e.target.value.trim()
          if (raw === '') return send(null)
          const parsed = Number(raw.replace(',', '.'))
          if (Number.isFinite(parsed) && parsed >= 0) send(parsed === 0 ? null : parsed)
        }}
      />
      <button
        type="button"
        className="btn btn--quiet btn--icon"
        aria-label="Увеличить оценку"
        onClick={() => step(1)}
      >
        <PlusIcon />
      </button>
      {/* Единица — подпись, а не элемент управления: меняется она
          в настройках доски, одна на всю доску. */}
      <span className="stepper-unit">{draft === null ? 'без оценки' : `${draft} ${unitLabel(draft, unit)}`}</span>
    </span>
  )
}
