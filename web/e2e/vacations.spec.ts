import { expect, test } from '@playwright/test'

for (const lang of ['ru', 'en'] as const) for (const width of [1440, 360]) for (const theme of ['light', 'dark'] as const) {
  test(`Отпуск и замещение: ${lang}, ${width}, ${theme}`, async ({ page, browser, baseURL }, info) => {
    await page.setViewportSize({ width, height: 900 })
    const settings = { lang, theme }
    await page.addInitScript(({ lang, theme }) => { localStorage.setItem('lang', lang); localStorage.setItem('theme', theme) }, settings)
    const suffix = crypto.randomUUID()
    expect((await page.request.post('/api/auth/register', { data: { org: `Vacation ${suffix}`, name: lang === 'ru' ? 'Анна Планировщик' : 'Anna Planner', email: `vacation-${suffix}@example.test`, password: 'parol12345' } })).ok()).toBeTruthy()
    const owner = await (await page.request.get('/api/me')).json()
    const context = await browser.newContext({ baseURL, viewport: { width, height: 900 } })
    try {
      await context.addInitScript(({ lang, theme }) => { localStorage.setItem('lang', lang); localStorage.setItem('theme', theme) }, settings)
      const invite = await (await page.request.post('/api/invites', { data: { email: `sub-${suffix}@example.test`, role: 'member' } })).json()
      expect((await context.request.post('/api/invites/accept', { data: { token: invite.link.split('/').at(-1), name: lang === 'ru' ? 'Борис Заместитель' : 'Boris Substitute', password: 'parol12345' } })).ok()).toBeTruthy()
      const member = await (await context.request.get('/api/me')).json()
      const departmentName = lang === 'ru' ? 'Разработка и сопровождение корпоративных сервисов' : 'Development and maintenance of corporate services'
      const teamName = lang === 'ru' ? 'Команда платформы' : 'Platform team'
      const otherName = lang === 'ru' ? 'Эксплуатация и поддержка пользователей' : 'Operations and user support'
      const createTeam = async (name: string, parentId: string | null = null) => {
        const response = await page.request.post('/api/teams', { data: { name, parentId } })
        expect(response.ok(), await response.text()).toBeTruthy()
        return response.json()
      }
      const department = await createTeam(departmentName), team = await createTeam(teamName, department.id), other = await createTeam(otherName)
      expect((await page.request.put(`/api/teams/${team.id}/members/${owner.id}`)).ok()).toBeTruthy()
      expect((await page.request.put(`/api/teams/${other.id}/members/${member.id}`)).ok()).toBeTruthy()
      const board = await (await page.request.post('/api/boards', { data: { name: lang === 'ru' ? 'План поставки оборудования' : 'Equipment delivery plan' } })).json()
      const snap = await (await page.request.get(`/api/boards/${board.id}`)).json()
      const op = async (type: string, payload: unknown) => {
        const res = await page.request.post(`/api/boards/${board.id}/operations`, { data: { operationId: crypto.randomUUID(), type, payload } })
        expect(res.ok(), await res.text()).toBeTruthy(); return res.json()
      }
      const title = lang === 'ru' ? 'Согласовать поставку оборудования с подрядчиком и командой эксплуатации' : 'Agree equipment delivery with the contractor and the operations team'
      const card = (await op('CREATE_CARD', { columnId: snap.columns[0].id, title, assigneeIds: [owner.id], place: 'end' })).patch.cards[0]
      const initial = await (await page.request.get('/api/availability')).json()
      const start = initial.today, end = new Date(Date.parse(`${start}T00:00:00Z`) + 6 * 86400000).toISOString().slice(0, 10)
      await op('SET_CARD_PLAN', { cardId: card.id, plannedStartOn: start, plannedEndOn: end, expected: { plannedStartOn: null, plannedEndOn: null } })
      await page.goto('/team?view=vacations&mine=1')
      await page.getByRole('button', { name: lang === 'ru' ? 'Добавить отпуск' : 'Add vacation', exact: true }).click()
      await page.getByLabel(lang === 'ru' ? 'Начало отпуска' : 'Vacation start', { exact: true }).fill(start)
      await page.getByLabel(lang === 'ru' ? 'Последний день отпуска' : 'Last day of vacation', { exact: true }).fill(end)
      await page.getByLabel(lang === 'ru' ? 'Примечание' : 'Note', { exact: true }).fill(lang === 'ru' ? 'Документы передам до отъезда' : 'I will hand over the documents before leaving')
      await page.getByRole('button', { name: lang === 'ru' ? 'Сохранить отпуск' : 'Save vacation', exact: true }).click()
      await expect.poll(async () => (await (await page.request.get('/api/availability')).json()).vacations.length).toBe(1)
      await expect(page.locator('.vacation-calendar .vacation-day')).toHaveCount(7)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
      await page.screenshot({ path: info.outputPath('calendar.png'), fullPage: true })
      await page.getByRole('button', { name: lang === 'ru' ? 'Добавить отпуск' : 'Add vacation', exact: true }).click()
      await page.getByLabel(lang === 'ru' ? 'Начало отпуска' : 'Vacation start', { exact: true }).fill(start)
      await page.getByLabel(lang === 'ru' ? 'Последний день отпуска' : 'Last day of vacation', { exact: true }).fill(end)
      await page.getByRole('button', { name: lang === 'ru' ? 'Сохранить отпуск' : 'Save vacation', exact: true }).click()
      await expect(page.getByText(lang === 'ru' ? 'эти даты пересекаются с другим вашим отпуском' : 'these dates overlap another of your vacations', { exact: true })).toBeVisible()
      await page.screenshot({ path: info.outputPath('vacation-error.png'), fullPage: true })
      await page.getByRole('button', { name: lang === 'ru' ? 'Отмена' : 'Cancel', exact: true }).click()

      // Участник видит отпуска коллег, объединяет команды и поднимается
      // на родительскую ветвь. Перезагрузка сохраняет несколько корней.
      const substitute = await context.newPage()
      await substitute.goto('/team?view=vacations')
      const calendar = substitute.locator('.vacation-calendar')
      await expect(calendar.locator('tbody tr')).toHaveCount(2)
      await expect(calendar.locator('.vacation-day')).toHaveCount(7)
      const teams = substitute.getByRole('combobox', { name: lang === 'ru' ? 'Подразделения' : 'Subdivisions', exact: true })
      await teams.selectOption(team.id)
      await expect(calendar.locator('tbody tr')).toHaveCount(1)
      await expect(calendar.locator('tbody th')).toContainText(lang === 'ru' ? 'Анна Планировщик' : 'Anna Planner')
      await teams.selectOption(other.id)
      await expect(calendar.locator('tbody tr')).toHaveCount(2)
      expect(new URL(substitute.url()).searchParams.getAll('vacationTeam')).toEqual([team.id, other.id])
      await substitute.reload()
      await expect(calendar.locator('tbody tr')).toHaveCount(2)
      expect(await substitute.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
      await substitute.screenshot({ path: info.outputPath('calendar-teams.png'), fullPage: true })
      await substitute.getByRole('button', { name: `${lang === 'ru' ? 'Убрать подразделение:' : 'Remove subdivision:'} ${teamName}`, exact: true }).click()
      await expect(calendar.locator('tbody tr')).toHaveCount(1)
      await expect(calendar.locator('tbody th')).toContainText(lang === 'ru' ? 'Борис Заместитель' : 'Boris Substitute')
      await substitute.getByRole('button', { name: lang === 'ru' ? 'Вся организация' : 'Whole organisation', exact: true }).click()
      await expect(calendar.locator('tbody tr')).toHaveCount(2)
      await teams.selectOption(department.id)
      await expect(calendar.locator('tbody tr')).toHaveCount(1)
      await expect(calendar.locator('tbody th')).toContainText(lang === 'ru' ? 'Анна Планировщик' : 'Anna Planner')
      await teams.selectOption(team.id)
      await expect(calendar.locator('tbody tr')).toHaveCount(1)
      await substitute.screenshot({ path: info.outputPath('calendar-hierarchy.png'), fullPage: true })
      await substitute.goto(`/tasks?user=${owner.id}`)
      await expect(substitute.locator('.vacation-warning')).toContainText(lang === 'ru' ? 'Замещение не подтверждено' : 'Coverage is not confirmed')
      await expect(substitute.locator('.vacation-warning')).toContainText(lang === 'ru' ? 'Анна Планировщик' : 'Anna Planner')
      expect((await substitute.locator('.vacation-warning').boundingBox())!.width).toBeGreaterThanOrEqual(200)
      expect(await substitute.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
      await substitute.screenshot({ path: info.outputPath('tasks-vacation-warning.png'), fullPage: true })

      // Массовое предложение использует тот же контракт и сохраняет
      // основного исполнителя. По одной задаче доступна отдельная форма.
      if (lang === 'ru' && width === 1440 && theme === 'light') {
        const bulk = page.getByRole('form', { name: 'Предложить замещение выбранных задач' })
        await bulk.getByRole('checkbox').check()
        await bulk.getByLabel('Кто замещает').selectOption(member.id)
        await bulk.getByRole('button', { name: 'Предложить замещение', exact: true }).click()
      } else {
        await page.goto(`/board/${board.id}/card/${card.id}`)
        await page.locator('.panel-card').getByRole('tab', { name: lang === 'ru' ? 'Работа' : 'Work', exact: true }).click()
        const coverage = page.getByRole('region', { name: lang === 'ru' ? 'Замещение' : 'Coverage', exact: true })
        await coverage.getByLabel(lang === 'ru' ? 'Кто замещает' : 'Who covers', { exact: true }).selectOption(member.id)
        await coverage.getByRole('button', { name: lang === 'ru' ? 'Предложить замещение' : 'Propose coverage', exact: true }).click()
      }
      await expect.poll(async () => (await (await page.request.get('/api/availability')).json()).coverages[0]?.status).toBe('proposed')
      await substitute.goto(`/board/${board.id}/card/${card.id}`)
      await substitute.locator('.panel-card').getByRole('tab', { name: lang === 'ru' ? 'Работа' : 'Work', exact: true }).click()
      const accept = substitute.getByRole('button', { name: lang === 'ru' ? 'Принять' : 'Accept', exact: true })
      await accept.click()
      await expect.poll(async () => (await (await page.request.get('/api/availability')).json()).coverages[0]?.status).toBe('accepted')
      await substitute.screenshot({ path: info.outputPath('coverage.png'), fullPage: true })
      const saved = await (await page.request.get(`/api/boards/${board.id}`)).json()
      expect(saved.cardAssignees[card.id]).toEqual([owner.id])
      await substitute.goto('/tasks?covering=1')
      await expect(substitute.getByRole('button', { name: title, exact: false })).toBeVisible()
      await expect(substitute.locator('.vacation-warning')).toContainText(lang === 'ru' ? 'Замещение подтверждено на весь отпуск' : 'Coverage is confirmed for the entire vacation')
      expect(await substitute.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
      await substitute.screenshot({ path: info.outputPath('covering-tasks.png'), fullPage: true })
      await page.goto(`/board/${board.id}?view=gantt&ganttFrom=${start}`)
      await expect(page.locator('.gantt-name .vacation-warning')).toContainText(lang === 'ru' ? 'Замещение подтверждено на весь отпуск' : 'Coverage is confirmed for the entire vacation')
      const row = page.locator(`.gantt-row[data-card-id="${card.id}"]`)
      const name = await row.locator('.gantt-name').boundingBox(), track = await row.locator('.gantt-track').boundingBox()
      expect(Math.abs(name!.height - track!.height)).toBeLessThan(1)
      await page.screenshot({ path: info.outputPath('gantt-vacation.png'), fullPage: true })
      await page.goto('/team?view=vacations&mine=1')
      await page.getByRole('button', { name: lang === 'ru' ? 'Отменить отпуск' : 'Cancel vacation', exact: true }).click()
      await expect.poll(async () => (await (await page.request.get('/api/availability')).json()).coverages[0]?.valid).toBe(false)
      // Возврат в уже открытый список перечитывает договорённости без F5.
      await substitute.evaluate(() => window.dispatchEvent(new Event('focus')))
      await expect(substitute.getByRole('button', { name: title, exact: false })).toHaveCount(0)
    } finally { await context.close() }
  })
}
