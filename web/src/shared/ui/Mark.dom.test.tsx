// Знак Takt (система знака — холст «Takt — логотип», 05.2). Проверяется
// то, что ломается незаметно: версия по размеру (у малой нет основания,
// иначе вырез залипает в пиксельной сетке), вырез — маска, а не белая
// заливка (знак обязан быть верен на тёмной теме), и маски двух знаков
// на одной странице не делят один id — иначе второй знак рисуется
// вырезом первого.

import { render } from '@testing-library/react'
import { expect, it } from 'vitest'
import { Mark } from './Mark.tsx'

it('полный знак с основанием от 32 пикселей, малый — без', () => {
  const { container } = render(
    <>
      <Mark size={56} />
      <Mark size={24} />
    </>,
  )
  const [full, small] = container.querySelectorAll('svg')
  expect(full.querySelector('path[d="M14.5 36.5 H33.5"]')).not.toBeNull()
  expect(small.querySelector('path[d="M14.5 36.5 H33.5"]')).toBeNull()
  expect(small.querySelector('circle')?.getAttribute('r')).toBe('5.5')
})

it('вырез — маска, у каждого знака своя, и диктору знак не читается', () => {
  const { container } = render(
    <>
      <Mark size={24} />
      <Mark size={24} />
    </>,
  )
  const marks = [...container.querySelectorAll('svg')]
  const ids = marks.map((m) => m.querySelector('mask')!.id)
  expect(new Set(ids).size).toBe(2)
  for (const [i, m] of marks.entries()) {
    expect(m.getAttribute('aria-hidden')).toBe('true')
    expect(m.querySelector('.takt-mark-body')!.getAttribute('mask')).toBe(`url(#${ids[i]})`)
    // Белого в знаке нет: белое внутри маски — это «видно», не цвет.
    const painted = [...m.querySelectorAll('[fill], [stroke]')].filter((el) => !el.closest('mask'))
    expect(painted).toEqual([])
  }
})

it('ожидание качает маятник', () => {
  const { container } = render(<Mark size={40} swing />)
  expect(container.querySelector('svg')!.classList.contains('takt-mark--swing')).toBe(true)
  expect(container.querySelector('.takt-mark-pendulum')).not.toBeNull()
})
