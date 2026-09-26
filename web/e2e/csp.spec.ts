import { expect, test } from '@playwright/test'

// Политика содержимого не нарушается (PROMPT-TESTING.md, уровень 8).
//
// Разбор ZAP 26.09.2026 нашёл, что style-src разрешала встроенные стили
// целиком: 'unsafe-inline' «ради атрибутов style». Политика сужена до
// 'self' без исключений, и нарушение теперь не ломает сервер, а молча
// лишает экран стилей: браузер пишет его в консоль и ничего не говорит. Эта проверка слушает консоль на основных экранах
// и при переносе карточки мышью — там библиотека перетаскивания рисует
// превью, — и не терпит ни одного нарушения.
test('политика содержимого не нарушается на основных экранах и при переносе', async ({ page }) => {
  const violations: string[] = []
  page.on('console', (m) => {
    if (/Content Security Policy|Refused to (apply|load|execute)/i.test(m.text())) violations.push(m.text())
  })

  await page.goto('/')
  await page.getByLabel('Почта').fill('anna@example.test')
  await page.getByLabel('Пароль').fill('parol12345')
  await page.getByRole('button', { name: 'Войти' }).click()

  const board = page.getByRole('button', { name: 'Поставки', exact: true })
  await expect(board, 'нет демо — make demo').toBeVisible({ timeout: 10_000 })
  await board.click()

  const queue = page.getByRole('region', { name: 'Очередь' })
  const work = page.getByRole('region', { name: 'В работе' })
  const card = queue.getByRole('group').first()
  const from = await card.boundingBox()
  const to = await work.boundingBox()
  if (!from || !to) throw new Error('не видно ни карточки, ни колонки')
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  for (let i = 1; i <= 10; i++) {
    await page.mouse.move(
      from.x + ((to.x + to.width / 2 - from.x) * i) / 10,
      from.y + ((to.y + 100 - from.y) * i) / 10,
      { steps: 2 },
    )
  }
  // Отпускаем над исходной колонкой: превью нарисовано, а данные стенда
  // остаются как были.
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2, { steps: 5 })
  await page.mouse.up()

  await card.getByRole('button').first().click()
  await expect(page.getByRole('tab').first()).toBeVisible()
  await page.keyboard.press('Escape')

  for (const path of ['/tasks', '/reports', '/team', '/structure']) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
  }
  // Справка несёт свои стили внутри страницы и живёт по своей политике:
  // без неё открывалась без оформления (26.09.2026).
  for (const path of ['/help/ru/howto', '/help/en/whats-new', '/help/ru/search?q=доска']) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
  }
  await page.goto('/api/v1/docs')
  await page.waitForLoadState('networkidle')

  expect(violations, 'нарушения политики содержимого').toEqual([])
})
