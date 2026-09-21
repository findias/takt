// Кому экран структуры даёт действия.
//
// Проверяется не разметка, а совпадение экрана с тем, что разрешает
// база: администратор области распоряжается ею и всем под ней, рядовой
// участник — ничем. До 23 августа 2026 экран спрашивал одно —
// «владелец ли ты», — и администратор области, которому и политика,
// и текст отказа, и соседний раздел этого экрана обещали его область,
// не видел у неё ни одной кнопки.
//
// Второй случай тут же: «Вернуть» в архиве стояло у всякой строки
// и у всякого смотрящего, а рядовой участник получал в ответ
// «не найдено» про узел, названный строкой выше.

import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import type { ArchivedTeam, Principal, Team, TeamAdmin } from '../shared/api/index.ts'

const AREA = 't-area'
const DEPT = 't-dept'
const OTHER = 't-other'

const TEAMS: Team[] = [
  { id: AREA, name: 'Область', parentId: null, depth: 1, members: 1, boards: 0, fromDirectory: false },
  { id: DEPT, name: 'Отдел', parentId: AREA, depth: 2, members: 0, boards: 0, fromDirectory: false },
  { id: OTHER, name: 'Соседняя область', parentId: null, depth: 1, members: 0, boards: 0, fromDirectory: false },
]

const ARCHIVED: ArchivedTeam[] = [
  {
    id: 'a-mine',
    name: 'Убранный свой',
    parentId: AREA,
    parentName: 'Область',
    parentArchived: false,
    archivedAt: '2026-08-20T10:00:00Z',
  },
  {
    id: 'a-foreign',
    name: 'Убранный чужой',
    parentId: OTHER,
    parentName: 'Соседняя область',
    parentArchived: false,
    archivedAt: '2026-08-20T10:00:00Z',
  },
]

const BORIS: Principal = {
  id: 'u-boris',
  email: 'boris@example.test',
  name: 'Борис Ветров',
  orgId: 'org-1',
  orgName: 'Северный проект',
  orgSlug: 'sever',
  role: 'member',
  estimateUnit: 'points',
}

const listAdmins = vi.fn()

vi.mock('../shared/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../shared/api')>()
  return {
    ...real,
    api: {
      ...real.api,
      listTeams: vi.fn().mockResolvedValue({ teams: TEAMS }),
      team: vi.fn().mockResolvedValue({ members: [{ userId: 'u-boris', name: 'Борис Ветров', email: 'boris@example.test', role: 'member', kind: 'person', joinedAt: '' }] }),
      listObservers: vi.fn().mockResolvedValue({ observers: [] }),
      listAdmins,
      archivedTeams: vi.fn().mockResolvedValue({ teams: ARCHIVED }),
      teamMembers: vi.fn().mockResolvedValue({ members: [] }),
      teamBoards: vi.fn().mockResolvedValue({ boards: [] }),
    },
  }
})

const { Structure } = await import('./Structure')

function show(admins: TeamAdmin[]) {
  listAdmins.mockResolvedValue({ admins })
  return render(<Structure principal={BORIS} onOpenBoard={() => {}} />)
}

const ADMIN_OF_AREA: TeamAdmin[] = [
  { id: 'adm-1', userId: 'u-boris', name: 'Борис Ветров', email: 'boris@example.test', teamId: AREA, teamName: 'Область' },
]

/** Действия узла живут в меню строки — пункты читаются, только когда
 *  меню открыто. */
async function actionsOf(name: string) {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: `Действия подразделения «${name}»` }))
  const menu = await screen.findByRole('menu')
  const items = within(menu)
    .getAllByRole('menuitem')
    .map((i) => i.textContent)
  await user.keyboard('{Escape}')
  return items
}

it('администратор области распоряжается ею и всем под ней, но не соседней и не корнем', async () => {
  show(ADMIN_OF_AREA)

  // Своя область и отдел под ней — действия на месте.
  await waitFor(() => screen.getByRole('button', { name: 'Действия подразделения «Область»' }))
  expect(await actionsOf('Область')).toEqual([
    'Завести отдел…',
    'Переименовать…',
    'Перенести…',
    'Убрать подразделение',
  ])
  expect(await actionsOf('Отдел')).toContain('Убрать подразделение')

  // Соседняя область — ни одного.
  expect(screen.queryByRole('button', { name: 'Действия подразделения «Соседняя область»' })).toBeNull()

  // Корень остаётся за владельцем: у нового корневого узла нет старшего,
  // а значит нет и того, кто за него отвечает.
  expect(screen.queryByRole('button', { name: 'Новое подразделение' })).toBeNull()

  // Из архива возвращается своё, чужое — нет.
  screen.getByLabelText('Вернуть из архива: Убранный свой')
  expect(screen.queryByLabelText('Вернуть из архива: Убранный чужой')).toBeNull()
})

it('у строки одно видимое действие — меню, а не ряд из четырёх', async () => {
  // Ряд «+ отдел · Переименовать · Перенести… · Убрать подразделение»
  // стоял у каждой строки и делал из дерева таблицу действий: названия
  // терялись среди глаголов, а убирающее стояло вплотную к безобидному.
  show(ADMIN_OF_AREA)
  await waitFor(() => screen.getByRole('button', { name: 'Действия подразделения «Область»' }))
  expect(screen.queryByRole('button', { name: /^Переименовать/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /^Убрать подразделение/ })).toBeNull()
  expect(screen.queryByRole('combobox', { name: /^Перенести/ })).toBeNull()
})

it('пункт с вопросом открывает поле у строки и ставит в него фокус', async () => {
  const user = userEvent.setup()
  show(ADMIN_OF_AREA)
  await user.click(await screen.findByRole('button', { name: 'Действия подразделения «Область»' }))
  await user.click(await screen.findByRole('menuitem', { name: 'Завести отдел…' }))
  expect(document.activeElement).toBe(screen.getByRole('textbox', { name: 'Завести отдел в «Область»' }))

  await user.click(screen.getByRole('button', { name: 'Действия подразделения «Область»' }))
  await user.click(await screen.findByRole('menuitem', { name: 'Перенести…' }))
  expect(document.activeElement).toBe(screen.getByRole('combobox', { name: 'Перенести: Область' }))
})

it('рядовому участнику экран остаётся, действия — нет', async () => {
  show([])

  // Дерево читается: узел на месте, свёрнутый.
  await waitFor(() => screen.getByRole('button', { name: '▸ Область' }))
  expect(screen.queryByRole('button', { name: /^Действия подразделения/ })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Новое подразделение' })).toBeNull()
  // Список убранного читают все — он отвечает на «куда делось
  // подразделение», — но кнопки, которая заведомо ответит отказом, нет.
  screen.getByText('Убранный свой')
  expect(screen.queryByLabelText('Вернуть из архива: Убранный свой')).toBeNull()
})
