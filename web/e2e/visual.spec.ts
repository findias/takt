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

type Recorded = {
  now: number
  boardId: string
  cardId?: string
  responses: Record<string, { status: number; body: string }>
}

// Кто смотрит. Владелец организации видит всё; владелец подразделения
// (Борис, «Платформа») видит «Команду» и «Структуру» иначе — своё
// поддерево, свои права, — и именно эти экраны менялись в этапе 31.
const LANGS = [
  { lang: 'ru', locale: 'ru-RU', email: 'anna@example.test', board: 'Поставки', only: null },
  { lang: 'en', locale: 'en-GB', email: 'anna@en.example.test', board: 'Supplies', only: null },
  { lang: 'ru-boris', locale: 'ru-RU', email: 'boris@example.test', board: 'Поставки', only: ['команда', 'структура'] },
] as const

type Screen = { name: string; path: string; tab?: number }

// Карточка — четырьмя вкладками: вкладка выбирается нажатием, адреса
// у неё нет.
const SCREENS = (boardId: string, cardId = ''): Screen[] => [
  { name: 'доски', path: '/' },
  { name: 'доска', path: `/board/${boardId}` },
  ...['обсуждение', 'работа', 'история', 'задачи'].map((tab, i) => ({
    name: `карточка-${tab}`,
    path: `/board/${boardId}/card/${cardId}`,
    tab: i,
  })),
  { name: 'задачи', path: '/tasks' },
  { name: 'отчёты', path: '/reports' },
  { name: 'команда', path: '/team' },
  { name: 'структура', path: '/structure' },
]

const screensFor = (only: readonly string[] | null, r: Pick<Recorded, 'boardId' | 'cardId'>) =>
  SCREENS(r.boardId, r.cardId).filter((s) => !only || only.includes(s.name))

async function show(page: Page, screen: Screen) {
  await page.goto(screen.path)
  if (screen.tab !== undefined) {
    const tab = page.getByRole('tab').nth(screen.tab)
    await tab.click()
    await expect(tab).toHaveAttribute('aria-selected', 'true')
  }
  await settle(page)
}

const key = (url: string) => {
  const u = new URL(url)
  return u.pathname + u.search
}

for (const { lang, locale, email, board, only } of LANGS) {
  test(`визуальные эталоны: ${lang}`, async ({ browser }) => {
    test.setTimeout(240_000)
    if (RECORD) {
      await record(browser, lang, locale, email, board, only)
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
        for (const screen of screensFor(only, recorded)) {
          await show(page, screen)
          await expect(page, `${lang} ${screen.name} ${width} ${scheme}`).toHaveScreenshot(
            `${lang}-${screen.name}-${width}-${scheme}.png`,
            { fullPage: true, animations: 'disabled', maxDiffPixels: 50, threshold: 0.05 },
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
  only: readonly string[] | null,
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
  await page.getByLabel(lang.startsWith('ru') ? 'Почта' : 'E-mail').fill(email)
  await page.getByLabel(lang.startsWith('ru') ? 'Пароль' : 'Password').fill('parol12345')
  await page.getByRole('button', { name: lang.startsWith('ru') ? 'Войти' : 'Sign in', exact: true }).click()
  const boardButton = page.getByRole('button', { name: boardName, exact: true })
  await expect(boardButton, 'нет демо — make demo').toBeVisible({ timeout: 15_000 })
  await boardButton.click()
  await page.waitForURL(/\/board\//)
  out.boardId = new URL(page.url()).pathname.split('/')[2]
  // Карточка — первая в первой колонке: у неё в демо обсуждение,
  // история и срок.
  await page.locator('.card').first().getByRole('button').first().click()
  await page.waitForURL(/\/card\//)
  out.cardId = new URL(page.url()).pathname.split('/')[4]

  // Обойти экраны так же, как их обходит сравнение, — на обеих ширинах
  // и темах запросы одни и те же, достаточно одной.
  for (const screen of screensFor(only, out)) await show(page, screen)
  mkdirSync(dirname(fixtures(lang)), { recursive: true })
  writeFileSync(fixtures(lang), JSON.stringify(out))
  await context.close()
}
