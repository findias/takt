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
  // Срок больше обычного: организацию заводит каждая проверка этого
  // файла, идут они разом, а на создании считается хеш пароля —
  // и вчетвером они не укладываются в пять секунд на занятой машине.
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible({ timeout: 20_000 })
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

/**
 * Открытое меню отдаёт фокус первому пункту.
 *
 * Это обещано в самом меню и проверено с подменённой разметкой — но
 * подменённая разметка не знает про верхний слой, и там проверка
 * проходила, пока в настоящем браузере фокус оставался на кнопке:
 * список показывался невидимым один кадр, а невидимое нельзя
 * сфокусировать, и `focus()` молча ничего не делал. Меню открывалось,
 * стрелки не работали, `Enter` закрывал его обратно.
 */
test('открытое меню отдаёт фокус первому пункту', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Меню с клавиатуры')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Добавить карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Добавить', exact: true }).click()

  const card = queue.getByRole('group', { name: /Карточка «Карточка»/ })
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()

  const focused = () =>
    page.evaluate(() => ({
      роль: document.activeElement?.getAttribute('role'),
      имя: document.activeElement?.textContent?.trim(),
    }))
  expect(await focused()).toEqual({ роль: 'menuitem', имя: 'Переименовать' })

  await page.keyboard.press('ArrowDown')
  expect((await focused()).роль).toBe('menuitem')
  expect((await focused()).имя).not.toBe('Переименовать')

  // Escape закрывает и возвращает фокус на кнопку, которая открыла.
  await page.keyboard.press('Escape')
  await expect(page.getByRole('menu')).toHaveCount(0)
  expect(await page.evaluate(() => document.activeElement?.getAttribute('aria-label'))).toMatch(
    /Действия карточки/,
  )
})

/**
 * Панель над доской достижима на всех ширинах.
 *
 * Переключатель видов раньше сжимался вместе со строкой и прятал
 * лишнее под `overflow: hidden`: на 942 за краем оставались
 * «Изменения», на 390 сегмент ужимался до двух пикселей, а «Поток»
 * и «Архив» уходили за окно целиком — с телефона нельзя было открыть
 * ни один вид, кроме текущего.
 *
 * Проверяется координатами и попаданием: обрезанная кнопка остаётся
 * в разметке и «видна» кому угодно, кроме указателя.
 */
test('панель над доской не обрезается ни на одной ширине', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Панель')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Добавить карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Добавить', exact: true }).click()

  for (const width of [1440, 942, 390]) {
    await page.setViewportSize({ width, height: 800 })
    const unreachable = await page.evaluate(() => {
      const out: string[] = []
      for (const el of document.querySelectorAll<HTMLElement>(
        '.board-toolbar button, .board-toolbar select',
      )) {
        const box = el.getBoundingClientRect()
        if (box.width === 0 && box.height === 0) continue
        const name = (el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 24)
        const under = document.elementFromPoint(
          box.left + box.width / 2,
          box.top + box.height / 2,
        )
        const inWindow = box.left >= -0.5 && box.right <= window.innerWidth + 0.5
        if (!inWindow || !(under === el || el.contains(under))) out.push(name)
      }
      return out
    })
    expect(unreachable, `ширина ${width}`).toEqual([])
  }
})

/**
 * Флажок и подпись не сжимаются в тесной строке.
 *
 * Размер флажка задан общим правилом — цель нажатия 24×24 по WCAG
 * 2.5.8, — но заданный размер флексу не указ: в тесной верхней строке
 * карточки (номер, ссылка на родителя, места под метку и исполнителя,
 * меню) флажок ужимался до семнадцати пикселей и меньше, а промах
 * по нему открывает карточку. Тем же способом уезжала под квадрат
 * подпись флажка: «Жёсткий лимит (сначала задайте лимит)» в разметке
 * колонки становилась вдвое выше и вставала под ним, а не рядом.
 *
 * Ни того, ни другого не видно в разметке: там всё правильно.
 */
test('флажок не мельче цели нажатия, подпись не уезжает под него', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Тесная строка')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Добавить карточку' }).click()
  await queue
    .getByPlaceholder('Что нужно сделать?')
    .fill('Собрать отчёт по всему кварталу и году')
  await queue.getByRole('button', { name: 'Добавить', exact: true }).click()

  // Подзадача теснит верхнюю строку: в ней появляется ссылка
  // на родителя, и места флажку остаётся меньше всего. Название
  // родителя длинное намеренно — короткое в строку помещается,
  // и тесно не становится.
  const parent = queue.getByRole('group', { name: /Карточка «Собрать отчёт/ })
  await parent.getByRole('button', { name: /Собрать отчёт/ }).click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel('Название подзадачи').fill('Свести цифры за весь квартал')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await expect(page.getByRole('button', { name: 'Свести цифры за весь квартал' }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть' }).first().click()
  // Ссылка на родителя появляется на карточке из следующего снимка
  // доски: без перезагрузки замеряли бы карточку без неё, а тесно
  // строке становится именно от ссылки.
  await page.reload()
  await expect(queue.getByRole('group', { name: /Свести цифры/ })).toBeVisible()

  const squeezed = await page.evaluate((min) => {
    // Флажки показаны по наведению; замерять надо все, а навести можно
    // на одну карточку — поэтому показываем их правилом.
    const style = document.createElement('style')
    style.textContent = '.card-check{display:inline-block}'
    document.head.append(style)
    const small = [...document.querySelectorAll<HTMLElement>('.card-check')]
      .map((c) => Math.round(c.getBoundingClientRect().width))
      .filter((w) => w < min)
    style.remove()
    return small
  }, 24)
  expect(squeezed, 'флажки выделения мельче цели нажатия').toEqual([])

  // Подпись флажка стоит справа от него, а не под ним.
  await page.getByRole('button', { name: /Разметк/ }).first().click()
  const under = await page.evaluate(() => {
    const out: string[] = []
    for (const label of document.querySelectorAll<HTMLElement>('.column-settings label')) {
      const box = label.querySelector('input[type="checkbox"]')?.getBoundingClientRect()
      const text = label.querySelector('span')?.getBoundingClientRect()
      if (!box || !text) continue
      if (text.left < box.right) out.push(label.textContent?.trim().slice(0, 24) ?? '')
    }
    return out
  })
  expect(under, 'подписи, уехавшие под флажок').toEqual([])
})

/**
 * Одинаковые кнопки разных объектов названы по объекту.
 *
 * «Разметка» стоит в шапке каждой колонки, «Доступ» — в каждой строке
 * списка досок, «закрыть» — у каждой открытой итерации. Глазами их
 * различают по месту, а с диктора все они звучат одинаково, и выбрать
 * нужную нельзя: имя обязано называть то, над чем действие.
 *
 * Список короткий намеренно: это не правило «имена не повторяются» —
 * две кнопки про одну и ту же карточку повторяться могут, — а перечень
 * тех, которые уже повторялись по разным объектам.
 */
const CONTEXTLESS = [
  'Разметка',
  'Добавить карточку',
  'Доступ',
  'закрыть',
  'Убрать',
  'Отозвать',
  'Скопировать',
]

async function contextless(page: Page): Promise<string[]> {
  return page.evaluate((names) => {
    const out: string[] = []
    for (const el of document.querySelectorAll<HTMLElement>('button, a[href]')) {
      const box = el.getBoundingClientRect()
      if (box.width === 0 && box.height === 0) continue
      const name = (el.getAttribute('aria-label') || el.textContent || '').trim()
      if (names.includes(name)) out.push(name)
    }
    return out
  }, CONTEXTLESS)
}

test('кнопки, которых по нескольку, названы по своему объекту', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Имена')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()

  // На доске столько «Разметок» и «Добавить карточку», сколько колонок.
  expect(await contextless(page), 'доска').toEqual([])

  await page.getByRole('button', { name: '+ итерация' }).click()
  await page.getByPlaceholder('Название').fill('Неделя 34')
  await page.getByLabel('Начало').fill('2026-08-10')
  await page.getByLabel('Конец').fill('2026-08-16')
  await page.getByRole('button', { name: 'Создать' }).click()
  await expect(page.getByRole('button', { name: /^Неделя 34 ·/ })).toBeVisible()
  expect(await contextless(page), 'доска с итерацией').toEqual([])

  // В списке досок «Доступ» стоит в каждой строке.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible()
  expect(await contextless(page), 'список досок').toEqual([])

  // На «Структуре» «Убрать» значило и «убрать подразделение», и «вывести
  // человека из состава» — рядом, в одной раскрытой ветке.
  await page.getByRole('button', { name: 'Структура' }).click()
  await page.getByRole('button', { name: 'Новое подразделение' }).click()
  await page.getByPlaceholder('Название').fill('Продажи')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  await page.getByRole('button', { name: '▸ Продажи' }).click()
  await page.getByLabel('Добавить в подразделение').selectOption({ index: 1 })
  await expect(page.getByRole('button', { name: /^Убрать из состава/ })).toBeVisible()
  expect(await contextless(page), 'структура').toEqual([])

  // На «Команде» «Отозвать» стояло у приглашения и у ключа сразу.
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByPlaceholder('Почта коллеги').fill('kolya@example.test')
  await page.getByRole('button', { name: 'Пригласить' }).click()
  await page.getByPlaceholder('Для чего ключ').fill('Обмен')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  await expect(page.getByRole('button', { name: /^Отозвать ключ/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /^Отозвать приглашение/ })).toBeVisible()
  expect(await contextless(page), 'команда').toEqual([])
})
