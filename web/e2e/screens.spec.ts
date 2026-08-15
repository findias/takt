import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Снимки экранов — чтобы смотреть на интерфейс глазами, а не верить,
// что он выглядит как задумано.
//
// Это не проверка: здесь ничего не утверждается, кроме того, что экраны
// открываются. Файлы складываются в screenshots/ и разбираются человеком.
// Запуск: npm run screens.

const SHOTS = 'screenshots'

async function register(page: Page, org: string) {
  const id = Math.random().toString(36).slice(2, 8)
  await page.goto('/')
  await page.getByRole('button', { name: 'Создать новую организацию' }).click()
  await page.getByLabel('Название организации').fill(org)
  await page.getByLabel('Как вас зовут').fill('Мария Кузнецова')
  await page.getByLabel('Почта').fill(`shot-${id}@example.test`)
  await page.getByLabel('Пароль').fill('parol12345')
  await page.getByRole('button', { name: 'Создать организацию' }).click()
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible()
}

async function board(page: Page, name: string) {
  await page.getByPlaceholder('Название новой доски').fill(name)
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

async function addCard(page: Page, column: string, title: string) {
  const section = page.getByRole('region', { name: column })
  await section.getByRole('button', { name: 'Добавить карточку' }).click()
  await section.getByPlaceholder('Что нужно сделать?').fill(title)
  await section.getByRole('button', { name: 'Добавить', exact: true }).click()
  await expect(
    section.getByRole('group', { name: new RegExp(`Карточка «${title}»`) }),
  ).toBeVisible()
}

async function fill(page: Page) {
  await addCard(page, 'Очередь', 'Согласовать смету с подрядчиком')
  await addCard(page, 'Очередь', 'Разобрать обращения за неделю')
  await addCard(page, 'Очередь', 'Обновить регламент приёмки')
  await addCard(page, 'В работе', 'Перенести отчётность на новый склад')
  await addCard(page, 'В работе', 'Договор аренды: продление')
  await addCard(page, 'Готово', 'Инвентаризация по второму цеху')
}

test('снимки экранов', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })

  await page.goto('/')
  await page.screenshot({ path: `${SHOTS}/01-вход.png` })

  await register(page, 'Северный проект')
  await page.screenshot({ path: `${SHOTS}/02-список-досок-пустой.png` })

  await board(page, 'Поставки')
  await page.screenshot({ path: `${SHOTS}/03-доска-пустая.png` })

  await fill(page)
  await page.screenshot({ path: `${SHOTS}/04-доска.png` })

  // Тёмная тема — системная.
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: `${SHOTS}/05-доска-тёмная.png` })
  await page.emulateMedia({ colorScheme: 'light' })

  // Плотный режим — тот самый множитель.
  const denser = page.getByRole('checkbox', { name: 'Плотнее' })
  await denser.check()
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${SHOTS}/06-доска-плотная.png` })
  await denser.uncheck()

  // Панель карточки открывается кнопкой «Открыть» на самой карточке:
  // действия появляются по наведению.
  const card = page.getByRole('group', { name: /Согласовать смету/ })
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.screenshot({ path: `${SHOTS}/07a-меню-карточки.png` })
  await page.keyboard.press('Escape')
  await card.click()
  await page.getByLabel('Название подзадачи').fill('Свести смету с прошлым годом')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/07-панель-сбоку.png` })

  const mode = page.getByLabel('Как показывать панель')
  if (await mode.isVisible()) {
    await mode.selectOption('center')
    await page.waitForTimeout(300)
    await page.screenshot({ path: `${SHOTS}/08-панель-по-центру.png` })
    await mode.selectOption('side')
  }
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // Доступ к доске — прямо с доски.
  await page.getByRole('button', { name: /Видна/ }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/09a-доступ.png` })
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // Поток.
  await page.getByRole('button', { name: 'Поток' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/09-поток.png` })
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // Экран команды и структуры.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await expect(page.getByRole('button', { name: 'Поставки', exact: true })).toBeVisible()
  await page.screenshot({ path: `${SHOTS}/10-список-досок.png` })
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/11-команда.png`, fullPage: true })
  await page.getByRole('button', { name: 'Структура' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/12-структура.png` })

  // Узкий экран.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'Доски' }).click()
  await page.getByRole('button', { name: 'Поставки', exact: true }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/13-узкий-экран.png` })
})
