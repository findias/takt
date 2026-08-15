import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Сквозной путь: от пустого браузера до переставленной карточки.
//
// Проверяется не поведение отдельной части, а то, что части сходятся:
// клиент из образа, сервер из того же образа и настоящая база с её
// политиками. Каждая часть проверена по отдельности — здесь проверяется
// стык, на котором уже ломались.
//
// У доски есть свой адрес, и это проверяется здесь же: перезагрузка
// оставляет открытым то, что было открыто, а ссылка на карточку
// открывает карточку. До появления маршрутов приложение жило по «/»,
// и прислать коллеге ссылку было нельзя.

type Newcomer = { email: string; password: string; org: string }

/** Каждый сценарий работает в своей организации: чужого состояния нет. */
function newcomer(): Newcomer {
  const id = Math.random().toString(36).slice(2, 10)
  return { email: `e2e-${id}@example.test`, password: 'parol12345', org: `Команда ${id}` }
}

async function register(page: Page): Promise<Newcomer> {
  const who = newcomer()
  await page.goto('/')
  await page.getByRole('button', { name: 'Создать новую организацию' }).click()
  await page.getByLabel('Название организации').fill(who.org)
  await page.getByLabel('Как вас зовут').fill('Проверяющий')
  await page.getByLabel('Почта').fill(who.email)
  await page.getByLabel('Пароль').fill(who.password)
  await page.getByRole('button', { name: 'Создать организацию' }).click()
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible()
  return who
}

async function signIn(page: Page, who: Newcomer) {
  await page.goto('/')
  await page.getByLabel('Почта').fill(who.email)
  await page.getByLabel('Пароль').fill(who.password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible()
}

async function createBoard(page: Page, name: string) {
  await page.getByPlaceholder('Название новой доски').fill(name)
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

async function openBoard(page: Page, name: string) {
  await page.getByRole('button', { name, exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

async function addCard(page: Page, column: string, title: string) {
  const section = page.getByRole('region', { name: column })
  await section.getByRole('button', { name: 'Добавить карточку' }).click()
  await section.getByPlaceholder('Что нужно сделать?').fill(title)
  await section.getByRole('button', { name: 'Добавить', exact: true }).click()
  await expect(cardIn(page, column, title)).toBeVisible()
}

/**
 * Ждать подтверждения сервером.
 *
 * Перемещение применяется мгновенно, а подтверждение приходит потом —
 * ровно ради этого всё и сделано. Значит, перезагружать страницу сразу
 * после переноса бессмысленно: проверялось бы, успел ли уйти запрос,
 * а не сохранилось ли изменение.
 *
 * Ждать исчезновения пометки «сохраняем…» нельзя: её ещё может не быть,
 * и проверка пройдёт, ничего не дождавшись. Ждём того, что растёт только
 * от подтверждённой операции, — версии доски.
 */
async function boardVersion(page: Page) {
  return ((await page.locator('.version').textContent()) ?? '').trim()
}

async function savedSince(page: Page, before: string) {
  await expect(page.locator('.version')).not.toHaveText(before)
  await expect(page.locator('.pending')).toHaveCount(0)
}

function cardIn(page: Page, column: string, title: string) {
  return page
    .getByRole('region', { name: column })
    .getByRole('group', { name: new RegExp(`Карточка «${title}»`) })
}

test('от входа до переставленной карточки', async ({ page }) => {
  const who = await register(page)

  await createBoard(page, 'Первая доска')
  await addCard(page, 'Очередь', 'Собрать требования')

  // Перенос с клавиатуры — тот самый путь, который обязан существовать
  // по WCAG 2.5.7 наравне с перетаскиванием.
  const beforeMove = await boardVersion(page)
  await cardIn(page, 'Очередь', 'Собрать требования').focus()
  await page.keyboard.press('Control+ArrowRight')
  await expect(cardIn(page, 'В работе', 'Собрать требования')).toBeVisible()
  await savedSince(page, beforeMove)

  // Перезагрузка отвечает на два вопроса сразу: доехало ли изменение
  // до базы и остались ли мы там, где были.
  const boardUrl = page.url()
  await page.reload()
  await expect(cardIn(page, 'В работе', 'Собрать требования')).toBeVisible()
  expect(page.url()).toBe(boardUrl)

  // И переживает выход с повторным входом — то есть лежит не в браузере.
  // Выход живёт в шапке списка досок: на самой доске в шапке только доска.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Выйти' }).click()
  await signIn(page, who)
  await openBoard(page, 'Первая доска')
  await expect(cardIn(page, 'В работе', 'Собрать требования')).toBeVisible()
})

test('перетаскивание мышью переносит карточку', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с перетаскиванием')
  await addCard(page, 'Очередь', 'Перетащить меня')

  const card = cardIn(page, 'Очередь', 'Перетащить меня')
  const target = page.getByRole('region', { name: 'В работе' })

  const beforeMove = await boardVersion(page)
  const from = await card.boundingBox()
  const to = await target.boundingBox()
  if (!from || !to) throw new Error('не видно ни карточки, ни колонки')

  // Настоящие движения указателя, а не «щёлкнуть и отпустить»:
  // библиотека перетаскивания начинает работу только после заметного
  // смещения, и одиночный прыжок она не замечает.
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  for (let i = 1; i <= 10; i++) {
    await page.mouse.move(
      from.x + ((to.x + to.width / 2 - from.x) * i) / 10,
      from.y + ((to.y + 100 - from.y) * i) / 10,
      { steps: 2 },
    )
  }
  await page.mouse.up()

  await expect(cardIn(page, 'В работе', 'Перетащить меня')).toBeVisible()
  await savedSince(page, beforeMove)
  await page.reload()
  await expect(cardIn(page, 'В работе', 'Перетащить меня')).toBeVisible()
})

// Человек переносит карточку и тут же жмёт F5 — привычка, а не редкость.
// Браузер отменяет незавершённые запросы вместе со страницей, и без
// keepalive перенос терялся: карточка возвращалась на прежнее место,
// хотя человек видел её перенесённой. Проверка нашла это и охраняет.
test('перенос не теряется, если сразу перезагрузить страницу', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с нетерпеливым')
  await addCard(page, 'Очередь', 'Успеть до перезагрузки')

  await cardIn(page, 'Очередь', 'Успеть до перезагрузки').focus()
  await page.keyboard.press('Control+ArrowRight')
  await expect(cardIn(page, 'В работе', 'Успеть до перезагрузки')).toBeVisible()

  // Никакого ожидания подтверждения: в этом и суть.
  //
  // Перезагрузка может застать сервер в момент, когда операция ещё
  // в пути, — и тогда снимок придёт старым. Это не потеря: страница,
  // открытая заново, покажет уже применённое. Поэтому проверка
  // повторяет перезагрузку, а не ждёт одного удачного попадания:
  // утверждение здесь — «изменение доехало до базы», а не «доехало
  // за такое-то время».
  await expect(async () => {
    await page.reload()
    await expect(cardIn(page, 'В работе', 'Успеть до перезагрузки')).toBeVisible({
      timeout: 2_000,
    })
  }).toPass({ timeout: 20_000 })
})

test('изменение доезжает до второй открытой доски', async ({ page, browser }) => {
  // Поток изменений проверен и на сервере, и в хуке — но только здесь
  // видно, что он вообще доходит до соседнего браузера: между ними
  // прокси, заголовки и буферизация, и каждое из этого уже ломало поток.
  const who = await register(page)
  await createBoard(page, 'Общая доска')

  const second = await browser.newContext()
  const watcher = await second.newPage()
  await signIn(watcher, who)
  await openBoard(watcher, 'Общая доска')

  // Считаем, чем именно догоняет сосед. Раньше на каждое чужое изменение
  // он перечитывал доску целиком — на трёхстах карточках это заметно,
  // и заметно тем сильнее, чем больше людей работает.
  let snapshots = 0
  let catchups = 0
  watcher.on('response', (r) => {
    const path = r.url().split('/api')[1] ?? ''
    if (/^\/boards\/[0-9a-f-]+$/.test(path)) snapshots++
    if (path.includes('/changes?')) catchups++
  })

  await addCard(page, 'Очередь', 'Появись у соседа')

  await expect(cardIn(watcher, 'Очередь', 'Появись у соседа')).toBeVisible({ timeout: 15_000 })
  expect(catchups, 'сосед догнал патчем').toBeGreaterThan(0)
  expect(snapshots, 'снимок доски перезапрашивать не пришлось').toBe(0)
  await second.close()
})

test('ссылка открывает доску и карточку, чужая — не открывает ничего', async ({ page, browser }) => {
  await register(page)
  await createBoard(page, 'Доска со ссылкой')
  await addCard(page, 'Очередь', 'Прислать коллеге')

  // Карточка открывается — и адрес меняется вместе с ней.
  // Действия карточки живут в меню: три подписи в ряд не помещались
  // в ширину колонки и обрезались.
  await cardIn(page, 'Очередь', 'Прислать коллеге').hover()
  await cardIn(page, 'Очередь', 'Прислать коллеге')
    .getByRole('button', { name: /Действия карточки/ })
    .click()
  await page.getByRole('menuitem', { name: 'Открыть' }).click()
  await expect(page.getByRole('heading', { name: 'Прислать коллеге' })).toBeVisible()
  const cardUrl = page.url()
  expect(cardUrl).toMatch(/\/board\/[0-9a-f-]+\/card\/[0-9a-f-]+$/)

  // Та же ссылка в новой вкладке открывает ту же карточку.
  const again = await browser.newContext({ storageState: undefined })
  await again.close()
  await page.goto('/')
  await page.goto(cardUrl)
  await expect(page.getByRole('heading', { name: 'Прислать коллеге' })).toBeVisible()

  // А посторонний по той же ссылке не видит ни доски, ни карточки:
  // проверка живёт здесь, а не только в тестах API, потому что раньше
  // мы верили, что клиент покажет отказ, а не пустой экран.
  const stranger = await browser.newContext()
  const strangerPage = await stranger.newPage()
  await register(strangerPage)
  await strangerPage.goto(cardUrl)
  await expect(strangerPage.getByText(/не найдена|Не удалось/)).toBeVisible()
  await expect(strangerPage.getByRole('group', { name: /Карточка/ })).toHaveCount(0)
  await stranger.close()
})

test('карточка получает исполнителя, и это видно', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с исполнителем')
  await addCard(page, 'Очередь', 'Кому-то делать')

  const beforeAssign = await boardVersion(page)
  const card = cardIn(page, 'Очередь', 'Кому-то делать')
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: /Назначить: / }).first().click()

  // Инициалы — подпись под работой: доска отвечает на вопрос «кто это
  // делает», ради которого её и открывают.
  const avatar = card.locator('.avatar')
  await expect(avatar).toBeVisible()
  const initials = await avatar.textContent()
  expect(initials?.trim().length).toBeGreaterThan(0)

  // Перед перезагрузкой ждём подтверждения: назначение применяется
  // мгновенно, а уходит следом, и перезагружаться, не дождавшись,
  // значит проверять скорость сети, а не сохранность данных.
  await savedSince(page, beforeAssign)
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'Кому-то делать').locator('.avatar')).toHaveText(
    initials!.trim(),
    { timeout: 10_000 },
  )

  // Снять исполнителя можно тем же меню.
  await cardIn(page, 'Очередь', 'Кому-то делать').hover()
  await cardIn(page, 'Очередь', 'Кому-то делать')
    .getByRole('button', { name: /Действия карточки/ })
    .click()
  await page.getByRole('menuitem', { name: 'Снять исполнителя' }).click()
  await expect(cardIn(page, 'Очередь', 'Кому-то делать').locator('.avatar')).toHaveCount(0)
})

test('метка заводится в организации и вешается на карточку', async ({ page }) => {
  await register(page)

  // Метки живут в организации: одинаково названная метка на двух досках
  // это одна метка, иначе фильтр собирать не из чего.
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByPlaceholder('Название метки').fill('Срочно')
  await page.getByRole('button', { name: 'Завести метку' }).click()
  await expect(page.getByText('Срочно')).toBeVisible()

  await page.getByRole('button', { name: 'Доски' }).click()
  await createBoard(page, 'Доска с метками')
  await addCard(page, 'Очередь', 'Пометить меня')

  const beforeLabel = await boardVersion(page)
  const card = cardIn(page, 'Очередь', 'Пометить меня')
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Метка «Срочно»' }).click()

  await expect(card.getByText('Срочно')).toBeVisible()

  // Переживает перезагрузку — это данные, а не украшение экрана.
  // Ждём подтверждения: метка вешается мгновенно, а уходит следом.
  await savedSince(page, beforeLabel)
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'Пометить меня').getByText('Срочно')).toBeVisible({
    timeout: 10_000,
  })

  // И снимается тем же меню.
  await cardIn(page, 'Очередь', 'Пометить меня').hover()
  await cardIn(page, 'Очередь', 'Пометить меня')
    .getByRole('button', { name: /Действия карточки/ })
    .click()
  await page.getByRole('menuitem', { name: 'Снять метку «Срочно»' }).click()
  await expect(cardIn(page, 'Очередь', 'Пометить меня').getByText('Срочно')).toHaveCount(0)
})

test('фильтр прячет лишнее и живёт в адресе', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с фильтром')
  await addCard(page, 'Очередь', 'Согласовать смету')
  await addCard(page, 'Очередь', 'Разобрать обращения')
  await addCard(page, 'В работе', 'Договор аренды')

  // Поиск: показывает найденное и говорит, сколько скрыл, — иначе доска
  // выглядит опустевшей без объяснения.
  await page.getByRole('searchbox', { name: 'Найти карточку' }).fill('договор')
  await expect(cardIn(page, 'В работе', 'Договор аренды')).toBeVisible()
  await expect(page.getByRole('group', { name: /Согласовать смету/ })).toHaveCount(0)
  await expect(page.getByText(/скрыто 2/)).toBeVisible()

  // Фильтр — состояние адреса: ссылку на отфильтрованный вид можно
  // прислать, и она переживает перезагрузку.
  expect(page.url()).toContain('q=')
  const filtered = page.url()
  await page.reload()
  await expect(cardIn(page, 'В работе', 'Договор аренды')).toBeVisible()
  await expect(page.getByRole('group', { name: /Согласовать смету/ })).toHaveCount(0)
  expect(page.url()).toBe(filtered)

  // «Показать все» возвращает доску целиком и чистит адрес.
  await page.getByRole('button', { name: 'Показать все' }).click()
  await expect(cardIn(page, 'Очередь', 'Согласовать смету')).toBeVisible()
  expect(page.url()).not.toContain('q=')
})

test('фильтр по исполнителю показывает и то, что ни на ком', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с исполнителями')
  await addCard(page, 'Очередь', 'Моя работа')
  await addCard(page, 'Очередь', 'Ничья работа')

  const mine = cardIn(page, 'Очередь', 'Моя работа')
  await mine.hover()
  await mine.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: /Назначить: / }).first().click()
  await expect(mine.locator('.avatar')).toBeVisible()

  // Работа без исполнителя и есть то, что теряется: её надо уметь
  // спросить отдельно.
  await page.getByLabel('Исполнитель').selectOption('none')
  await expect(cardIn(page, 'Очередь', 'Ничья работа')).toBeVisible()
  await expect(page.getByRole('group', { name: /Моя работа/ })).toHaveCount(0)
})

test('группировка раскладывает доску по дорожкам и живёт в адресе', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с дорожками')
  await addCard(page, 'Очередь', 'Моя работа')
  await addCard(page, 'Очередь', 'Ничья работа')

  const mine = cardIn(page, 'Очередь', 'Моя работа')
  await mine.hover()
  await mine.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: /Назначить: / }).first().click()
  await expect(mine.locator('.avatar')).toBeVisible()

  await page.getByLabel('Группировка').selectOption('assignee')

  // Дорожек две: своя и «ни на ком» — именно там теряется работа,
  // поэтому она остаётся видимой всегда.
  await expect(page.getByRole('heading', { name: 'Ни на ком' })).toBeVisible()
  await expect(page.locator('.swimlane')).toHaveCount(2)

  // Группировка — состояние адреса: вид посылают ссылкой.
  expect(page.url()).toContain('group=assignee')
  await page.reload()
  await expect(page.locator('.swimlane')).toHaveCount(2)

  await page.getByLabel('Группировка').selectOption('none')
  await expect(page.locator('.swimlane')).toHaveCount(1)
  expect(page.url()).not.toContain('group=')
})
