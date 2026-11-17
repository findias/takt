import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

// Те же записанные демоданные, что у visual.spec.ts. Эти проверки
// закрепляют геометрию таблицы, которой нет в штатных визуальных эталонах.
for (const lang of ['ru', 'en']) for (const width of [1440, 360]) for (const theme of ['light', 'dark'] as const) {
  test(`иерархическая таблица: ${lang}, ${width}, ${theme}`, async ({ browser }, info) => {
    const recorded = JSON.parse(readFileSync(new URL(`./visual/${lang}.json`, import.meta.url), 'utf8'))
    const key = `GET /api/boards/${recorded.boardId}`
    const board = JSON.parse(recorded.responses[key].body)
    const title = (lang === 'ru'
      ? 'Полное название важной задачи должно оставаться читаемым на узком экране без обрезки и многоточия. '
      : 'The entire title of an important task must remain readable on a narrow screen without clipping or ellipsis. ').repeat(3)
    board.cards[0].title = title
    recorded.responses[key].body = JSON.stringify(board)
    const context = await browser.newContext({ baseURL: info.project.use.baseURL, locale: lang === 'ru' ? 'ru-RU' : 'en-GB', viewport: { width, height: 900 }, colorScheme: theme })
    try {
      const page = await context.newPage()
      const errors: string[] = []
      page.on('pageerror', (e) => errors.push(e.message))
      await page.clock.setFixedTime(recorded.now)
      await page.route('**/api/**', async (route) => {
        const url = new URL(route.request().url())
        // При запуске на Vite путь исходного модуля тоже содержит /api/.
        if (!url.pathname.startsWith('/api/')) return route.continue()
        if (url.pathname.endsWith('/stream')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
        if (url.pathname.endsWith('/tree')) return route.fulfill({ status: 200, contentType: 'application/json', body: '{"nodes":[]}' })
        const response = recorded.responses[`${route.request().method()} ${url.pathname}${url.search}`]
        await route.fulfill(response ? { status: response.status, contentType: 'application/json', body: response.body }
          : { status: 404, contentType: 'application/json', body: '{"error":"not recorded"}' })
      })
      await page.goto(`/board/${recorded.boardId}?view=tree&treeAlone=1`)
      const tree = page.locator('.tree-view')
      const task = tree.locator('.tree-title').filter({ hasText: title })
      await expect(task).toBeVisible()
      const search = tree.getByRole('searchbox')
      expect((await search.boundingBox())!.width).toBeGreaterThan(180)
      const dimensions = await task.evaluate((el) => ({ scroll: el.scrollHeight, client: el.clientHeight, clamp: getComputedStyle(el).webkitLineClamp }))
      expect(dimensions.client).toBeGreaterThanOrEqual(dimensions.scroll)
      expect(dimensions.clamp).toBe('none')
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      await expect(page.locator('.column-switch')).toHaveCount(0)
      const firstCell = tree.locator('tbody td').first()
      const before = (await firstCell.boundingBox())!.x
      await tree.locator('.table-wrap').evaluate((el) => { el.scrollLeft = 500 })
      expect(Math.abs((await firstCell.boundingBox())!.x - before)).toBeLessThan(2)
      await tree.locator('.table-wrap').evaluate((el) => { el.scrollLeft = 0 })
      await page.screenshot({ path: info.outputPath('tree.png'), fullPage: true })
      await search.focus()
      await search.pressSequentially(lang === 'ru' ? 'важной задачи' : 'important task')
      await expect(task).toBeVisible()
      await page.screenshot({ path: info.outputPath('search.png'), fullPage: true })
      await search.fill('zzzz-no-match')
      await expect(tree.getByRole('table')).toHaveCount(0)
      expect(errors).toEqual([])
    } finally {
      await context.close()
    }
  })
}
