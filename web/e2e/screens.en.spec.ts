import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Английские снимки (ROADMAP 30.5): те экраны, что показывают README
// и английская документация, — на английском интерфейсе и английских
// данных. Русские снимает screens.spec.ts; здесь только то, на что
// ссылается документация, — шесть экранов, не весь набор.
//
// Данные — английская организация стенда (`takt demo` заводит её рядом
// с русской): люди свои, anna@en.example.test, пароль тот же. Браузер —
// с английским языком системы, язык у человека не выбран, так что
// экран английский по браузеру, как у настоящего посетителя.

const SHOTS = 'screenshots/en'

// Язык системы — тоже английский: пустое поле даты Chrome рисует сам,
// на языке системы, а не страницы, и на русской машине посреди
// английского снимка стояло бы «дд.мм.гггг».
test.use({
  locale: 'en-GB',
  launchOptions: { env: { ...process.env, LANG: 'en_GB.UTF-8', LANGUAGE: 'en_GB' }, args: ['--lang=en-GB'] },
})

async function onBoard(page: Page) {
  await expect(page.getByRole('region', { name: 'Queue' })).toBeVisible()
}

test('английские снимки для документации', async ({ page }) => {
  test.setTimeout(90_000)
  await page.setViewportSize({ width: 1440, height: 900 })

  await page.goto('/')
  await page.getByLabel('E-mail').fill('anna@en.example.test')
  await page.getByLabel('Password').fill('parol12345')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  const board = page.getByRole('button', { name: 'Supplies', exact: true })
  await expect(board).toBeVisible({ timeout: 10_000 })
  await page.screenshot({ path: `${SHOTS}/02-boards.png`, fullPage: true })

  await board.click()
  await onBoard(page)
  const boardUrl = page.url()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/03-board.png` })

  await page.getByRole('group', { name: /Ship the warehouse release/ }).click()
  await page.getByRole('tab', { name: 'Work' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/09-card-work.png` })
  await page.goto(boardUrl)
  await onBoard(page)

  await page.getByRole('button', { name: 'Table' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/12-table.png` })
  await page.getByRole('button', { name: 'Board', exact: true }).click()

  await page.getByRole('button', { name: 'Flow' }).click()
  await page.waitForTimeout(600)
  await page.screenshot({ path: `${SHOTS}/14-flow.png`, fullPage: true })
  await page.goto(boardUrl)
  await onBoard(page)

  await page.getByRole('button', { name: 'All boards' }).click()
  await page.getByRole('button', { name: 'Team', exact: true }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/19-team.png`, fullPage: true })
})
