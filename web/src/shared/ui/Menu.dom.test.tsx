// Меню действий.
//
// Проверяется поведение, описанное в WAI-ARIA APG, — то самое, которое
// люди уже знают по любому другому меню: стрелки, Escape, возврат
// фокуса. Проверять это глазами нельзя: на снимке экрана открытое меню
// выглядит одинаково и с клавиатурой, и без неё.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { Menu } from './Menu.tsx'

function show(onSelect = vi.fn()) {
  render(
    <>
      <button>Снаружи</button>
      <Menu
        label="Действия карточки «Смета»"
        items={[
          { label: 'Открыть', onSelect },
          { label: 'Переименовать', onSelect: vi.fn() },
          { label: 'Убрать в архив', danger: true, onSelect: vi.fn() },
        ]}
      >
        …
      </Menu>
    </>,
  )
  return { onSelect, trigger: screen.getByRole('button', { name: /Действия карточки/ }) }
}

describe('меню', () => {
  it('до нажатия закрыто и сообщает об этом', () => {
    const { trigger } = show()
    expect(screen.queryByRole('menu')).toBeNull()
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(trigger.getAttribute('aria-haspopup')).toBe('menu')
  })

  it('открывается и показывает все действия целиком', async () => {
    const user = userEvent.setup()
    const { trigger } = show()
    await user.click(trigger)

    const items = screen.getAllByRole('menuitem')
    expect(items.map((i) => i.textContent)).toEqual([
      'Открыть',
      'Переименовать',
      'Убрать в архив',
    ])
    // Ради этого меню и заведено: раньше подписи обрезались до «Откры».
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
  })

  it('ходит стрелками и выбирает с клавиатуры', async () => {
    const user = userEvent.setup()
    const { onSelect, trigger } = show()

    await user.click(trigger)
    expect(document.activeElement?.textContent).toBe('Открыть')

    await user.keyboard('{ArrowDown}')
    expect(document.activeElement?.textContent).toBe('Переименовать')

    await user.keyboard('{End}')
    expect(document.activeElement?.textContent).toBe('Убрать в архив')

    // По кругу: за последним пунктом идёт первый.
    await user.keyboard('{ArrowDown}')
    expect(document.activeElement?.textContent).toBe('Открыть')

    await user.keyboard('{Enter}')
    expect(onSelect).toHaveBeenCalledTimes(1)
  })

  // Правка по самому полю: пункт-переключатель отвечает не «сделать»,
  // а «включено или нет», и это должно быть слышно, а не только видно
  // по галочке.
  it('пункт-переключатель называет своё состояние', async () => {
    const user = userEvent.setup()
    render(
      <Menu
        label="Исполнители"
        items={[
          { label: 'Анна', checked: true, onSelect: vi.fn() },
          { label: 'Борис', checked: false, onSelect: vi.fn() },
        ]}
      >
        …
      </Menu>,
    )
    await user.click(screen.getByRole('button', { name: 'Исполнители' }))

    const items = screen.getAllByRole('menuitemcheckbox')
    expect(items.map((i) => i.getAttribute('aria-checked'))).toEqual(['true', 'false'])

    // Стрелки ходят по ним так же: роль другая, поведение то же.
    expect(document.activeElement?.textContent).toBe('Анна')
    await user.keyboard('{ArrowDown}')
    expect(document.activeElement?.textContent).toBe('Борис')
  })

  it('закрывается по Escape и возвращает фокус кнопке', async () => {
    const user = userEvent.setup()
    const { trigger } = show()

    await user.click(trigger)
    await user.keyboard('{Escape}')

    expect(screen.queryByRole('menu')).toBeNull()
    // Иначе фокус улетает в начало страницы, и человек теряет место.
    expect(document.activeElement).toBe(trigger)
  })

  it('закрывается щелчком вне себя', async () => {
    const user = userEvent.setup()
    const { trigger } = show()

    await user.click(trigger)
    await user.click(screen.getByRole('button', { name: 'Снаружи' }))

    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('выбор действия закрывает меню', async () => {
    const user = userEvent.setup()
    const { onSelect, trigger } = show()

    await user.click(trigger)
    await user.click(screen.getByRole('menuitem', { name: 'Открыть' }))

    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('menu')).toBeNull()
  })
})
