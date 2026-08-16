import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Доступность, которую нельзя проверить глазами.
//
// Размер цели нажатия и наличие имени у элемента управления — это то,
// что видно только измерением: на снимке экрана мелкая кнопка выглядит
// аккуратной, а флажок без подписи — обычным флажком. Проверяется
// на настоящих экранах, потому что размеры зависят от раскладки,
// а не от компонента в отдельности.

/** WCAG 2.5.8 (AA): цель нажатия не меньше 24×24 CSS-пикселей. */
const TARGET = 24

type Small = { name: string; width: number; height: number }

async function tinyTargets(page: Page): Promise<Small[]> {
  return page.evaluate((min) => {
    const out: { name: string; width: number; height: number }[] = []
    const nodes = document.querySelectorAll<HTMLElement>(
      'button, a[href], select, input[type="checkbox"], input[type="radio"], [tabindex="0"]',
    )
    for (const el of nodes) {
      const box = el.getBoundingClientRect()
      if (box.width === 0 && box.height === 0) continue // спрятан
      if (box.width >= min && box.height >= min) continue
      // Исключение самого критерия: ссылка внутри строки текста
      // размером не управляет.
      const parent = el.parentElement
      const inline = parent && getComputedStyle(parent).display === 'inline'
      if (inline) continue
      out.push({
        name: (el.getAttribute('aria-label') || el.textContent || el.tagName).trim().slice(0, 40),
        width: Math.round(box.width),
        height: Math.round(box.height),
      })
    }
    return out
  }, TARGET)
}

async function namelessControls(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const out: string[] = []
    const nodes = document.querySelectorAll<HTMLElement>(
      'button, select, input:not([type="hidden"]), textarea',
    )
    for (const el of nodes) {
      const box = el.getBoundingClientRect()
      if (box.width === 0 && box.height === 0) continue
      const labelled =
        el.getAttribute('aria-label') ||
        el.getAttribute('title') ||
        (el.id && document.querySelector(`label[for="${el.id}"]`)) ||
        el.closest('label') ||
        (el instanceof HTMLInputElement && el.placeholder) ||
        el.textContent?.trim()
      if (!labelled) out.push(`${el.tagName.toLowerCase()}.${el.className || 'без класса'}`)
    }
    return out
  })
}

async function register(page: Page) {
  const id = Math.random().toString(36).slice(2, 8)
  await page.goto('/')
  await page.getByRole('button', { name: 'Создать новую организацию' }).click()
  await page.getByLabel('Название организации').fill('Проверка доступности')
  await page.getByLabel('Как вас зовут').fill('Проверяющий')
  await page.getByLabel('Почта').fill(`a11y-${id}@example.test`)
  await page.getByLabel('Пароль').fill('parol12345')
  await page.getByRole('button', { name: 'Создать организацию' }).click()
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible()
}

test('на всех экранах цели нажатия не мельче 24 пикселей', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })

  await page.goto('/')
  expect(await tinyTargets(page), 'экран входа').toEqual([])

  await register(page)
  expect(await tinyTargets(page), 'список досок').toEqual([])

  await page.getByPlaceholder('Название новой доски').fill('Доступность')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Добавить карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Добавить', exact: true }).click()

  // Действия карточки появляются по наведению — до этого их не измерить.
  await queue.getByRole('group', { name: /Карточка «Карточка»/ }).hover()
  expect(await tinyTargets(page), 'доска').toEqual([])

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await expect(page.getByRole('heading', { name: 'В организации' })).toBeVisible()
  expect(await tinyTargets(page), 'команда').toEqual([])
})

test('у каждого элемента управления есть имя', async ({ page }) => {
  await page.goto('/')
  expect(await namelessControls(page), 'экран входа').toEqual([])

  await register(page)
  expect(await namelessControls(page), 'список досок').toEqual([])

  await page.getByRole('button', { name: 'Команда' }).click()
  await expect(page.getByRole('heading', { name: 'В организации' })).toBeVisible()
  expect(await namelessControls(page), 'команда').toEqual([])
})
