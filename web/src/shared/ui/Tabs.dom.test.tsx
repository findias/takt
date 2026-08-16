// Вкладки.
//
// Проверяется то, из-за чего вкладки нельзя собрать из трёх кнопок
// и условия: полоса вкладок обязана вести себя как полоса — таб
// останавливается в ней один раз, а внутри ходят стрелками, — и обязана
// быть связана с содержимым, иначе тот, кто читает экран с диктора,
// слышит три кнопки и не понимает, что они переключают.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { TabPanel, Tabs, useTabIds } from './Tabs.tsx'

const TABS = [
  { id: 'work', label: 'Работа' },
  { id: 'talk', label: 'Обсуждение' },
  { id: 'history', label: 'История' },
]

function Example() {
  const ids = useTabIds()
  const [active, setActive] = useState('work')
  return (
    <>
      <button>Снаружи</button>
      <Tabs base={ids} tabs={TABS} active={active} onSelect={setActive} label="Разделы" />
      <TabPanel base={ids} id={active}>
        <p>Содержимое: {active}</p>
      </TabPanel>
    </>
  )
}

describe('вкладки', () => {
  it('показывают содержимое только выбранной вкладки', async () => {
    const user = userEvent.setup()
    render(<Example />)

    expect(screen.getByText('Содержимое: work')).toBeTruthy()

    await user.click(screen.getByRole('tab', { name: 'История' }))
    expect(screen.getByText('Содержимое: history')).toBeTruthy()
    expect(screen.queryByText('Содержимое: work')).toBeNull()
  })

  // Таб в полосе останавливается один раз: иначе, чтобы дойти
  // от вкладок до содержимого, пришлось бы протабать все вкладки.
  it('в полосе одна остановка таба, внутри ходят стрелками', async () => {
    const user = userEvent.setup()
    render(<Example />)

    const stops = screen
      .getAllByRole('tab')
      .filter((t) => t.getAttribute('tabindex') !== '-1')
    expect(stops.length).toBe(1)
    expect(stops[0].textContent).toBe('Работа')

    await user.tab() // «Снаружи»
    await user.tab() // выбранная вкладка
    expect(document.activeElement?.textContent).toBe('Работа')

    await user.keyboard('{ArrowRight}')
    expect(screen.getByText('Содержимое: talk')).toBeTruthy()
    // Фокус едет за выбором, иначе следующая стрелка пойдёт от прежней
    // вкладки и переключение начнёт перескакивать.
    expect(document.activeElement?.textContent).toBe('Обсуждение')

    await user.keyboard('{End}')
    expect(document.activeElement?.textContent).toBe('История')
    // По кругу: за последней вкладкой снова первая.
    await user.keyboard('{ArrowRight}')
    expect(document.activeElement?.textContent).toBe('Работа')
  })

  it('вкладка связана со своим содержимым', () => {
    render(<Example />)

    const tab = screen.getByRole('tab', { name: 'Работа' })
    const panel = screen.getByRole('tabpanel')
    expect(tab.getAttribute('aria-selected')).toBe('true')
    expect(tab.getAttribute('aria-controls')).toBe(panel.id)
    expect(panel.getAttribute('aria-labelledby')).toBe(tab.id)
  })
})
