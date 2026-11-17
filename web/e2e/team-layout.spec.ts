import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

for (const source of ['ru', 'en', 'ru-boris']) for (const width of [1440, 360]) for (const theme of ['light', 'dark'] as const) {
  test(`состав команды: ${source}, ${width}, ${theme}`, async ({ browser }, info) => {
    const lang = source === 'en' ? 'en' : 'ru'
    const r = JSON.parse(readFileSync(new URL(`./visual/${source}.json`, import.meta.url), 'utf8'))
    const team = JSON.parse(r.responses['GET /api/team'].body)
    const me = JSON.parse(r.responses['GET /api/me'].body)
    const member = team.members.find((m: { kind: string; userId: string; erasable: boolean }) => m.kind === 'person' && m.userId !== me.id && m.erasable)
    const context = await browser.newContext({ baseURL: info.project.use.baseURL, locale: lang === 'ru' ? 'ru-RU' : 'en-GB', viewport: { width, height: 900 }, colorScheme: theme })
    try {
      const page = await context.newPage(), errors: string[] = []
      page.on('pageerror', (e) => errors.push(e.message))
      await page.clock.setFixedTime(r.now)
      await page.route('**/api/**', async (route) => {
        const u = new URL(route.request().url())
        if (!u.pathname.startsWith('/api/')) return route.continue()
        const hit = r.responses[`${route.request().method()} ${u.pathname}${u.search}`]
        await route.fulfill(u.pathname.endsWith('/stream') ? { status: 200, contentType: 'text/event-stream', body: '' }
          : hit ? { status: hit.status, contentType: 'application/json', body: hit.body }
            : { status: 404, contentType: 'application/json', body: '{"error":"not recorded"}' })
      })
      await page.goto('/team')
      const list = page.locator('.team-people')
      await expect(list.locator(':scope > li')).toHaveCount(team.members.length)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      const row = list.locator(':scope > li').filter({ hasText: member.email })
      const trigger = row.getByRole('button', { name: new RegExp(lang === 'ru' ? '^Действия участника:' : '^Member actions:') })
      const role = row.getByRole('combobox')
      if (me.role === 'owner') await expect(role).toHaveValue(member.role)
      else await expect(role).toHaveCount(0)
      await page.screenshot({ path: info.outputPath('team.png') })
      await trigger.focus()
      await page.keyboard.press('Enter')
      const menu = page.getByRole('menu')
      await expect(menu).toBeVisible()
      const bounds = (await menu.boundingBox())!
      expect(bounds.x).toBeGreaterThanOrEqual(0)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(width)
      expect(bounds.y).toBeGreaterThanOrEqual(0)
      expect(bounds.y + bounds.height).toBeLessThanOrEqual(900)
      expect(await menu.evaluate((el) => { const b = el.getBoundingClientRect(); return el.contains(document.elementFromPoint(b.x + b.width / 2, b.y + 16)) })).toBe(true)
      await expect(menu.getByRole('menuitem')).toHaveCount(me.role === 'owner' ? 5 : 1)
      await page.screenshot({ path: info.outputPath('menu.png') })
      await page.keyboard.press('Escape')
      await expect(trigger).toBeFocused()
      if (me.role === 'owner') {
        await trigger.click()
        await page.getByRole('menuitem', { name: lang === 'ru' ? 'Имя…' : 'Name…', exact: true }).click()
        const dialog = page.getByRole('dialog')
        await expect(dialog).toBeVisible()
        await expect(dialog.getByRole('textbox')).toBeFocused()
        await page.screenshot({ path: info.outputPath('name-dialog.png') })
        await dialog.getByRole('button', { name: lang === 'ru' ? 'Отмена' : 'Cancel', exact: true }).click()
        await expect(trigger).toBeFocused()
      }
      await trigger.click()
      await page.getByRole('menuitem', { name: lang === 'ru' ? 'Удалить данные' : 'Erase data', exact: true }).click()
      const confirmation = page.getByRole('dialog')
      await expect(confirmation).toContainText(member.name)
      await page.screenshot({ path: info.outputPath('erase-dialog.png') })
      await confirmation.getByRole('button', { name: lang === 'ru' ? 'Отмена' : 'Cancel', exact: true }).click()
      await expect(trigger).toBeFocused()
      if (width === 360) {
        member.name = (lang === 'ru' ? 'Очень длинное имя участника команды ' : 'A very long team member name ').repeat(3)
        r.responses['GET /api/team'].body = JSON.stringify(team)
        await page.reload()
        const long = list.locator(':scope > li').filter({ hasText: member.email }).locator('.member-who > .wrap-title').first()
        await expect(long).toHaveText(member.name)
        expect(await long.evaluate((el) => el.clientHeight >= el.scrollHeight && getComputedStyle(el).whiteSpace === 'normal')).toBe(true)
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
        await page.screenshot({ path: info.outputPath('long-name.png') })
      }
      expect(errors).toEqual([])
    } finally { await context.close() }
  })
}
