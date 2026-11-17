import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

type Recorded = {
  now: number
  boardId: string
  responses: Record<string, { status: number; body: string }>
}

function recording(lang: string): Recorded {
  return JSON.parse(readFileSync(new URL(`./visual/${lang}.json`, import.meta.url), 'utf8'))
}

// Разметку проверяем на одних данных и остановленных часах: возраст
// демо не должен менять результат проверки координат.
async function replay(page: Page, data: Recorded, lang: string) {
  await page.clock.setFixedTime(data.now)
  await page.addInitScript((value) => localStorage.setItem('lang', value), lang)
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) return route.continue()
    if (url.pathname.endsWith('/stream')) {
      return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    }
    const hit = data.responses[`${route.request().method()} ${url.pathname}${url.search}`]
    expect(hit, `нет записанного ответа: ${url.pathname}`).toBeTruthy()
    await route.fulfill({ ...hit, contentType: 'application/json' })
  })
}

for (const lang of ['ru', 'en']) {
  for (const colorScheme of ['light', 'dark'] as const) {
    for (const width of [1280, 1366]) {
      test(`компоновка ноутбука ${width} ${lang} ${colorScheme}`, async ({ page }) => {
        const data = recording(lang)
        await page.setViewportSize({ width, height: width === 1280 ? 720 : 768 })
        await page.emulateMedia({ colorScheme })
        await replay(page, data, lang)
        await page.goto(`/board/${data.boardId}`)
        const columns = page.locator('.column')
        await expect(columns.first().locator('.card').first()).toBeVisible()
        expect((await columns.first().locator('.card').first().boundingBox())!.y).toBeLessThan(220)
        expect((await columns.nth(1).locator('.card').first().boundingBox())!.y).toBeLessThan(250)
        await expect(page.locator('.filters-rest')).toBeHidden()
        await expect(page.locator('#board-context')).toBeHidden()
        const countVisible = () => columns.nth(1).locator('.cards').evaluate((area) => {
          const bounds = area.getBoundingClientRect()
          return [...area.querySelectorAll(':scope > [data-card]')].filter((card) => {
            const rect = card.getBoundingClientRect()
            return rect.top >= bounds.top - 1 && rect.bottom <= bounds.bottom + 1
          }).length
        })
        const compactCount = await countVisible()
        expect(compactCount).toBeGreaterThanOrEqual(width === 1280 ? 2 : 3)
        const first = columns.first().locator('.card').first()
        const neighbour = columns.first().locator('.card').nth(1)
        const neighbourHeight = (await neighbour.boundingBox())!.height
        const titleMetrics = await first.locator('.card-title').evaluate((node) => ({
          height: node.clientHeight, fullHeight: node.scrollHeight,
          width: node.clientWidth, fullWidth: node.scrollWidth,
          whiteSpace: getComputedStyle(node).whiteSpace,
        }))
        expect(titleMetrics.fullHeight).toBeLessThanOrEqual(titleMetrics.height + 1)
        expect(titleMetrics.fullWidth).toBeLessThanOrEqual(titleMetrics.width + 1)
        expect(titleMetrics.whiteSpace).not.toBe('nowrap')
        const expand = first.locator('.card-expand')
        await expand.click()
        await expect(expand).toHaveAttribute('aria-expanded', 'true')
        await expect(first.locator('.card-labels')).toBeVisible()
        await expect(neighbour).not.toHaveClass(/card--expanded/)
        expect((await neighbour.boundingBox())!.height).toBeCloseTo(neighbourHeight, 0)
        await expand.press('Enter')
        await expect(expand).toHaveAttribute('aria-expanded', 'false')
        await expect(first.locator('.card-labels')).toBeHidden()
        await first.hover()
        const select = first.locator('.card-check')
        await select.check()
        await expect(select).toBeChecked()
        const selectedBorder = await first.evaluate((node) => getComputedStyle(node).borderColor)
        await neighbour.hover()
        expect(await first.evaluate((node) => getComputedStyle(node).borderColor)).toBe(selectedBorder)
        await expect(expand).toHaveAttribute('aria-expanded', 'false')
        await select.uncheck()
        const checkMetrics = await select.evaluate((node) => ({
          target: node.getBoundingClientRect().width,
          square: parseFloat(getComputedStyle(node, '::before').width),
          shadow: getComputedStyle(node.closest('.card-tools')!).boxShadow,
        }))
        expect(checkMetrics.target).toBeGreaterThanOrEqual(24)
        expect(checkMetrics.square).toBeLessThanOrEqual(16)
        expect(checkMetrics.shadow).toBe('none')
        const tools = page.getByRole('button', { name: lang === 'ru' ? 'Инструменты' : 'Tools', exact: true })
        await tools.click()
        const details = page.getByRole('checkbox', { name: lang === 'ru' ? 'Подробные карточки' : 'Detailed cards', exact: true })
        await details.check()
        await tools.click()
        await expect(page.locator('.board-screen')).toHaveClass(/board-screen--detailed-cards/)
        expect(await countVisible()).toBeLessThan(compactCount)
        await page.reload()
        await expect(page.locator('.board-screen')).toHaveClass(/board-screen--detailed-cards/)
        await tools.click()
        await details.uncheck()
        await tools.click()
        await expect(page.locator('.board-screen')).not.toHaveClass(/board-screen--detailed-cards/)
        const rules = columns.nth(1).locator('.column-rules')
        await rules.locator('summary').click()
        await expect(rules).toHaveAttribute('open', '')
        await rules.locator('summary').click()
        const filter = page.getByRole('button', { name: lang === 'ru' ? 'Отбор' : 'Filter', exact: true })
        await filter.click()
        await page.getByRole('checkbox', { name: lang === 'ru' ? 'Заблокированные' : 'Blocked', exact: true }).check()
        await page.locator('.filters-toggle').click()
        await expect(page.locator('.filters-rest')).toBeHidden()
        await expect(page.locator('.filter-count')).toHaveText('1')
      })
    }

    test(`спокойная доска: телефон ${lang} ${colorScheme}`, async ({ browser, baseURL }) => {
      const context = await browser.newContext({
        baseURL, viewport: { width: 360, height: 900 }, hasTouch: true, isMobile: true,
        locale: lang === 'ru' ? 'ru-RU' : 'en-GB', colorScheme,
      })
      try {
        const page = await context.newPage()
        const data = recording(lang)
        await replay(page, data, lang)
        await page.goto(`/board/${data.boardId}`)
        await expect(page.locator('.card').first()).toBeVisible()
        expect((await page.locator('.card').first().boundingBox())!.y).toBeLessThan(400)
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360)

        const tools = page.getByRole('button', { name: lang === 'ru' ? 'Инструменты' : 'Tools', exact: true })
        await expect(page.locator('#board-context')).toBeHidden()
        await tools.click()
        await expect(page.locator('#board-context')).toBeVisible()
        await tools.click()
        await expect(page.locator('#board-context')).toBeHidden()

        // Постоянно видимые кнопки не закрывают метки, приоритет и оценку.
        for (const card of await page.locator('.card').all()) {
          const buttons = await card.locator('.card-tools').boundingBox()
          if (!buttons) continue
          for (const value of await card.locator('.priority-mark, .chip--card, .card-estimate, .card-age').all()) {
            const box = await value.boundingBox()
            if (!box) continue
            const overlapX = Math.min(box.x + box.width, buttons.x + buttons.width) - Math.max(box.x, buttons.x)
            const overlapY = Math.min(box.y + box.height, buttons.y + buttons.height) - Math.max(box.y, buttons.y)
            expect(overlapX > 0.5 && overlapY > 0.5, await value.textContent()).toBe(false)
          }
        }
      } finally {
        await context.close()
      }
    })

    test(`спокойная доска: блокировка и срок ${lang} ${colorScheme}`, async ({ page }) => {
      const data = recording(lang)
      const response = data.responses[`GET /api/boards/${data.boardId}`]
      const snapshot = JSON.parse(response.body)
      const blocked = snapshot.cards.find((card: { blocked?: unknown }) => card.blocked)
      blocked.dueOn = '2020-01-01'
      blocked.description = lang === 'ru' ? 'Описание работы после предупреждения' : 'Work description after the warning'
      blocked.title = (lang === 'ru' ? 'Полное название задачи без сокращения и потери смысла ' : 'The complete task title without truncation or loss of meaning ').repeat(3)
      response.body = JSON.stringify(snapshot)
      await page.setViewportSize({ width: 1440, height: 900 })
      await page.emulateMedia({ colorScheme })
      await replay(page, data, lang)
      // Штатная настройка ширины включает контейнерный запрос,
      // которого нет в обычном наборе снимков.
      await page.addInitScript(({ boardId, columns }) => {
        localStorage.setItem('column-widths', JSON.stringify({
          [boardId]: Object.fromEntries(columns.map((column: { id: string }) => [column.id, 13.125])),
        }))
      }, { boardId: data.boardId, columns: snapshot.columns })
      await page.goto(`/board/${data.boardId}`)
      const card = page.locator(`[data-card="${blocked.id}"]`)
      await expect(card.locator('.card-due--urgent')).toBeVisible()
      const titleNode = card.locator('.card-title')
      expect(await titleNode.evaluate((node) => node.scrollHeight <= node.clientHeight + 1 && node.scrollWidth <= node.clientWidth + 1)).toBe(true)
      const title = (await card.locator('.card-meta').boundingBox())!
      const alarm = (await card.locator(':scope > .mark--alarm').boundingBox())!
      expect(title.y + title.height).toBeLessThanOrEqual(alarm.y)
      await expect(card.locator('.card-age-strip')).toHaveCount(0)
      await card.locator('.card-expand').click()
      await expect(card.locator('.card-expanded-description')).toBeVisible()
      const expandedAlarm = (await card.locator(':scope > .mark--alarm').boundingBox())!
      const description = (await card.locator('.card-expanded-description').boundingBox())!
      expect(expandedAlarm.y + expandedAlarm.height).toBeLessThanOrEqual(description.y)

    })
  }
}

for (const lang of ['ru', 'en']) for (const width of [1280, 360]) {
  test(`родитель раскрывает подзадачи и возвращает исходную ${lang} ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 360 ? 900 : 720 })
    await page.emulateMedia({ colorScheme: width === 360 ? 'dark' : 'light' })
    const data = recording(lang)
    const snapshot = JSON.parse(data.responses[`GET /api/boards/${data.boardId}`].body)
    const link = snapshot.links.find((item: { kind: string; fromCard: string; toCard: string }) => item.kind === 'subtask' && snapshot.cards.some((card: { id: string }) => card.id === item.fromCard) && snapshot.cards.some((card: { id: string }) => card.id === item.toCard))
    const parent = snapshot.cards.find((card: { id: string }) => card.id === link.fromCard)
    await replay(page, data, lang)
    await page.route('**/api/boards/*/cards/*/comments**', (route) => route.fulfill({ json: { comments: [] } }))
    await page.route('**/api/cards/*/path', (route) => route.fulfill({ json: { path: route.request().url().includes(link.toCard) ? [{ id: parent.id, title: parent.title, boardId: data.boardId, visible: true }] : [] } }))
    const start = `/board/${data.boardId}/card/${link.toCard}`
    await page.goto(start)
    await page.locator('.card-path a').click()
    await expect(page.locator('.parent-return')).toBeVisible()
    await expect(page.locator('.related--source')).toContainText(lang === 'ru' ? 'Исходная подзадача' : 'Source subtask')
    const children = snapshot.links.filter((item: { kind: string; fromCard: string }) => item.kind === 'subtask' && item.fromCard === parent.id)
    expect(await page.locator('.related').count()).toBeGreaterThanOrEqual(children.length)
    if (width === 360) await page.locator('.related--source').scrollIntoViewIfNeeded()
    await expect(page.locator('.related--source')).toBeInViewport()
    await page.screenshot({ path: `/tmp/takt-parent-${lang}-${width}.png`, fullPage: true })
    await page.reload()
    await expect(page.locator('.related--source')).toBeVisible()
    await page.locator('.parent-return').click()
    await expect(page).toHaveURL(new RegExp(`${start}$`))
    await expect(page.locator('.parent-return')).toHaveCount(0)
  })
}

test('родитель на другой доске сохраняет экран задач и отбор', async ({ page }) => {
  const data = recording('ru')
  const snapshot = JSON.parse(data.responses[`GET /api/boards/${data.boardId}`].body)
  const link = snapshot.links.find((item: { kind: string; fromCard: string; toCard: string }) => item.kind === 'subtask' && snapshot.cards.some((card: { id: string }) => card.id === item.fromCard) && snapshot.cards.some((card: { id: string }) => card.id === item.toCard))
  const parent = snapshot.cards.find((card: { id: string }) => card.id === link.fromCard)
  const child = snapshot.cards.find((card: { id: string }) => card.id === link.toCard)
  const target = 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa'
  const other = structuredClone(snapshot)
  other.board.id = target
  other.cards = other.cards.filter((card: { id: string }) => card.id !== child.id)
  other.linked.push({ ...snapshot.linked[0], id: child.id, title: child.title, boardId: data.boardId })
  await replay(page, data, 'ru')
  await page.route('**/api/boards/*/cards/*/comments**', (route) => route.fulfill({ json: { comments: [] } }))
  await page.route(`**/api/boards/${target}**`, (route) => {
    const path = new URL(route.request().url()).pathname
    const original = data.responses[`GET ${path.replace(target, data.boardId)}`]
    return route.fulfill({ contentType: 'application/json', body: path.endsWith(target) ? JSON.stringify(other) : (original?.body ?? '{}') })
  })
  await page.route('**/api/tasks?*', (route) => route.fulfill({ ...data.responses['GET /api/tasks'], contentType: 'application/json' }))
  await page.route('**/api/cards/*/path', (route) => route.fulfill({ json: { path: [{ id: parent.id, title: parent.title, boardId: target, visible: true }] } }))
  const start = `/tasks?done=1&status=blocked&open=${data.boardId}:${child.id}`
  await page.goto(start)
  await page.locator('.card-path a').click()
  await expect(page.locator('.related--source')).toContainText(child.title)
  expect(new URL(page.url()).pathname).toBe('/tasks')
  expect(new URL(page.url()).searchParams.get('status')).toBe('blocked')
  expect(new URL(page.url()).searchParams.get('open')).toBe(`${target}:${parent.id}`)
  await page.locator('.parent-return').click()
  expect(new URL(page.url()).pathname + new URL(page.url()).search).toBe(start)
})
