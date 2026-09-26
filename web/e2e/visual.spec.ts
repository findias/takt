import { expect, test, type Page } from '@playwright/test'
import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

// Визуальные эталоны (PROMPT-TESTING.md, уровень 6).
//
// Снимки `npm run screens` смотрели глазами, а сравнивать было не с чем:
// сползшую на четыре пикселя кнопку никто не замечал. Здесь основные
// экраны сравниваются с эталонами попиксельно.
//
// Эталон, снятый с живой базы, краснел бы сам по себе: возраст карточек,
// время в журнале и прогресс итерации считаются от «сейчас». Поэтому
// данные записываются один раз (VISUAL_RECORD=1 — ответы API и момент
// записи), а при сравнении страница получает записанные ответы вместо
// сервера и часы браузера стоят на моменте записи. База не нужна.
//
// Шрифты системные, и эталоны совпадают только на той машине, где сняты:
// это локальная проверка перед выпуском (`make visual`), а не часть CI.
// Расхождение — вопрос, а не ошибка: посмотреть разницу в отчёте
// Playwright и либо чинить, либо обновить эталон отдельным коммитом
// («visual change: …») — `make visual-update`.

const here = dirname(fileURLToPath(import.meta.url))
const fixtures = (lang: string) => join(here, 'visual', `${lang}.json`)
const RECORD = process.env.VISUAL_RECORD === '1'

type Recorded = { now: number; boardId: string; responses: Record<string, { status: number; body: string }> }

const LANGS = [
  { lang: 'ru', locale: 'ru-RU', email: 'anna@example.test', board: 'Поставки' },
  { lang: 'en', locale: 'en-GB', email: 'anna@en.example.test', board: 'Supplies' },
] as const

const SCREENS = (boardId: string) => [
  { name: 'доски', path: '/' },
  { name: 'доска', path: `/board/${boardId}` },
  { name: 'задачи', path: '/tasks' },
  { name: 'отчёты', path: '/reports' },
  { name: 'команда', path: '/team' },
  { name: 'структура', path: '/structure' },
]

const key = (url: string) => {
  const u = new URL(url)
  return u.pathname + u.search
}

for (const { lang, locale, email, board } of LANGS) {
  test(`визуальные эталоны: ${lang}`, async ({ browser }) => {
    test.setTimeout(240_000)
    if (RECORD) {
      await record(browser, lang, locale, email, board)
      return
    }
    test.skip(!existsSync(fixtures(lang)), 'нет записи — make visual-update')
    const recorded: Recorded = JSON.parse(readFileSync(fixtures(lang), 'utf8'))

    for (const width of [1440, 360]) {
      for (const scheme of ['light', 'dark'] as const) {
        if (width === 360 && scheme === 'dark') continue
        const context = await browser.newContext({ locale, viewport: { width, height: 900 }, colorScheme: scheme })
        const page = await context.newPage()
        const missing = await replay(page, recorded)
        for (const screen of SCREENS(recorded.boardId)) {
          await page.goto(screen.path)
          await settle(page)
          await expect(page, `${lang} ${screen.name} ${width} ${scheme}`).toHaveScreenshot(
            `${lang}-${screen.name}-${width}-${scheme}.png`,
            { fullPage: true, animations: 'disabled', maxDiffPixelRatio: 0.002 },
          )
        }
        expect(missing, 'запросы, которых нет в записи — перезапишите: make visual-update').toEqual([])
        await context.close()
      }
    }
  })
}

async function settle(page: Page) {
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(400)
}

async function replay(page: Page, recorded: Recorded): Promise<string[]> {
  const missing: string[] = []
  await page.clock.setFixedTime(recorded.now)
  await page.route('**/api/**', async (route) => {
    const k = key(route.request().url())
    if (k.endsWith('/stream')) {
      // Поток изменений молчит: изменений в записи нет.
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
      return
    }
    const hit = recorded.responses[`${route.request().method()} ${k}`]
    if (!hit) {
      missing.push(`${route.request().method()} ${k}`)
      await route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"нет в записи"}' })
      return
    }
    await route.fulfill({ status: hit.status, contentType: 'application/json', body: hit.body })
  })
  return missing
}

async function record(
  browser: import('@playwright/test').Browser,
  lang: string,
  locale: string,
  email: string,
  boardName: string,
) {
  const context = await browser.newContext({ locale, viewport: { width: 1440, height: 900 } })
  const page = await context.newPage()
  const out: Recorded = { now: Date.now(), boardId: '', responses: {} }
  page.on('response', async (response) => {
    const req = response.request()
    const k = key(response.url())
    if (!k.startsWith('/api/') || k.endsWith('/stream') || req.method() !== 'GET') return
    const type = response.headers()['content-type'] ?? ''
    if (!type.includes('json')) return
    try {
      out.responses[`GET ${k}`] = { status: response.status(), body: await response.text() }
    } catch {
      // Ответ ушёл вместе со страницей — возьмём его на следующем экране.
    }
  })

  await page.goto('/')
  await page.getByLabel(lang === 'ru' ? 'Почта' : 'E-mail').fill(email)
  await page.getByLabel(lang === 'ru' ? 'Пароль' : 'Password').fill('parol12345')
  await page.getByRole('button', { name: lang === 'ru' ? 'Войти' : 'Sign in', exact: true }).click()
  const boardButton = page.getByRole('button', { name: boardName, exact: true })
  await expect(boardButton, 'нет демо — make demo').toBeVisible({ timeout: 15_000 })
  await boardButton.click()
  await page.waitForURL(/\/board\//)
  out.boardId = new URL(page.url()).pathname.split('/')[2]

  // Обойти экраны так же, как их обходит сравнение, — на обеих ширинах
  // и темах запросы одни и те же, достаточно одной.
  for (const screen of SCREENS(out.boardId)) {
    await page.goto(screen.path)
    await settle(page)
  }
  mkdirSync(dirname(fixtures(lang)), { recursive: true })
  writeFileSync(fixtures(lang), JSON.stringify(out))
  await context.close()
}
