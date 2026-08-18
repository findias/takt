import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Снимки экранов — чтобы смотреть на интерфейс глазами, а не верить,
// что он выглядит как задумано.
//
// Это не проверка: здесь ничего не утверждается, кроме того, что экраны
// открываются. Файлы складываются в screenshots/ и разбираются человеком.
//
// Снимается демонстрационный стенд, а не наспех заведённые карточки.
// Раньше сценарий заводил себе шесть пустых карточек — и снимки врали
// в лучшую сторону: ни длинного названия, ни двух исполнителей рядом,
// ни блокировки с причиной, ни метрик, которым есть что показать.
// Почти всякая ошибка вёрстки видна только на настоящих данных.
//
// Запуск: make screens (наполнит базу и снимет) либо npm run screens,
// если данные уже залиты через `board demo`.

const SHOTS = 'screenshots'
const OWNER = 'anna@example.test'
const PASSWORD = 'parol12345'

async function signIn(page: Page) {
  await page.goto('/')
  await page.getByLabel('Почта').fill(OWNER)
  await page.getByLabel('Пароль').fill(PASSWORD)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByRole('button', { name: 'Поставки', exact: true })).toBeVisible({
    timeout: 10_000,
  })
}

async function openBoard(page: Page, name: string) {
  await page.getByRole('button', { name, exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

// Панели закрываются возвратом на адрес доски, а не кнопкой: кнопок
// «Закрыть» на экране бывает несколько, а промахнувшийся снимок
// замечаешь через двадцать файлов.
async function backToBoard(page: Page, url: string) {
  await page.goto(url)
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

test('снимки экранов', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })

  await page.goto('/')
  await page.screenshot({ path: `${SHOTS}/01-вход.png` })

  await signIn(page)
  await page.screenshot({ path: `${SHOTS}/02-список-досок.png`, fullPage: true })

  await openBoard(page, 'Поставки')
  const boardUrl = page.url()
  await page.screenshot({ path: `${SHOTS}/03-доска.png` })

  // Тёмная тема — системная.
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: `${SHOTS}/04-доска-тёмная.png` })
  await page.emulateMedia({ colorScheme: 'light' })

  // Плотный режим — тот самый множитель.
  const denser = page.getByRole('checkbox', { name: 'Плотнее' })
  await denser.check()
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${SHOTS}/05-доска-плотная.png` })
  await denser.uncheck()

  // Разбиение работы раскрывается прямо с доски.
  const parent = page.getByRole('group', { name: /Выпустить релиз склада/ })
  await parent.getByRole('button', { name: /Подзадачи/ }).click()
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${SHOTS}/06-доска-с-подзадачами.png` })
  await parent.getByRole('button', { name: /Подзадачи/ }).click()

  // Меню карточки: действия появляются по наведению.
  await parent.hover()
  await parent.getByRole('button', { name: /Действия карточки/ }).click()
  await page.screenshot({ path: `${SHOTS}/07-меню-карточки.png` })
  await page.keyboard.press('Escape')

  // Правка по самому полю: исполнители правятся нажатием по стопке,
  // а не походом в меню «…».
  await parent.hover()
  await parent.getByRole('button', { name: /Исполнител/ }).click()
  await page.screenshot({ path: `${SHOTS}/07б-исполнители-на-карточке.png` })
  await page.keyboard.press('Escape')

  // То же меню у нижней карточки колонки. Снимок нужен отдельным: список
  // рисуется в верхнем слое ради этого случая, а на высоком экране
  // случай не наступает — колонка кончается далеко от края окна.
  await page.setViewportSize({ width: 942, height: 700 })
  const lowest = page.locator('.cards .card').last()
  await lowest.hover()
  await lowest.getByRole('button', { name: /Действия карточки/ }).click()
  await page.screenshot({ path: `${SHOTS}/07в-меню-у-нижнего-края.png` })
  await page.keyboard.press('Escape')
  await page.setViewportSize({ width: 1440, height: 900 })

  // Выделение и полоса действий над многими сразу.
  await parent.hover()
  await parent.getByRole('checkbox').check()
  const neighbour = page.getByRole('group', { name: /Разобрать обращения/ })
  await neighbour.hover()
  await neighbour.getByRole('checkbox').check()
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${SHOTS}/07г-выделение-и-массовые-действия.png` })
  await page.getByRole('button', { name: 'Снять выделение' }).click()

  // Карточка тремя вкладками. Открывается обсуждением.
  await parent.click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/08-карточка-обсуждение.png` })
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/09-карточка-работа.png` })
  await page.getByRole('tab', { name: 'История' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/10-карточка-история.png` })

  const mode = page.getByLabel('Как показывать панель')
  if (await mode.isVisible()) {
    await mode.selectOption('center')
    await page.waitForTimeout(300)
    await page.screenshot({ path: `${SHOTS}/11-панель-по-центру.png` })
    await mode.selectOption('side')
  }
  await backToBoard(page, boardUrl)

  // Таблица — тот же набор данных плоским списком.
  await page.getByRole('button', { name: 'Таблица' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/12-таблица.png` })
  await page.getByRole('button', { name: 'Изменения' }).click()
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${SHOTS}/13-изменения.png` })
  await page.getByRole('button', { name: 'Доска' }).click()

  // Поток: обещание, время цикла, возраст работы, пропускная способность.
  await page.getByRole('button', { name: 'Поток' }).click()
  await page.waitForTimeout(600)
  await page.screenshot({ path: `${SHOTS}/14-поток.png`, fullPage: true })
  await backToBoard(page, boardUrl)

  // Архив карточек.
  await page.getByRole('button', { name: 'Архив' }).click()
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${SHOTS}/15-архив-карточек.png` })
  await backToBoard(page, boardUrl)

  // Отчёт по закрытой итерации.
  await page.getByRole('button', { name: 'Неделя 32', exact: true }).click()
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${SHOTS}/16-отчёт-итерации.png` })
  await backToBoard(page, boardUrl)

  // Доступ к доске — прямо из её шапки.
  await page.getByRole('button', { name: /Видна/ }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/17-доступ.png` })
  await backToBoard(page, boardUrl)

  // Поиск и команды одним списком.
  await page.getByRole('button', { name: /Найти/ }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/18-поиск.png` })
  await page.keyboard.press('Escape')

  // Экраны организации.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/19-команда.png`, fullPage: true })

  // Журнал доставок подписки: ради него к подпискам и возвращаются.
  await page.getByRole('button', { name: 'Доставки' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/19б-подписки.png`, fullPage: true })
  await page.getByRole('button', { name: 'Структура' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/20-структура.png`, fullPage: true })

  // Раскрытый узел: кто здесь и чем занят.
  await page.getByRole('button', { name: /Продажи/ }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/20б-узел-структуры.png`, fullPage: true })

  // Узкий экран: колонки не помещаются рядом, показывается одна.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'Доски' }).click()
  await openBoard(page, 'Поставки')
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/21-узкий-экран.png` })
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: `${SHOTS}/22-узкий-экран-тёмный.png` })
})
