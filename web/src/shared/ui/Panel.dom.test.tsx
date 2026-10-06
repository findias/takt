// Панель в трёх режимах.
//
// Проверяется ровно то, из-за чего режимы вообще разведены: боковая
// панель оставляет доску рабочей, а центральная и полноэкранная её
// перекрывают — значит, это диалог со всеми его обязанностями. Сделать
// вид, что разница только в ширине, — обычный способ получить панель,
// из которой не выбраться с клавиатуры, и заметить это глазами нельзя.

import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { Panel, usePanelMode } from './Panel.tsx'
import type { PanelMode } from './Panel.tsx'
import { act, renderHook } from '@testing-library/react'

function show(mode: PanelMode, onClose = () => {}) {
  return render(
    <>
      <button>Снаружи</button>
      <Panel mode={mode} onMode={() => {}} title="Карточка" label="Карточка" onClose={onClose}>
        <button>Внутри раз</button>
        <button>Внутри два</button>
      </Panel>
    </>,
  )
}

beforeEach(() => {
  localStorage.clear()
})

describe('боковая панель', () => {
  // Доска рядом с ней остаётся рабочей: её листают и в ней перетаскивают
  // карточки. Значит, ни диалогом, ни ловушкой фокуса она быть не должна.
  it('не диалог и не запирает фокус', async () => {
    const user = userEvent.setup()
    show('side')

    expect(screen.queryByRole('dialog')).toBeNull()

    const outside = screen.getByRole('button', { name: 'Снаружи' })
    outside.focus()
    await user.tab()
    // Фокус свободно уходит внутрь панели и так же свободно вернётся:
    // ничего не перехвачено.
    expect(document.activeElement).not.toBe(outside)
  })
})

describe('перекрывающая панель', () => {
  it('объявляет себя диалогом', () => {
    show('center')
    const dialog = screen.getByRole('dialog')
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(dialog.getAttribute('aria-label')).toBe('Карточка')
  })

  // Ловушка фокуса — не украшение: без неё Tab уводит на доску, которая
  // в этот момент перекрыта, и человек с клавиатуры оказывается там,
  // где ничего не видит.
  it('не выпускает фокус наружу по кругу', async () => {
    const user = userEvent.setup()
    show('full')

    const inside = screen.getAllByRole('button').filter((b) => b.textContent?.startsWith('Внутри'))
    expect(inside.length).toBe(2)

    // Проходим по кругу заведомо больше раз, чем в панели элементов:
    // если фокус хоть раз выскочит наружу, кнопка «Снаружи» его поймает.
    const outside = screen.getByRole('button', { name: 'Снаружи' })
    for (let i = 0; i < 8; i++) {
      await user.tab()
      expect(document.activeElement).not.toBe(outside)
    }
  })

  it('закрывается по Escape', async () => {
    const user = userEvent.setup()
    let closed = 0
    show('center', () => closed++)

    await user.keyboard('{Escape}')
    expect(closed).toBe(1)
  })

  // Фокус возвращается туда, откуда пришли: иначе после закрытия он
  // улетает в начало страницы, и человек теряет место.
  it('возвращает фокус тому, кто её открыл', async () => {
    const opener = document.createElement('button')
    opener.textContent = 'Открыть'
    document.body.append(opener)
    opener.focus()

    const view = show('center')
    expect(document.activeElement).not.toBe(opener)

    view.unmount()
    expect(document.activeElement).toBe(opener)
    opener.remove()
  })
})

describe('память о режиме', () => {
  // Переключать режим каждый раз никто не станет — значит, выбор надо
  // помнить.
  it('запоминает выбранный режим между открытиями', () => {
    const first = renderHook(() => usePanelMode())
    expect(first.result.current[0]).toBe('side')
    act(() => first.result.current[1]('full'))
    first.unmount()

    const second = renderHook(() => usePanelMode())
    expect(second.result.current[0]).toBe('full')
  })
})

describe('ширина боковой панели', () => {
  const size = () => document.documentElement.style.getPropertyValue('--panel-side-size')

  it('у боковой панели есть ручка ширины, у перекрывающей — нет', () => {
    const side = show('side')
    const handle = screen.getByRole('separator', { name: 'Ширина панели' })
    expect(handle.getAttribute('aria-valuenow')).toBe('26')
    side.unmount()
    show('center')
    expect(screen.queryByRole('separator')).toBeNull()
  })

  it('ручку ведут влево — панель шире; сохраняется на отпускании', () => {
    const view = show('side')
    const handle = screen.getByRole('separator', { name: 'Ширина панели' })
    fireEvent.pointerDown(handle, { clientX: 500, pointerId: 1, button: 0 })
    fireEvent.pointerMove(handle, { clientX: 340, pointerId: 1 })
    expect(size()).toBe('36rem')
    expect(localStorage.getItem('panel-side-width')).toBeNull()
    fireEvent.pointerUp(handle, { clientX: 340, pointerId: 1 })
    expect(localStorage.getItem('panel-side-width')).toBe('36')

    // Закрыли панель — доска забирает место обратно.
    view.unmount()
    expect(size()).toBe('')

    // Открыли снова — ширина та же.
    show('side')
    expect(size()).toBe('36rem')
  })

  it('стрелки двигают шагом, двойной щелчок возвращает исходную', async () => {
    const user = userEvent.setup()
    show('side')
    const handle = screen.getByRole('separator', { name: 'Ширина панели' })
    handle.focus()
    await user.keyboard('{ArrowLeft}')
    expect(handle.getAttribute('aria-valuenow')).toBe('28')
    await user.keyboard('{ArrowRight}{ArrowRight}')
    expect(handle.getAttribute('aria-valuenow')).toBe('24')
    await user.dblClick(handle)
    expect(handle.getAttribute('aria-valuenow')).toBe('26')
    expect(localStorage.getItem('panel-side-width')).toBeNull()
  })
})
