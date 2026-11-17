import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

// Меряем расположение результатов на записанных демоданных: проверка
// наличия сводки не замечает, что до неё нужно прокрутить целый экран.
for (const lang of ['ru', 'en']) for (const width of [1440, 360]) for (const theme of ['light', 'dark'] as const) {
  test(`отчёты: ${lang}, ${width}, ${theme}`, async ({ browser }, info) => {
    const recorded = JSON.parse(readFileSync(new URL(`./visual/${lang}.json`, import.meta.url), 'utf8'))
    const responses = recorded.responses as Record<string, { status: number; body: string }>
    const boards = JSON.parse(responses['GET /api/boards'].body)
    const name = (lang === 'ru' ? 'Доска с длинным названием для проверки переноса условий отчёта ' : 'A board with a long name to verify wrapping of report filters ').repeat(3)
    boards.boards[0].name = name
    responses['GET /api/boards'].body = JSON.stringify(boards)
    const context = await browser.newContext({ baseURL: info.project.use.baseURL, locale: lang === 'ru' ? 'ru-RU' : 'en-GB', viewport: { width, height: 900 }, colorScheme: theme })
    try {
      const page = await context.newPage()
      const errors: string[] = []
      page.on('pageerror', (e) => errors.push(e.message))
      await page.clock.setFixedTime(recorded.now)
      await page.route('**/api/**', async (route) => {
        const u = new URL(route.request().url())
        if (!u.pathname.startsWith('/api/')) return route.continue()
        const key = `${route.request().method()} ${u.pathname}${u.search}`
        const fallback = Object.keys(responses).find((k) => k.startsWith(`GET ${u.pathname}?`))
        const hit = responses[key] ?? (fallback ? responses[fallback] : undefined)
        await route.fulfill(u.pathname.endsWith('/stream') ? { status: 200, contentType: 'text/event-stream', body: '' }
          : hit ? { status: hit.status, contentType: 'application/json', body: hit.body }
            : { status: 404, contentType: 'application/json', body: '{"error":"not recorded"}' })
      })
      await page.goto('/reports')
      const firstResult = page.getByRole('heading', { name: lang === 'ru' ? 'Требует внимания сейчас' : 'Needs attention now' })
      await expect(firstResult).toBeVisible()
      await page.evaluate(() => scrollTo(0, 0))
      const result = (await firstResult.boundingBox())!
      await page.screenshot({ path: info.outputPath('report.png') })
      expect(result.y).toBeLessThan(width === 360 ? 700 : 600)
      const filter = page.locator('.reports-filter-panel')
      await expect(filter).not.toHaveAttribute('open', '')
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      await page.screenshot({ path: info.outputPath('report.png'), fullPage: false })
      if (width === 360) {
        await page.getByRole('combobox', { name: lang === 'ru' ? 'Готовый период' : 'Quick period' }).selectOption('days30')
        await expect(page).toHaveURL(/period=days30/)
      }
      const trigger = filter.locator(':scope > summary')
      const box = (await trigger.boundingBox())!
      expect(await trigger.evaluate((el) => { const r = el.getBoundingClientRect(); return el.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)) })).toBe(true)
      expect(box.height).toBeGreaterThanOrEqual(24)
      await trigger.focus()
      await page.keyboard.press('Enter')
      await expect(filter).toHaveAttribute('open', '')
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      await page.screenshot({ path: info.outputPath('filters-open.png') })
      await page.getByRole('combobox', { name: lang === 'ru' ? 'Доски' : 'Boards', exact: true }).selectOption(boards.boards[0].id)
      const conditions = page.getByRole('list', { name: lang === 'ru' ? 'Активные условия отчёта' : 'Active report filters' })
      await expect(conditions).toContainText(name)
      await trigger.click()
      await expect(conditions).toBeVisible()
      const fullCondition = conditions.getByRole('button').first()
      expect(await fullCondition.evaluate((el) => el.clientHeight >= el.scrollHeight)).toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      await expect(page.locator('.reports > [role="status"]')).toContainText('22')
      await page.screenshot({ path: info.outputPath('filtered.png') })
      await expect(page.getByRole('link', { name: 'CSV', exact: true })).toHaveAttribute('href', new RegExp(`board=${boards.boards[0].id}`))
      console.log(`${lang} ${width} ${theme}: first result y=${Math.round(result.y)}`)
      await page.emulateMedia({ media: 'print' })
      await page.screenshot({ path: info.outputPath('print.png') })
      await expect(conditions).toBeVisible()
      expect(errors).toEqual([])
    } finally { await context.close() }
  })
}
