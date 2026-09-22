import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Сценарии против выложенного тестового стенда (playwright.stand.config.ts).
//
// Данные — демонстрационные, как у локального стенда (`takt demo`), и
// люди на стенде общие с теми, кто проверяет его руками. Поэтому язык
// учётной записи перед русскими проверками ставится явно: человек мог
// переключить его на английский, и русские подписи бы не нашлись.

const PASSWORD = 'parol12345'

async function signIn(page: Page, who: string, lang: 'ru' | 'en' = 'ru') {
  await page.goto('/')
  // Подписи — на языке, который выберет экран входа: он смотрит
  // на браузер и cookie, а не на учётную запись.
  await page.getByLabel(/^(Почта|E-mail)$/).fill(`${who}@example.test`)
  await page.getByLabel(/^(Пароль|Password)$/).fill(PASSWORD)
  await page.getByRole('button', { name: /^(Войти|Sign in)$/ }).click()
  await expect(page.getByRole('button', { name: /^(Личные настройки|Personal settings)/ })).toBeVisible()
  // Язык учётной записи — тот, что нужен сценарию. Если он сменился,
  // клиент перезагрузит страницу сам; ждём уже нужных подписей.
  const status = await page.evaluate(async (l) => {
    const r = await fetch('/api/me/lang', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch' },
      body: JSON.stringify({ lang: l }),
    })
    return r.status
  }, lang)
  expect(status, 'язык учётной записи не сохранился').toBe(204)
  await page.reload()
  const settings = lang === 'ru' ? 'Личные настройки' : 'Personal settings'
  await expect(page.getByRole('button', { name: new RegExp(`^${settings}`) })).toBeVisible()
}

test('полоса стенда называет ветку, заметка — коммиты и как войти', async ({ page }) => {
  await page.goto('/')
  const bar = page.locator('.stand-bar')
  await expect(bar).toContainText('Тестовый стенд: ветка')
  if (process.env.STAND_VERSION) await expect(bar).toContainText(process.env.STAND_VERSION)

  await page.getByRole('button', { name: 'Что на стенде' }).click()
  const note = page.getByRole('dialog', { name: 'Что на стенде' })
  await expect(note).toContainText('anna@example.test')
  await expect(note).toContainText(PASSWORD)
  await page.keyboard.press('Escape')
  await expect(note).toBeHidden()
})

test('владелец видит доску с карточками, карточку и «Поток»', async ({ page }) => {
  await signIn(page, 'anna')
  await page.getByRole('button', { name: 'Поставки', exact: true }).click()
  for (const column of ['Очередь', 'В работе', 'Готово']) {
    await expect(page.getByRole('region', { name: column })).toBeVisible()
  }
  // Доска не пустая: пустой экран вместо доски — ровно та поломка,
  // которую глазами принимают за «данных нет».
  const card = page.getByRole('group', { name: /Выпустить релиз склада/ })
  await expect(card).toBeVisible()

  await card.click()
  await expect(page.getByRole('tab', { name: 'Работа' })).toBeVisible()
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: 'Поток' }).click()
  await expect(page.getByRole('heading', { name: /Поток/ }).first()).toBeVisible()
})

test('личные настройки открываются за именем и закрываются Escape', async ({ page }) => {
  await signIn(page, 'boris')
  const name = page.getByRole('button', { name: /^Личные настройки/ })
  await name.click()
  const dialog = page.getByRole('dialog', { name: 'Личные настройки' })
  for (const tab of ['Язык', 'Оформление', 'Вход']) {
    await expect(dialog.getByRole('tab', { name: tab })).toBeVisible()
  }
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(name).toBeFocused()
})

test('наблюдатель смотрит, но не заводит', async ({ page }) => {
  await signIn(page, 'gleb')
  await expect(page.getByRole('button', { name: 'Поставки', exact: true })).toBeVisible()
  await expect(page.getByPlaceholder('Название новой доски')).toHaveCount(0)
})

test('участник заводит доску и карточку, а доска уходит в архив', async ({ page }) => {
  await signIn(page, 'vera')
  const name = `Автотест ${Date.now().toString(36)}`
  await page.getByPlaceholder('Название новой доски').fill(name)
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await expect(queue).toBeVisible()

  await queue.getByRole('button', { name: /Завести карточку/ }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Проверить стенд')
  await page.keyboard.press('Enter')
  await expect(queue.getByRole('group', { name: /Проверить стенд/ })).toBeVisible()

  // Своё — убрать: стенд общий, и доски автотестов копились бы в списке.
  await page.getByRole('button', { name: 'Все доски' }).click()
  const archive = page.getByRole('button', { name: `Убрать доску «${name}» в архив` })
  await archive.click()
  // Доска остаётся видна в разделе «Архив» — так и задумано, — а вот
  // среди рабочих её больше нет, и убирать её второй раз нечем.
  await expect(archive).toHaveCount(0)
})

test('по-английски: экран и заметка стенда на английском', async ({ page }) => {
  await signIn(page, 'vera', 'en')
  await expect(page.getByPlaceholder('New board name')).toBeVisible()
  await page.getByRole('button', { name: 'What is on the stand' }).click()
  await expect(page.getByRole('dialog', { name: 'What is on the stand' })).toContainText(
    'anna@example.test',
  )
  await page.keyboard.press('Escape')
  // Вернуть русский: вера — общий человек стенда, и следующий, кто
  // войдёт под ней руками, ждёт привычного экрана.
  const back = await page.evaluate(async () => {
    const r = await fetch('/api/me/lang', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch' },
      body: JSON.stringify({ lang: 'ru' }),
    })
    return r.status
  })
  expect(back).toBe(204)
})
