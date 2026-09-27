// Знак Takt — метроном из карточек. Проверяется то, что ломается
// незаметно: ступень по размеру (щель уже пикселя залипает, и знак
// читается пятном), щели и вырезы — маска, а не белая заливка (знак
// обязан быть верен на тёмной теме), и маски двух знаков на одной
// странице не делят один id — иначе второй знак рисуется вырезом первого.

import { render } from '@testing-library/react'
import { expect, it } from 'vitest'
import { Mark } from './Mark.tsx'

it('чёрточки заголовков от 64 пикселей, орбита от 32, до 32 — только метроном', () => {
  const { container } = render(
    <>
      <Mark size={64} />
      <Mark size={40} />
      <Mark size={24} />
    </>,
  )
  const [big, mid, small] = container.querySelectorAll('svg')
  const titles = (m: Element) => m.querySelectorAll('mask rect[height="0.9"]').length
  expect(titles(big)).toBe(13)
  expect(titles(mid)).toBe(0)
  expect(mid.querySelector('path[d^="M7.39"]')).not.toBeNull()
  expect(small.querySelector('path[d^="M7.39"]')).toBeNull()
  expect(small.querySelectorAll('mask')).toHaveLength(1)
})

it('щели — маска, у каждого знака своя, и диктору знак не читается', () => {
  const { container } = render(
    <>
      <Mark size={40} />
      <Mark size={40} />
    </>,
  )
  const marks = [...container.querySelectorAll('svg')]
  const ids = marks.map((m) => m.querySelector('mask')!.id)
  expect(new Set(ids).size).toBe(2)
  for (const [i, m] of marks.entries()) {
    expect(m.getAttribute('aria-hidden')).toBe('true')
    expect(m.querySelector('.takt-mark-body')!.getAttribute('mask')).toBe(`url(#${ids[i]})`)
    // Белого вне маски нет: белое внутри маски — это «видно», не цвет.
    const painted = [...m.querySelectorAll('[fill], [stroke]')].filter((el) => !el.closest('mask'))
    for (const el of painted) {
      expect([el.getAttribute('fill'), el.getAttribute('stroke')]).not.toContain('#fff')
    }
  }
})

it('ожидание качает маятник вместе с его вырезом', () => {
  const { container } = render(<Mark size={40} swing />)
  expect(container.querySelector('svg')!.classList.contains('takt-mark--swing')).toBe(true)
  const pendulum = [...container.querySelectorAll('.takt-mark-pendulum')]
  expect(pendulum.map((g) => Boolean(g.closest('mask')))).toEqual([true, false])
})
