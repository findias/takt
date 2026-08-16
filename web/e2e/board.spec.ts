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

  // Карточка открывается нажатием — и адрес меняется вместе с ней.
  await cardIn(page, 'Очередь', 'Прислать коллеге').click()
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
  // Ошибка — это тревога с причиной и кнопкой, а не строчка текста:
  // проверяем именно её, иначе совпадений в разметке несколько.
  const refusal = strangerPage.getByRole('alert')
  await expect(refusal).toBeVisible()
  await expect(refusal).toContainText(/не найдена/)
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

  // Снять — тем же пунктом меню, что и назначить: два списка
  // «назначить» и «снять» вдвое длиннее и заставляют помнить, кто где.
  await cardIn(page, 'Очередь', 'Кому-то делать').hover()
  await cardIn(page, 'Очередь', 'Кому-то делать')
    .getByRole('button', { name: /Действия карточки/ })
    .click()
  await page.getByRole('menuitem', { name: /Снять: / }).first().click()
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

test('у задачи есть номер, он виден и находится поиском', async ({ page }) => {
  await register(page)
  // Ключ выводится из названия доски: «Продукт» даёт ПРОД.
  await createBoard(page, 'Продукт')
  await addCard(page, 'Очередь', 'Согласовать смету')
  await addCard(page, 'Очередь', 'Разобрать обращения')

  // Номер виден прямо на доске: за ним не надо открывать карточку.
  await expect(cardIn(page, 'Очередь', 'Согласовать смету').getByText('ПРОД-1')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Разобрать обращения').getByText('ПРОД-2')).toBeVisible()

  // И в панели — над названием: открыв карточку по ссылке из переписки,
  // первым делом сверяют, та ли это задача.
  await cardIn(page, 'Очередь', 'Согласовать смету').getByRole('button', { name: 'Согласовать смету' }).click()
  await expect(page.getByRole('complementary').getByText('ПРОД-1')).toBeVisible()
  await page.keyboard.press('Escape')

  // Поиск по номеру — то, ради чего номер и заводился.
  await page.getByRole('searchbox', { name: 'Найти карточку' }).fill('ПРОД-2')
  await expect(cardIn(page, 'Очередь', 'Разобрать обращения')).toBeVisible()
  await expect(page.getByRole('group', { name: /Согласовать смету/ })).toHaveCount(0)
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

test('колонку можно свернуть, и это переживает перезагрузку', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со сворачиванием')
  await addCard(page, 'Готово', 'Уже сделано')

  const done = page.getByRole('region', { name: 'Готово' })
  await expect(done.getByRole('group', { name: /Уже сделано/ })).toBeVisible()

  await done.getByRole('button', { name: 'Свернуть «Готово»' }).click()

  // Карточек не видно, а счётчик остался: свёрнутая колонка не должна
  // становиться слепым пятном.
  await expect(done.getByRole('group', { name: /Уже сделано/ })).toBeHidden()
  await expect(done.getByRole('button', { name: /Развернуть «Готово»/ })).toBeVisible()
  await expect(done.getByRole('button', { name: 'Добавить карточку' })).toHaveCount(0)

  // Личное предпочтение смотрящего: не в адресе, но переживает
  // перезагрузку.
  expect(page.url()).not.toContain('collapsed')
  await page.reload()
  await expect(
    page.getByRole('region', { name: 'Готово' }).getByRole('button', { name: /Развернуть/ }),
  ).toBeVisible({ timeout: 10_000 })

  await page.getByRole('region', { name: 'Готово' }).getByRole('button', { name: /Развернуть/ }).click()
  await expect(
    page.getByRole('region', { name: 'Готово' }).getByRole('group', { name: /Уже сделано/ }),
  ).toBeVisible()
})

test('настроенный вид сохраняется и открывается одним нажатием', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с видами')
  await addCard(page, 'Очередь', 'Согласовать смету')
  await addCard(page, 'Очередь', 'Договор аренды')

  // Пока ничего не настроено, сохранять нечего: вид «доска как есть»
  // не нужен никому.
  await expect(page.getByRole('button', { name: 'Сохранить вид' })).toHaveCount(0)

  await page.getByRole('searchbox', { name: 'Найти карточку' }).fill('договор')
  await expect(page.getByRole('group', { name: /Согласовать смету/ })).toHaveCount(0)

  await page.getByRole('button', { name: 'Сохранить вид' }).click()
  await page.getByLabel('Название вида').fill('Только договоры')
  await page.getByRole('button', { name: 'Сохранить', exact: true }).click()
  // Точное имя: рядом стоит кнопка «Забыть вид «Только договоры»»,
  // и по подстроке нашлись бы обе.
  await expect(page.getByRole('button', { name: 'Только договоры', exact: true })).toBeVisible()

  // Сбрасываем фильтр и открываем вид заново — это и есть весь его смысл.
  await page.getByRole('button', { name: 'Показать все' }).click()
  await expect(cardIn(page, 'Очередь', 'Согласовать смету')).toBeVisible()

  await page.getByRole('button', { name: 'Только договоры', exact: true }).click()
  await expect(page.getByRole('group', { name: /Согласовать смету/ })).toHaveCount(0)
  await expect(cardIn(page, 'Очередь', 'Договор аренды')).toBeVisible()

  // Вид живёт на сервере, а не в браузере: переживает перезагрузку.
  await page.reload()
  await expect(page.getByRole('button', { name: 'Только договоры', exact: true })).toBeVisible({
    timeout: 10_000,
  })
})

test('палитра находит карточку и выполняет команду', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с палитрой')
  await addCard(page, 'Очередь', 'Согласовать смету')
  await addCard(page, 'Очередь', 'Договор аренды')

  await page.keyboard.press('Control+k')
  const input = page.getByRole('combobox', { name: 'Поиск и команды' })
  await expect(input).toBeVisible()

  // Карточка: четыре буквы вместо прокрутки и глаз.
  await input.fill('догов')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: 'Договор аренды' })).toBeVisible()
  expect(page.url()).toMatch(/\/card\//)

  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // Команда: в том же списке, потому что человек не разделяет
  // «найти» и «сделать», пока не начал набирать.
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Поиск и команды' }).fill('исполнител')
  await page.keyboard.press('Enter')
  // Исполнителей ни у кого нет, поэтому дорожка одна — «Ни на ком».
  // Она остаётся видимой всегда: именно там теряется работа.
  await expect(page.getByRole('heading', { name: 'Ни на ком' })).toBeVisible()
  expect(page.url()).toContain('group=assignee')
})

test('палитра закрывается по Escape и ничего не делает', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска без последствий')
  await addCard(page, 'Очередь', 'Не трогать')

  await page.keyboard.press('Control+k')
  await expect(page.getByRole('combobox', { name: 'Поиск и команды' })).toBeVisible()
  await page.keyboard.press('Escape')

  await expect(page.getByRole('combobox', { name: 'Поиск и команды' })).toBeHidden()
  await expect(cardIn(page, 'Очередь', 'Не трогать')).toBeVisible()
  expect(page.url()).not.toMatch(/\/card\//)
})

// Настоящее сенсорное устройство, а не просто узкое окно: у наведения
// и у пальца разные правила, и проверять надо те, что достаются пальцу.
test('на узком экране показывается одна колонка с переключателем', async ({ browser }) => {
  const phone = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  })
  const page = await phone.newPage()
  await register(page)
  await createBoard(page, 'Доска в кармане')
  await addCard(page, 'Очередь', 'Первое дело')

  // Колонка одна: горизонтальная прокрутка доски на телефоне
  // превращает работу в поиск.
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'В работе' })).toHaveCount(0)

  // Остальные — переключателем, и в нём видно, сколько там работы.
  const tabs = page.getByRole('tablist', { name: 'Колонки' })
  await expect(tabs.getByRole('tab')).toHaveCount(3)
  await tabs.getByRole('tab', { name: /Очередь/ }).and(page.locator('[aria-selected="true"]')).waitFor()

  await tabs.getByRole('tab', { name: /В работе/ }).click()
  await expect(page.getByRole('region', { name: 'В работе' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Очередь' })).toHaveCount(0)

  // Перенести карточку без перетаскивания можно и здесь: на телефоне
  // это единственный путь — HTML5-перетаскивание пальцем не работает.
  await tabs.getByRole('tab', { name: /Очередь/ }).click()
  const card = cardIn(page, 'Очередь', 'Первое дело')
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Перенести в «В работе»' }).click()

  await tabs.getByRole('tab', { name: /В работе/ }).click()
  await expect(cardIn(page, 'В работе', 'Первое дело')).toBeVisible()

  // Отборы убраны под кнопку, и число на ней говорит, что доска
  // показана не вся: спрятанный фильтр без такого напоминания — это
  // фильтр, о котором забывают.
  const toggle = page.getByRole('button', { name: /Отбор/ })
  await expect(page.getByRole('combobox', { name: 'Исполнитель' })).toBeHidden()
  await expect(page.getByRole('searchbox', { name: 'Найти карточку' })).toBeVisible()

  await toggle.click()
  await page.getByRole('checkbox', { name: 'Заблокированные' }).check()
  await expect(toggle).toHaveText(/Отбор · 1/)

  await toggle.click()
  await expect(page.getByRole('checkbox', { name: 'Заблокированные' })).toBeHidden()
  await expect(toggle).toHaveText(/Отбор · 1/)
  await phone.close()
})

test('подзадачи раскрываются прямо с доски', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска разбиения')
  await addCard(page, 'Очередь', 'Собрать отчёт')

  const parent = cardIn(page, 'Очередь', 'Собрать отчёт')
  await parent.getByRole('button', { name: 'Собрать отчёт' }).click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel('Название подзадачи').fill('Свести цифры')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await expect(page.getByRole('button', { name: 'Свести цифры' }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // Свёрнуто по умолчанию: разбиение видно мерой, а не списком, —
  // иначе колонка из десяти разбитых задач превращается в простыню.
  const toggle = parent.getByRole('button', { name: /подзадачи/i })
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(parent.getByText('Свести цифры')).toBeHidden()

  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(parent.getByText('Свести цифры')).toBeVisible()

  // Подзадача этой же доски открывается прямо отсюда: связь должна
  // проходиться, а не только показываться.
  await parent.getByRole('button', { name: 'Свести цифры' }).click()
  await expect(page.getByRole('heading', { name: 'Свести цифры' })).toBeVisible()
})

test('история спрятана за вкладкой, карточка открывается обсуждением', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска вкладок')
  await addCard(page, 'Очередь', 'Первая задача')
  await addCard(page, 'Очередь', 'Вторая задача')

  await cardIn(page, 'Очередь', 'Первая задача').getByRole('button', { name: 'Первая задача' }).click()

  // Открывается обсуждение: карточку чаще открывают, чтобы прочитать,
  // о чём договорились. История же раньше шла последним разделом того же
  // свитка и вытесняла вниз всё, ради чего карточку открывали.
  await expect(page.getByRole('tab', { name: 'Обсуждение' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('heading', { name: 'История' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Подзадачи' })).toHaveCount(0)

  await page.getByRole('tab', { name: 'Работа' }).click()
  await expect(page.getByRole('heading', { name: 'Подзадачи' })).toBeVisible()

  await page.getByRole('tab', { name: 'История' }).click()
  await expect(page.getByRole('heading', { name: 'История' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Подзадачи' })).toHaveCount(0)

  // Вкладка не запоминается между карточками: заглянувший в историю
  // одной задачи открывает следующую не затем, чтобы читать историю.
  await page.getByRole('button', { name: 'Закрыть' }).first().click()
  await cardIn(page, 'Очередь', 'Вторая задача').getByRole('button', { name: 'Вторая задача' }).click()
  await expect(page.getByRole('tab', { name: 'Обсуждение' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})

test('нажатие открывает карточку, а нажатие на её кнопку — нет', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска нажатий')
  await addCard(page, 'Очередь', 'Открыться по нажатию')
  const card = cardIn(page, 'Очередь', 'Открыться по нажатию')

  // Куда угодно по карточке: человек целится в карточку целиком,
  // а не в её заголовок.
  await card.click({ position: { x: 5, y: 5 } })
  await expect(page.getByRole('heading', { name: 'Открыться по нажатию' })).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // У кнопки внутри карточки своё действие, и оно не должно тонуть
  // в открытии: до этой проверки меню открывалось вместе с панелью.
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await expect(page.getByRole('menuitem', { name: 'Переименовать' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Открыться по нажатию' })).toHaveCount(0)
})

test('подзадача заводится из карточки одним полем', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с подзадачами')
  await addCard(page, 'Очередь', 'Выпустить релиз')

  await cardIn(page, 'Очередь', 'Выпустить релиз').click()
  await expect(page.getByRole('heading', { name: 'Выпустить релиз' })).toBeVisible()
  // Карточка открывается обсуждением; подзадачи живут на «Работе».
  await page.getByRole('tab', { name: 'Работа' }).click()

  // Название — всё, что спрашивают: подзадача это обычная карточка,
  // и заводится она тем же движением, что и карточка в колонке.
  await page.getByLabel('Название подзадачи').fill('Прогнать тесты')
  await page.getByRole('button', { name: 'Подзадача' }).click()

  // Она сразу видна и в списке подзадач, и на самой доске: это одна
  // и та же карточка, а не запись внутри родителя.
  await expect(page.getByRole('complementary').getByText('Прогнать тесты')).toBeVisible()
  await expect(page.getByRole('progressbar', { name: 'Готово 0 из 1', exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Закрыть' }).first().click()
  await expect(cardIn(page, 'Очередь', 'Прогнать тесты')).toBeVisible()

  // И переживает перезагрузку — то есть связь легла в базу, а не
  // в память вкладки.
  await page.reload()
  await cardIn(page, 'Очередь', 'Выпустить релиз').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await expect(page.getByRole('progressbar', { name: 'Готово 0 из 1', exact: true })).toBeVisible()

  // Связь проходится в обе стороны: из родителя — в подзадачу,
  // из подзадачи — обратно. До этого связь было видно, но пройти по ней
  // можно было только поиском по доске.
  await page.getByRole('complementary').getByRole('button', { name: 'Прогнать тесты' }).click()
  await expect(page.getByRole('heading', { name: 'Прогнать тесты' })).toBeVisible()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByRole('complementary').getByRole('button', { name: 'Выпустить релиз' }).click()
  await expect(page.getByRole('heading', { name: 'Выпустить релиз' })).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // А на самой доске подзадача говорит, чья она часть, — и по этой
  // строке тоже можно перейти к родителю.
  const subtask = cardIn(page, 'Очередь', 'Прогнать тесты')
  await expect(subtask.getByRole('button', { name: 'Выпустить релиз' })).toBeVisible()
  // Родитель показывает разбиение полосой, и она же раскрывает
  // подзадачи: мера и путь внутрь неё — одно управление, поэтому мера
  // читается вслух как имя кнопки, а не как отдельная полоса.
  await expect(
    cardIn(page, 'Очередь', 'Выпустить релиз').getByRole('button', {
      name: /Подзадачи: готово 0 из 1/,
    }),
  ).toBeVisible()
})

test('исполнителей у карточки может быть несколько', async ({ page, browser }) => {
  const who = await register(page)
  await createBoard(page, 'Доска вдвоём')
  await addCard(page, 'Очередь', 'Делать вдвоём')

  // Второй человек в организации: без него «несколько» проверить не на ком.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await page
    .getByRole('textbox', { name: 'Почта коллеги' })
    .fill(`vtoroy-${Math.random().toString(36).slice(2, 8)}@example.test`)
  await page.getByRole('button', { name: 'Пригласить', exact: true }).click()
  // Ссылка лежит в поле рядом с кнопкой «Скопировать»: её и читаем.
  const invite = await page.locator('input[readonly]').first().inputValue()
  expect(invite, 'ссылка приглашения').toContain('/invite/')

  const second = await browser.newContext()
  const secondPage = await second.newPage()
  await secondPage.goto(invite!.trim())
  await secondPage.getByLabel('Как вас зовут').fill('Иван Петров')
  await secondPage.getByLabel('Пароль').fill('parol12345')
  await secondPage.getByRole('button', { name: /Принять|Присоединиться/ }).click()
  await expect(secondPage.getByRole('button', { name: 'Доска вдвоём' }).first()).toBeVisible()
  await second.close()

  // Назначаем обоих — из панели карточки.
  await page.goto('/')
  await openBoard(page, 'Доска вдвоём')
  await cardIn(page, 'Очередь', 'Делать вдвоём').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel('Добавить исполнителя').selectOption({ label: 'Проверяющий' })
  await page.getByLabel('Добавить исполнителя').selectOption({ label: 'Иван Петров' })

  // Оба в списке «кто делает» — и никого из них больше не предлагают
  // добавить: список исполнителей и список свободных не пересекаются.
  const panel = page.getByRole('complementary')
  await expect(panel.getByText('Проверяющий').first()).toBeVisible()
  await expect(panel.getByText('Иван Петров').first()).toBeVisible()
  await expect(page.getByLabel('Добавить исполнителя')).toHaveCount(0)
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  // На доске видны оба — и назначение пережило перезагрузку.
  await page.reload()
  const card = cardIn(page, 'Очередь', 'Делать вдвоём')
  await expect(card.getByTitle('Проверяющий')).toBeVisible()
  await expect(card.getByTitle('Иван Петров')).toBeVisible()

  // Фильтр «на мне» показывает работу, о которой договорились вдвоём:
  // иначе один из двоих не найдёт её у себя.
  await page.getByLabel('Исполнитель').selectOption({ label: 'Иван Петров' })
  await expect(card).toBeVisible()

  await page.getByLabel('Исполнитель').selectOption({ label: 'Ни на ком' })
  await expect(card).toHaveCount(0)
  expect(who.email).toBeTruthy()
})

test('ссылка на убранную доску предлагает вернуть её, а не сообщает о поломке', async ({
  page,
}) => {
  await register(page)
  await createBoard(page, 'Доска под архив')
  await addCard(page, 'Очередь', 'Тут была работа')
  const boardUrl = page.url()

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Убрать доску «Доска под архив» в архив' }).click()
  // Ждём, пока архивация доедет до сервера: иначе следующий переход
  // успевает опередить запрос и проверяет живую доску.
  await expect(page.getByRole('button', { name: 'Вернуть', exact: false }).first()).toBeVisible()

  // Прежде здесь было «доска не найдена» — и человек шёл искать поломку
  // там, где её нет.
  await page.goto(boardUrl)
  await expect(page.getByText('Доска в архиве')).toBeVisible()

  await page.getByRole('button', { name: 'Вернуть из архива' }).click()
  await expect(cardIn(page, 'Очередь', 'Тут была работа')).toBeVisible()
})

test('работу можно поставить на доску соседей, и видно, что с ней стало', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Поставки')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await createBoard(page, 'Платформа')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Поставки')
  await addCard(page, 'Очередь', 'Выпустить релиз')

  await cardIn(page, 'Очередь', 'Выпустить релиз').click()
  await page.getByRole('tab', { name: 'Работа' }).click()

  // Постановка работы соседям — то же одно поле плюс выбор доски.
  // Отдельной «заявки» нет: запрос это карточка на их доске.
  await page.getByLabel('Название подзадачи').fill('Поднять квоту на хранилище')
  await page.getByLabel('Доска подзадачи').selectOption({ label: 'Платформа' })
  // Правила доски-получателя названы до нажатия, а не после отказа.
  await expect(page.getByText(/ляжет на доску «Платформа»/)).toBeVisible()
  await page.getByRole('button', { name: 'Подзадача' }).click()

  // Разбиение её считает, хотя лежит она не здесь.
  await expect(page.getByRole('progressbar', { name: 'Готово 0 из 1', exact: true })).toBeVisible()
  const row = page.getByRole('complementary').getByText('Поднять квоту на хранилище')
  await expect(row).toBeVisible()
  // И видно, что с ней у них: взялись или лежит.
  await expect(page.getByText(/Ещё не начали · Очередь/)).toBeVisible()

  // На своей доске её нет — работа принадлежит исполнителю.
  await page.getByRole('button', { name: 'Закрыть' }).first().click()
  await expect(cardIn(page, 'Очередь', 'Поднять квоту на хранилище')).toHaveCount(0)

  // У соседей она есть, с их номером — и с указанием, чья это часть.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Платформа')
  const theirs = cardIn(page, 'Очередь', 'Поднять квоту на хранилище')
  await expect(theirs).toBeVisible()
  await expect(theirs.getByText('ПЛАТ-1')).toBeVisible()

  // Соседи работу не берут.
  await theirs.hover()
  await theirs.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Убрать в архив' }).click()

  // Отказ читается отказом, а не отсутствием доступа: раньше архивная
  // чужая карточка выпадала из ответа, и оставалась одна ветка —
  // «в подразделении, которого вам не видно».
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Поставки')
  await cardIn(page, 'Очередь', 'Выпустить релиз').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await expect(page.getByText('Работу не взяли')).toBeVisible()
  await expect(page.getByText(/которого вам не видно/)).toHaveCount(0)
})
