import { readFile } from 'node:fs/promises'
import { workbook } from './xlsx.ts'
import { taktPackage } from './takt-package.ts'
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
  await page.getByRole('button', { name: 'Завести новую организацию' }).click()
  await page.getByLabel('Название организации').fill(who.org)
  await page.getByLabel('Как вас зовут').fill('Проверяющий')
  await page.getByLabel('Почта').fill(who.email)
  await page.getByLabel('Пароль').fill(who.password)
  await page.getByRole('button', { name: 'Завести организацию' }).click()
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
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

async function openBoard(page: Page, name: string) {
  await page.getByRole('button', { name, exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
}

async function addCard(page: Page, column: string, title: string) {
  const section = page.getByRole('region', { name: column })
  const field = section.getByPlaceholder('Что нужно сделать?')
  // Форма остаётся открытой после заведения карточки — карточки заводят
  // подряд. Открывать её второй раз нечем: кнопки в этот момент нет.
  if (!(await field.isVisible())) {
    await section.getByRole('button', { name: `Завести карточку в «${column}»` }).click()
  }
  await field.fill(title)
  await section.getByRole('button', { name: `Завести карточку в «${column}»` }).click()
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

/**
 * Назначить или снять исполнителя прямо на карточке.
 *
 * Путь идёт через саму стопку исполнителей, а не через меню «…»:
 * поля правятся нажатием по ним самим, и пункт меню один на человека —
 * он же назначает, он же снимает.
 */
async function toggleAssignee(page: Page, card: ReturnType<typeof cardIn>) {
  await card.hover()
  await card.getByRole('button', { name: /Исполнител/ }).click()
  await page.getByRole('menuitemcheckbox').first().click()
}

/** Раскрыть отборы, если свёрнуты. На широком экране они открыты всегда
 *  (решение владельца 22.09.2026), кнопка «Отбор» есть только на узком;
 *  повторный вызов ничего не делает. */
async function openFilters(page: Page) {
  const toggle = page.getByRole('button', { name: /^Отбор/ })
  if (!(await toggle.isVisible())) return
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click()
}

/** Выбрать вид доски: он — выпадающим списком слева. */
async function pickView(page: Page, name: 'Доска' | 'Таблица' | 'Изменения') {
  await page.getByRole('combobox', { name: 'Вид доски' }).selectOption({ label: name })
}

/** То же для метки: нажатие по ряду меток, пункт по названию. Имя
 *  пункта — название и откуда метка, поэтому ищется по началу. */
async function toggleLabel(page: Page, card: ReturnType<typeof cardIn>, name: string) {
  await card.hover()
  await card.getByRole('button', { name: /^Метки/ }).click()
  await labelChoice(page).getByRole('option', { name: new RegExp(`^${name}`) }).click()
}

/** Окно выбора метки. Искать пункты по всей странице нельзя: `<option>`
 *  есть и в отборах над доской, и в выборе темы. */
function labelChoice(page: Page) {
  return page.getByRole('dialog', { name: 'Метки' })
}

/**
 * Поле обсуждения растёт по написанному.
 *
 * Раньше третья строка уезжала под собственную полосу прокрутки, и
 * человек писал в окошко высотой в два ряда. Делает это `field-sizing`
 * (Baseline newly с 16.06.2026, сверено 23.08.2026), а не скрипт, —
 * но проверять надо именно в браузере: свойство молодое, и «поддержано»
 * тут значит «померено», а не «написано в таблице».
 */
test('поле обсуждения растёт по написанному и не растёт без предела', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Растущее поле')
  await addCard(page, 'Очередь', 'Обсудить')
  await cardIn(page, 'Очередь', 'Обсудить').getByRole('button', { name: 'Обсудить' }).click()

  const поле = page.getByPlaceholder('Написать в обсуждение')
  const высота = async () => (await поле.boundingBox())!.height
  const пусто = await высота()

  await поле.fill('Первая строка\nВторая строка\nТретья строка\nЧетвёртая строка')
  const выросло = await высота()
  expect(выросло, 'поле не выросло под четырьмя строками').toBeGreaterThan(пусто)

  // Потолок обязателен: без него длинный текст выталкивает за экран
  // кнопку, которой его отправляют.
  await поле.fill('строка\n'.repeat(200))
  const предел = await высота()
  expect(предел, 'поле переросло свой потолок').toBeLessThanOrEqual(
    (await page.evaluate(() => window.innerHeight)) * 0.45,
  )
  await expect(page.getByRole('button', { name: 'Отправить' })).toBeVisible()
})

/**
 * Таблица остаётся читаемой, когда её листают.
 *
 * Три вещи, которых не видно ни в разметке, ни на снимке спокойного
 * экрана, и каждая ломается по-своему.
 *
 * Шапка: на третьем экране прокрутки столбцы есть, а чем они названы —
 * надо вспоминать. Замер 23.08.2026 до починки: заголовок стоял
 * на −3229 пикселей, то есть втрое дальше за краем, чем сам экран.
 *
 * Ключ: на узком экране таблица шире окна (790 при 360), и на прокрутке
 * вбок первый столбец уезжал — строка теряла то единственное, чем она
 * названа. На широком мониторе этого не увидеть вовсе: там всё влезает.
 *
 * Направление: `aria-sort` говорил «по возрастанию» на все пять
 * сортировок разом, а три из них идут сверху вниз от большего
 * к меньшему. Диктору говорили обратное тому, что видел зрячий.
 */
test('таблицу можно листать, не теряя ни шапки, ни ключа', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Листаемая')
  // Строк должно быть больше, чем влезает, иначе прокручивать нечего
  // и проверка проходит, ничего не проверив: первая её редакция
  // на трёх карточках была зелёной и без всякого закрепления.
  for (const n of [1, 2, 3, 4, 5, 6, 7, 8]) await addCard(page, 'Очередь', `Задача ${n}`)
  await page.setViewportSize({ width: 1200, height: 400 })

  await pickView(page, 'Таблица')
  const таблица = page.locator('.board-table')
  await expect(таблица).toBeVisible()

  // Возраст — сортировка по умолчанию, и она убывающая: самое старое
  // сверху. Заголовок обязан говорить то же самое.
  await expect(page.locator('th', { hasText: 'Возраст' })).toHaveAttribute(
    'aria-sort',
    'descending',
  )
  await page.getByRole('button', { name: /Отсортировать по сроку/ }).click()
  await expect(page.locator('th', { hasText: 'Срок' })).toHaveAttribute('aria-sort', 'ascending')

  // Шапка на месте после прокрутки: ячейка заголовка стоит у верхнего
  // края области, которая листается, а не уезжает вместе со строками.
  // Меряется ячейка, а не `thead`: у строки заголовка геометрия
  // остаётся на месте, и по ней кажется, будто ничего не закреплено, —
  // на этом первый замер и обманулся.
  const шапка = await page.evaluate(() => {
    const wrap = document.querySelector('.table-wrap') as HTMLElement
    wrap.scrollTop = 400
    // Не первая ячейка: она закреплена и по горизонтали — как столбец
    // с ключом, — и потому осталась бы на месте даже без закрепления
    // шапки. На этом вторая редакция проверки и обманулась: зелёная
    // и с `position: sticky`, и без него.
    const th = document.querySelector('.board-table thead th:nth-child(3)') as HTMLElement
    return {
      прокручено: Math.round(wrap.scrollTop),
      верхШапки: Math.round(th.getBoundingClientRect().top),
      верхОбласти: Math.round(wrap.getBoundingClientRect().top),
    }
  })
  expect(шапка.прокручено, 'прокручивать нечего — проверка ничего не проверяет').toBeGreaterThan(0)
  expect(
    Math.abs(шапка.верхШапки - шапка.верхОбласти),
    'заголовок уехал вместе со строками',
  ).toBeLessThan(4)

  // Ключ на месте после прокрутки вбок на узком экране.
  await page.setViewportSize({ width: 360, height: 760 })
  const ключ = await page.evaluate(() => {
    const wrap = document.querySelector('.table-wrap') as HTMLElement
    wrap.scrollLeft = 2000
    const cell = document.querySelector('.board-table tbody td') as HTMLElement
    const область = wrap.getBoundingClientRect()
    const к = cell.getBoundingClientRect()
    return { шире: wrap.scrollWidth > область.width, виден: к.right > область.left && к.left < область.right }
  })
  expect(ключ.шире, 'таблица влезла в узкое окно — прокрутки вбок нет, проверять нечего').toBe(true)
  expect(ключ.виден, 'первый столбец уехал за край при прокрутке вбок').toBe(true)
})

/**
 * Связи нет — и об этом сказано всё время, пока её нет.
 *
 * До сих пор отказ связи говорился тостом: «Нет связи с сервером.
 * Карточка вернулась на место», с кнопкой «Повторить». Тост про
 * действие правильный, но он уезжает через пять секунд, а связь
 * от этого не появляется: между тостами отключённый интерфейс
 * выглядит рабочим, и человек продолжает набирать в пустоту.
 *
 * Разница видна только на чужом продукте: у себя каждый отказ выглядел
 * разумным по отдельности. Разбор Chromium Issue Tracker 23.08.2026
 * с выключенной посреди работы сетью: полоса во всю ширину под шапкой,
 * висит, пока связи нет. Правило переносимое: **разовое событие —
 * тост, длящееся состояние — полоса в постоянном месте.**
 */
test('пропавшая связь говорит о себе всё время, а не пять секунд', async ({ page, context }) => {
  await register(page)
  await createBoard(page, 'Доска без сети')
  await addCard(page, 'Очередь', 'Работа')

  const полоса = page.getByText(/Связи с сервером нет/)
  await expect(полоса).toHaveCount(0)

  await context.setOffline(true)
  // Действие, которому нужна сеть: перенос карточки уходит на сервер
  // следом за тем, как применился на экране.
  await cardIn(page, 'Очередь', 'Работа').focus()
  await page.keyboard.press('Control+ArrowRight')
  await expect(полоса).toBeVisible({ timeout: 15_000 })

  // Полоса не уезжает сама: состояние длится, пока длится.
  await page.waitForTimeout(6000)
  await expect(полоса).toBeVisible()

  // Связь вернулась — полоса ушла с первым же удавшимся запросом.
  await context.setOffline(false)
  await page.getByRole('button', { name: 'Все доски' }).click()
  await expect(полоса).toHaveCount(0, { timeout: 15_000 })
})

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
  // Выход — в личных настройках за именем.
  await page.getByRole('button', { name: /^Личные настройки/ }).click()
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
  await toggleAssignee(page, card)

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
  await toggleAssignee(page, cardIn(page, 'Очередь', 'Кому-то делать'))
  await expect(cardIn(page, 'Очередь', 'Кому-то делать').locator('.avatar')).toHaveCount(0)
})

test('метка заводится в организации и вешается на карточку', async ({ page }) => {
  await register(page)

  // Метки живут в организации: одинаково названная метка на двух досках
  // это одна метка, иначе фильтр собирать не из чего.
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByPlaceholder('Название метки').fill('Срочно')
  await page.getByRole('button', { name: 'Завести метку' }).click()
  // Ждём чип, а не слово: «срочно» стоит и в объяснении пустого
  // списка, и ожидание по подстроке проходило раньше, чем метка
  // заводилась.
  await expect(page.getByText('Срочно', { exact: true })).toBeVisible()
  await page.getByPlaceholder('Название метки').fill('Важное')
  await page.getByRole('button', { name: 'Завести метку' }).click()
  await expect(page.getByText('Важное', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Доски' }).click()
  await createBoard(page, 'Доска с метками')
  await addCard(page, 'Очередь', 'Пометить меня')

  const beforeLabel = await boardVersion(page)
  const card = cardIn(page, 'Очередь', 'Пометить меня')
  await toggleLabel(page, card, 'Срочно')

  // На доске метка — точка: чип отвечает «что это за метка» и стоит
  // в панели, а точка отвечает «одна ли это группа». Имя метки
  // остаётся в подсказке и в имени поля — цвет не может быть
  // единственным носителем смысла.
  await expect(card.getByRole('button', { name: 'Метки: Срочно' })).toBeVisible()
  // Точная подсказка — у самой точки: название и откуда метка.
  // У поля она «Метки: Срочно».
  await expect(card.getByTitle('Срочно — вся организация', { exact: true })).toBeVisible()

  // Переживает перезагрузку — это данные, а не украшение экрана.
  // Ждём подтверждения: метка вешается мгновенно, а уходит следом.
  await savedSince(page, beforeLabel)
  await page.reload()
  await expect(
    cardIn(page, 'Очередь', 'Пометить меня').getByRole('button', { name: 'Метки: Срочно' }),
  ).toBeVisible({ timeout: 10_000 })

  // Метка вешается и из самой карточки: до этого за ней приходилось
  // возвращаться на доску — панель показывала о работе всё, кроме
  // того, чем она помечена.
  await cardIn(page, 'Очередь', 'Пометить меня').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «Пометить меня»/)
  await panel.getByRole('combobox', { name: 'Повесить или завести метку' }).fill('Важн')
  await panel.getByRole('option', { name: /^Важное/ }).click()
  const row = panel.locator('.related').filter({ hasText: 'Важное' })
  await expect(row).toHaveCount(1)
  await row.getByRole('button', { name: 'Снять' }).click()
  await expect(row).toHaveCount(0)
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // И снимается тем же меню, что вешалась.
  await toggleLabel(page, cardIn(page, 'Очередь', 'Пометить меня'), 'Срочно')
  await expect(
    cardIn(page, 'Очередь', 'Пометить меня').getByRole('button', { name: 'Метки: ни одной' }),
  ).toBeVisible()
})

// Метку заводят там, где она понадобилась: набрали на карточке название,
// которого нет, — метка заведена и повешена, и видна в «Команде».
// Прежде за этим уходили на «Команду» и возвращались — пять шагов
// и потерянное место. Всё — с клавиатуры: выбор проходят без мыши.
test('метка заводится прямо с карточки', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска на бегу')
  await addCard(page, 'Очередь', 'Договор с поставщиком')
  const card = cardIn(page, 'Очередь', 'Договор с поставщиком')

  // Меток в организации нет вовсе — а выбор на карточке всё равно
  // стоит: в нём и заводят.
  await card.hover()
  await card.getByRole('button', { name: 'Метки: ни одной' }).click()
  const search = page.getByRole('combobox', { name: 'Найти или завести метку' })
  await expect(search).toBeFocused()
  await page.keyboard.type('Ждём юристов')
  // Первым — узкое место: метка на бегу чаще нужна здесь.
  await expect(labelChoice(page).getByRole('option').first()).toHaveText(/Завести «Ждём юристов».*на этой доске/)
  await page.keyboard.press('ArrowDown')
  await expect(labelChoice(page).getByRole('option', { selected: true })).toHaveText(/на всю организацию/)
  await page.keyboard.press('Enter')

  await expect(card.getByRole('button', { name: 'Метки: Ждём юристов' })).toBeVisible()
  // Фокус вернулся туда, откуда открывали.
  await expect(card.getByRole('button', { name: 'Метки: Ждём юристов' })).toBeFocused()

  // Набрали то же в другом регистре — предложена существующая, второй
  // не заводится.
  await addCard(page, 'Очередь', 'Счёт за июль')
  const second = cardIn(page, 'Очередь', 'Счёт за июль')
  await second.hover()
  await second.getByRole('button', { name: 'Метки: ни одной' }).click()
  await page.keyboard.type('ЖДЁМ ЮРИСТОВ')
  await expect(labelChoice(page).getByRole('option')).toHaveCount(1)
  await expect(labelChoice(page).getByRole('option')).toHaveText(/^Ждём юристов/)
  await page.keyboard.press('Enter')
  await expect(second.getByRole('button', { name: 'Метки: Ждём юристов' })).toBeVisible()

  // Метка видна в «Команде» — одна, в организации, куда её и завели.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await expect(page.getByText('Ждём юристов', { exact: true })).toHaveCount(1)
  await expect(page.getByRole('heading', { name: 'Вся организация' })).toBeVisible()

  // Убрали в архив — набранное название предлагает вернуть, а не
  // завести вторую.
  await page.getByRole('button', { name: 'Убрать в архив метку «Ждём юристов»' }).click()
  await expect(page.getByRole('heading', { name: 'Убранные в архив' })).toBeVisible()
  await page.getByRole('button', { name: 'Доски' }).click()
  await openBoard(page, 'Доска на бегу')
  await addCard(page, 'Очередь', 'Третья')
  const third = cardIn(page, 'Очередь', 'Третья')
  await third.hover()
  await third.getByRole('button', { name: 'Метки: ни одной' }).click()
  await page.keyboard.type('ждём юристов')
  await expect(labelChoice(page).getByRole('option')).toHaveText([/^Вернуть из архива «Ждём юристов»/])
  await page.keyboard.press('Enter')
  await expect(third.getByRole('button', { name: 'Метки: Ждём юристов' })).toBeVisible()
})

// Оценку ставят пачкой на планировании, а причина блокировки меняется
// чаще, чем ставится. И то и другое жило только в панели: открывать
// карточку ради одного числа — пятнадцать лишних переходов на разбор
// бэклога.
test('оценка ставится шагами в панели, блокировка — прямо с доски', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с оценками')
  await addCard(page, 'Очередь', 'Оценить меня')
  const card = cardIn(page, 'Очередь', 'Оценить меня')

  // Оценку меняют почти всегда на единицу, и шаги делают это одним
  // нажатием; печатать число тоже можно — значение осталось полем.
  const beforeEstimate = await boardVersion(page)
  await card.click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «Оценить меня»/)
  for (let i = 0; i < 3; i++) await panel.getByRole('button', { name: 'Увеличить оценку' }).click()
  await expect(panel.getByLabel('Оценка')).toHaveValue('3')
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // На карточке оценка — тихая цифра: единица одна на всю доску
  // и живёт в подсказке.
  await expect(card.getByTitle(/Оценка: 3/)).toBeVisible()

  // Это данные, а не украшение экрана.
  await savedSince(page, beforeEstimate)
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'Оценить меня').getByTitle(/Оценка: 3/)).toBeVisible({
    timeout: 10_000,
  })

  // Блокировка ставится с доски и причиной, написанной словами.
  const again = cardIn(page, 'Очередь', 'Оценить меня')
  await again.hover()
  await again.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Заблокировать…' }).click()
  await again.getByLabel('Причина блокировки').fill('ждём смежников')
  await again.getByLabel('Причина блокировки').press('Enter')
  await expect(again.getByText('Заблокирована: ждём смежников')).toBeVisible()

  // Снимается тем же меню. Правки причины поверх открытой блокировки
  // нет намеренно: блокировка — интервал, и вторая поверх первой
  // посчитала бы время в блоке дважды.
  await again.hover()
  await again.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Снять блокировку' }).click()
  await expect(again.getByText(/Заблокирована/)).toHaveCount(0)
})

test('выделенные карточки переносятся и убираются пачкой', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с выделением')
  await addCard(page, 'Очередь', 'Первая пачка')
  await addCard(page, 'Очередь', 'Вторая пачка')
  await addCard(page, 'Очередь', 'Не трогать')

  // Полосы нет, пока ничего не выделено: она обещала бы действие,
  // которому не над чем работать.
  await expect(page.getByRole('status', { name: 'Действия над выделенными' })).toHaveCount(0)

  for (const title of ['Первая пачка', 'Вторая пачка']) {
    const card = cardIn(page, 'Очередь', title)
    await card.hover()
    await card.getByRole('checkbox', { name: `Выделить «${title}»` }).check()
  }

  const bar = page.getByRole('status', { name: 'Действия над выделенными' })
  await expect(bar).toContainText('Выделено: 2 карточки')

  const beforeMove = await boardVersion(page)
  await bar.getByRole('button', { name: 'Перенести выделенные' }).click()
  await page.getByRole('menuitem', { name: 'В работе' }).click()

  await expect(cardIn(page, 'В работе', 'Первая пачка')).toBeVisible()
  await expect(cardIn(page, 'В работе', 'Вторая пачка')).toBeVisible()
  // Невыделенное осталось на месте — ради этого и выделяли.
  await expect(cardIn(page, 'Очередь', 'Не трогать')).toBeVisible()
  // Выделение снято: полоса относится к тому, что выделено сейчас.
  await expect(page.getByRole('status', { name: 'Действия над выделенными' })).toHaveCount(0)
  await savedSince(page, beforeMove)

  // Убрать пачкой — с одной отменой на всех: двадцать уведомлений
  // подряд не читает никто.
  for (const title of ['Первая пачка', 'Вторая пачка']) {
    const card = cardIn(page, 'В работе', title)
    await card.hover()
    await card.getByRole('checkbox', { name: `Выделить «${title}»` }).check()
  }
  await bar.getByRole('button', { name: 'В архив' }).click()
  await expect(cardIn(page, 'В работе', 'Первая пачка')).toHaveCount(0)
  await expect(cardIn(page, 'В работе', 'Вторая пачка')).toHaveCount(0)

  // Уведомление от переноса ещё висит: берём последнее — то, что
  // предлагает вернуть только что убранное.
  await page.getByRole('button', { name: 'Вернуть', exact: true }).last().click()
  await expect(cardIn(page, 'В работе', 'Первая пачка')).toBeVisible()
  await expect(cardIn(page, 'В работе', 'Вторая пачка')).toBeVisible()
})

// Закрыть доску можно только вокруг себя — это следует из политик базы,
// и раньше следовало отказом: перевод в «только вписанным» отклонялся,
// пока автор не впишет себя в состав. Порядок, известный лишь из отказа,
// — не порядок, а загадка.
test('закрытая доска заводится одним действием', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска для своих')

  await page.getByRole('button', { name: /Видна/ }).click()
  await page.getByLabel('Видна').selectOption('private')

  // Закрывший вписан в состав, и доска осталась у него рабочей.
  await expect(page.getByRole('button', { name: /Видна: 1 поимённо/ })).toBeVisible()
  await expect(
    page.getByLabel('Доступ к доске').getByText('Проверяющий'),
  ).toBeVisible()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()

  // Это данные, а не состояние экрана.
  await page.reload()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Видна: 1 поимённо/ })).toBeVisible()
})

// Раскрытый узел структуры отвечал только «кто здесь», хотя доски узла
// обещаны были ещё этапом 2.1, а число досок в строке узла стояло
// с самого начала.
test('узел структуры показывает свои доски и открывает их', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска отдела')

  // Заводим подразделение и отдаём ему доску, оставив её видной всем:
  // «чья доска» и «кому видно» — разные вопросы.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Структура' }).click()
  await page.getByRole('button', { name: 'Новое подразделение' }).click()
  await page.getByPlaceholder('Название').fill('Продажи')
  await page.getByRole('button', { name: 'Завести подразделение', exact: true }).click()
  await expect(page.getByRole('button', { name: '▸ Продажи' })).toBeVisible()

  await page.getByRole('button', { name: 'Доски' }).click()
  await openBoard(page, 'Доска отдела')
  await page.getByRole('button', { name: /Видна/ }).click()
  await page.getByLabel('Подразделение').selectOption({ label: 'Продажи' })

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Структура' }).click()
  await page.getByRole('button', { name: '▸ Продажи' }).click()

  // Доска узла названа и открывается отсюда: следующий шаг после
  // ответа «чем занято подразделение» — открыть.
  const boards = page.getByRole('list').filter({ hasText: 'Доска отдела' })
  await expect(boards.getByRole('button', { name: 'Доска отдела' })).toBeVisible()
  await boards.getByRole('button', { name: 'Доска отдела' }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
})

// Подписки сервер умеет с пятого этапа, а интерфейса у них не было
// вовсе: заводили запросом к API, а о том, что доставка встала,
// узнавали от соседней системы.
test('подписка на события заводится и показывает свои доставки', async ({ page }) => {
  await register(page)

  await page.getByRole('button', { name: 'Команда' }).click()
  // Список событий приходит с сервера — тот же, что доставляется.
  // Свой список интерфейса был вчетверо короче, и «работа сделана»,
  // ради которой подписку чаще всего и заводят, в нём отсутствовала.
  await expect(page.getByText('Работа отмечена сделанной')).toBeVisible()
  await page.getByLabel('Название подписки').fill('Оповещение дежурного')
  await page.getByLabel('Адрес получателя').fill('https://example.test/hooks/board')
  await page.getByRole('button', { name: 'Завести подписку', exact: true }).click()

  // Ключ подписи показывается один раз: подписываем им мы, хранит его
  // получатель.
  await expect(page.getByRole('textbox', { name: 'Ключ подписи' })).toBeVisible()
  await expect(page.getByText('https://example.test/hooks/board')).toBeVisible()
  // Ключ без правила проверки бесполезен, и правило стоит рядом с ним:
  // узнать его иначе можно было только чтением нашего кода на Go.
  await expect(page.getByText(/HMAC-SHA256/)).toBeVisible()
  await expect(page.getByRole('link', { name: 'описании контракта' })).toBeVisible()

  // Пока событий не было, журнал доставок пуст и говорит об этом.
  await page.getByRole('button', { name: 'Доставки' }).click()
  await expect(page.getByText('Доставок ещё не было')).toBeVisible()
  await page.getByRole('button', { name: 'Скрыть доставки' }).click()

  // Случилось событие — доставка появилась. Получателя нет, поэтому
  // она и не доедет; ради этого журнал и существует.
  await page.getByRole('button', { name: 'Доски' }).click()
  await createBoard(page, 'Доска с подпиской')
  await addCard(page, 'Очередь', 'Событие для подписки')

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByRole('button', { name: 'Доставки' }).click()
  await expect(page.getByText('Карточка создана').first()).toBeVisible()

  // Границы того, что доставка делает сама, названы числами и на месте:
  // прежде было сказано «повторяем, удваивая паузу» — из чего нельзя
  // узнать ни сколько раз мы повторим, ни того, что после последней
  // неудачи подписка отключится совсем. Числа приходят с сервера,
  // оттуда, где они и действуют.
  await expect(page.getByText(/Что мы делаем сами/)).toBeVisible()
  await expect(page.getByText(/повторяем до 8 раз/)).toBeVisible()
  // Срок сказан порядком величины, а не машинной точностью: «примерно
  // 123 мин» складывается из восьми удвоений и меняется от любой правки
  // пауз, а читают его, чтобы решить — ждать или чинить получателя.
  await expect(page.getByText(/около 2 часов/)).toBeVisible()
  await expect(page.getByText(/подписка\s+отключается/)).toBeVisible()
  await expect(page.getByText(/храним 30 дней/)).toBeVisible()

  // Вмешаться в идущую работу можно обратимо. До паузы выбор был
  // такой: терпеть два часа отказов либо удалить подписку вместе
  // с ключом подписи и журналом.
  await page.getByRole('button', { name: /Приостановить подписку/ }).click()
  await expect(page.getByText('Приостановлена')).toBeVisible()
  await expect(page.getByText(/события по этой подписке не копятся/)).toBeVisible()
  await page.getByRole('button', { name: /Возобновить подписку/ }).click()
  await expect(page.getByText('Приостановлена')).toHaveCount(0)

  // Подписка убирается тем же экраном.
  await page.getByRole('button', { name: /Удалить подписку/ }).click()
  await expect(page.getByText('https://example.test/hooks/board')).toHaveCount(0)
})

// Приоритет — уровень: он говорит, что важнее. Порядок карточек
// в колонке остаётся ручным и говорит, что взято следующим: уровень
// ничего не переставляет сам, и это главное, что здесь проверяется.
test('приоритет виден, отбирается и не трогает порядок', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с приоритетами')
  await addCard(page, 'Очередь', 'Первая по порядку')
  await addCard(page, 'Очередь', 'Вторая по порядку')

  const second = cardIn(page, 'Очередь', 'Вторая по порядку')
  await second.hover()
  await second.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Наивысший приоритет' }).click()
  // В меню уровень назван полно, на карточке — коротко: в плашке место
  // меряется знаками. Слова разные намеренно, см. `priorityShort`.
  await expect(second.getByText('горит')).toBeVisible()

  // Наивысшая не всплыла наверх: порядок ручной, и уровень его
  // не трогает.
  const titles = await page
    .getByRole('region', { name: 'Очередь' })
    .locator('.card-title')
    .allInnerTexts()
  expect(titles).toEqual(['Первая по порядку', 'Вторая по порядку'])

  // «Горит» — это верх шкалы, и отбор живёт в адресе, как остальные.
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Горит' }).check()
  await expect(cardIn(page, 'Очередь', 'Первая по порядку')).toHaveCount(0)
  await expect(second).toBeVisible()
  expect(page.url()).toContain('urgent=1')
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'Вторая по порядку')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Первая по порядку')).toHaveCount(0)
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Горит' }).uncheck()

  // Поставленный уровень правится нажатием по нему самому — как
  // исполнители и метки. У карточки со средним уровнем плашки нет
  // вовсе: умолчание у каждой второй карточки не информация, и путь
  // к нему — меню «…» или панель.
  const first = cardIn(page, 'Очередь', 'Первая по порядку')
  await expect(first.getByRole('button', { name: /Приоритет:/ })).toHaveCount(0)
  await first.hover()
  await first.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Наивысший приоритет' }).click()
  await first.getByRole('button', { name: /Приоритет:/ }).click()
  await page.getByRole('menuitemcheckbox', { name: 'Высокий' }).click()
  await expect(first.getByText('важно')).toBeVisible()

  // Вся шкала — в панели.
  await cardIn(page, 'Очередь', 'Вторая по порядку').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «Вторая по порядку»/)
  await panel.getByLabel('Приоритет').selectOption({ label: 'Низкий' })
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await expect(cardIn(page, 'Очередь', 'Вторая по порядку').getByText('фоном')).toBeVisible()

  // Низкий из «горит» выпадает.
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Горит' }).check()
  await expect(cardIn(page, 'Очередь', 'Вторая по порядку')).toHaveCount(0)
})

// Разбор бэклога — это десяток карточек подряд, которым надо поставить
// один уровень и одну метку. По одной это десяток попаданий в квадрат
// в тринадцать пикселей, а потом десять открытий панели.
test('shift берёт диапазон, а полоса делает всё сразу', async ({ page }) => {
  await register(page)

  // Метка нужна общая: они живут в организации, а не на доске.
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByPlaceholder('Название метки').fill('Разобрать')
  await page.getByRole('button', { name: 'Завести метку' }).click()
  await page.getByRole('button', { name: 'Доски' }).click()

  await createBoard(page, 'Доска разбора')
  for (const title of ['Первая', 'Вторая', 'Третья', 'Четвёртая']) {
    await addCard(page, 'Очередь', title)
  }

  // Щелчок по первой, shift-щелчок по третьей — выделены три подряд,
  // четвёртая осталась в стороне.
  const first = cardIn(page, 'Очередь', 'Первая')
  const third = cardIn(page, 'Очередь', 'Третья')
  await first.hover()
  await first.getByRole('checkbox', { name: /Выделить/ }).check()
  await third.hover()
  await third.getByRole('checkbox', { name: /Выделить/ }).click({ modifiers: ['Shift'] })

  const bar = page.getByRole('status', { name: 'Действия над выделенными' })
  await expect(bar).toContainText('Выделено: 3 карточки')

  // Уровень — всем троим сразу.
  await bar.getByRole('button', { name: 'Приоритет выделенным' }).click()
  await page.getByRole('menuitem', { name: 'Высокий' }).click()
  await expect(cardIn(page, 'Очередь', 'Первая').getByText('важно')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Третья').getByText('важно')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Четвёртая').getByText('важно')).toHaveCount(0)

  // Метка — тем же движением, и отмена одна на всю пачку.
  await first.hover()
  await first.getByRole('checkbox', { name: /Выделить/ }).check()
  await third.hover()
  await third.getByRole('checkbox', { name: /Выделить/ }).click({ modifiers: ['Shift'] })
  await expect(bar).toContainText('Выделено: 3 карточки')
  await bar.getByRole('button', { name: 'Пометить выделенные' }).click()
  await labelChoice(page).getByRole('option', { name: /^Разобрать/ }).click()
  await expect(cardIn(page, 'Очередь', 'Вторая').getByRole('button', { name: /Метки: Разобрать/ })).toBeVisible()

  await page.getByRole('button', { name: 'Снять', exact: true }).last().click()
  await expect(
    cardIn(page, 'Очередь', 'Вторая').getByRole('button', { name: 'Метки: ни одной' }),
  ).toBeVisible()
})

// Итерации в продукте были с третьего этапа, но жили только в отчёте:
// на доске не увидеть, что входит в спринт, а «что мы не запланировали»
// не спросить вовсе.
test('итерация видна на карточке и отбирается на доске', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со спринтом')
  await addCard(page, 'Очередь', 'В спринте')
  await addCard(page, 'Очередь', 'Мимо спринта')

  await page.getByRole('button', { name: '+ итерация' }).click()
  await page.getByPlaceholder('Название').fill('Неделя 34')
  await page.getByLabel('Начало').fill('2026-08-17')
  await page.getByLabel('Конец').fill('2026-08-23')
  await page.getByRole('button', { name: 'Завести итерацию', exact: true }).click()

  await cardIn(page, 'Очередь', 'В спринте').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «В спринте»/)
  await panel.getByLabel('Итерация карточки').selectOption({ label: 'Неделя 34' })
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // На карточке итерация названа: без этого на доске не видно,
  // что в спринт входит.
  await expect(cardIn(page, 'Очередь', 'В спринте').getByText('Неделя 34')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Мимо спринта').getByText('Неделя 34')).toHaveCount(0)

  // Отбор живёт в адресе, как остальные.
  await openFilters(page)
  await page.getByLabel('Итерация', { exact: true }).selectOption({ label: 'Неделя 34' })
  await expect(cardIn(page, 'Очередь', 'Мимо спринта')).toHaveCount(0)
  expect(page.url()).toContain('iteration=')
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'В спринте')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Мимо спринта')).toHaveCount(0)

  // «Не в итерации» — тоже ответ: незапланированная работа и есть та,
  // что съедает спринт незаметно.
  await openFilters(page)
  await page.getByLabel('Итерация', { exact: true }).selectOption({ label: 'Не в итерации' })
  await expect(cardIn(page, 'Очередь', 'Мимо спринта')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'В спринте')).toHaveCount(0)
})

// Поля срока у всех подряд в системе нет намеренно. Но у части работы
// дата есть на самом деле — релиз, демонстрация, договор, — и вопрос
// «успеваем ли к четвергу» про неё считали в уме.
test('дата обязательства ставится, видна и отбирается', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с обязательствами')
  await addCard(page, 'Очередь', 'К релизу')
  await addCard(page, 'Очередь', 'Без обязательств')

  // Дата ставится в панели: она есть не у всякой работы, и место
  // на карточке под неё не держат.
  await cardIn(page, 'Очередь', 'К релизу').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «К релизу»/)
  // Дата собирается по местным частям, а не через toISOString:
  // полночь по местному времени в UTC — это ещё вчера, и «завтра»
  // превращалось в «сегодня».
  const tomorrow = new Date()
  tomorrow.setDate(tomorrow.getDate() + 1)
  const iso = [
    tomorrow.getFullYear(),
    String(tomorrow.getMonth() + 1).padStart(2, '0'),
    String(tomorrow.getDate()).padStart(2, '0'),
  ].join('-')
  await panel.getByLabel('Дата обязательства').fill(iso)
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // На карточке срок назван словами: «завтра» читается с одного
  // взгляда, а дата — разбирается.
  const card = cardIn(page, 'Очередь', 'К релизу')
  await expect(card.getByTitle('Дата обязательства')).toContainText('завтра')

  // Отбор «Срок подходит» — про обещанное наружу.
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Срок подходит' }).check()
  await expect(cardIn(page, 'Очередь', 'Без обязательств')).toHaveCount(0)
  await expect(card).toBeVisible()
  expect(page.url()).toContain('due=1')
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'К релизу')).toBeVisible()
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Срок подходит' }).uncheck()

  // Обязательство снимается: «обязательства нет» и «дата неизвестна» —
  // разные вещи, и первое должно быть выразимо.
  await cardIn(page, 'Очередь', 'К релизу').click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel(/Карточка .* «К релизу»/).getByRole('button', { name: 'Снять' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await expect(cardIn(page, 'Очередь', 'К релизу').getByTitle('Дата обязательства')).toHaveCount(0)
})

// Связь «блокирует» была видна только в панели и только с той стороны,
// где её завели: почему карточка стоит, приходилось выяснять, открыв её,
// а сколько работы держит эта — не видно было вовсе.
test('зависимость видна с обеих сторон и проходится', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска зависимостей')
  await addCard(page, 'Очередь', 'Держит других')
  await addCard(page, 'Очередь', 'Ждёт очереди')

  // Связь заводится из панели той карточки, которая держит.
  await cardIn(page, 'Очередь', 'Держит других').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  const panel = page.getByLabel(/Карточка .* «Держит других»/)
  await panel.getByText('Связать с существующей карточкой').click()
  await panel.getByLabel('Вид связи').selectOption('blocks')
  await panel.getByLabel('Карточка для связи').selectOption({ label: 'Ждёт очереди' })
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Держащая называет, какую именно работу она держит, и переход
  // работает: число без имени отвечало бы «стоит кто-то», а вопрос —
  // «кто именно».
  const holder = cardIn(page, 'Очередь', 'Держит других')
  await expect(holder.getByText('Держит:')).toBeVisible()
  await holder.getByRole('button', { name: 'Ждёт очереди' }).click()
  await expect(page.getByRole('heading', { name: 'Ждёт очереди' })).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Ждущая называет того, кто её держит, и переход туда работает:
  // связь должна проходиться, а не только показываться.
  const waiting = cardIn(page, 'Очередь', 'Ждёт очереди')
  await expect(waiting.getByText('Ждёт:')).toBeVisible()
  await waiting.getByRole('button', { name: 'Держит других' }).click()
  await expect(page.getByRole('heading', { name: 'Держит других' })).toBeVisible()
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
  await expect(page.getByText('скрыто 2', { exact: true })).toBeVisible()

  // Опустевшая колонка отвечает, почему она пуста: «Пусто. Перетащите
  // карточку сюда» при двух скрытых отправляло искать поломку, которой
  // нет, — карточки лежат на месте.
  const queue = page.getByRole('region', { name: 'Очередь' })
  await expect(queue).toContainText('Под отбор ничего не подошло: скрыто 2')
  await expect(queue).not.toContainText('Перетащите карточку сюда')

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
  await toggleAssignee(page, mine)
  await expect(mine.locator('.avatar')).toBeVisible()

  // Работа без исполнителя и есть то, что теряется: её надо уметь
  // спросить отдельно.
  await openFilters(page)
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
  await toggleAssignee(page, mine)
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

  // Дорожки идут одна под другой и друг на друга не налезают.
  // Проверяется координатами, а не наличием: дорожки рисовались все
  // и прокрутка работала — просто каждая ужималась до нуля, а колонки
  // вылезали наружу, и заголовок следующей оказывался под карточками
  // предыдущей. Окно нарочно низкое: ужимались они только тогда, когда
  // вместе не влезали в высоту экрана, — на большом мониторе поломки
  // не видно, на ноутбуке она с первого взгляда.
  await page.setViewportSize({ width: 1280, height: 500 })
  const lanes = page.locator('.swimlane')
  const columns = await lanes.nth(0).locator('.columns').boundingBox()
  const nextTitle = await lanes.nth(1).locator('.swimlane-title').boundingBox()
  expect(columns!.y + columns!.height).toBeLessThanOrEqual(nextTitle!.y + 1)

  // По уровню дорожки идут шкалой, а не по алфавиту: «Высокий» встал бы
  // выше «Наивысшего», и читать их сверху вниз стало бы нечем.
  // «Без уровня» дорожки нет — уровень есть у каждой карточки.
  const urgent = cardIn(page, 'Очередь', 'Моя работа')
  await urgent.hover()
  await urgent.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Наивысший приоритет' }).click()
  await page.getByLabel('Группировка').selectOption('priority')
  await expect(page.locator('.swimlane-title')).toHaveText(['Наивысший', 'Средний'])

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
  await expect(done.getByRole('button', { name: 'Завести карточку' })).toHaveCount(0)

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

  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // По номеру — тоже. Номер обещан именем, которое вводят в поиск,
  // а палитра смотрела только название и приписку: «ДОСК-2» не находил
  // ничего, хотя то же самое поле над доской находило.
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Поиск и команды' }).fill('ДОСК-2')
  await expect(page.getByRole('option', { name: /Договор аренды/ })).toHaveCount(1)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: 'Договор аренды' })).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

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
  await expect(toggle).toHaveAccessibleName('Отбор: включено 1')

  await toggle.click()
  await expect(page.getByRole('checkbox', { name: 'Заблокированные' })).toBeHidden()
  await expect(toggle).toHaveAccessibleName('Отбор: включено 1')
  await phone.close()
})

test('подзадачи раскрываются прямо с доски', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска разбиения')
  await addCard(page, 'Очередь', 'Собрать отчёт')

  const parent = cardIn(page, 'Очередь', 'Собрать отчёт')
  await parent.getByRole('button', { name: 'Собрать отчёт' }).click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Свести цифры')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await expect(page.getByRole('button', { name: 'Свести цифры' }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

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

  // В строке части видно, кто её делает и сколько там разговора: части
  // одной карточки почти всегда лежат на разных людях, а обсуждение
  // у каждой своё.
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «Свести цифры»/)
  await panel.getByLabel('Добавить исполнителя').selectOption({ index: 1 })
  await page.getByRole('tab', { name: 'Обсуждение' }).click()
  await panel.getByPlaceholder('Написать в обсуждение').fill('Взял на себя')
  await panel.getByRole('button', { name: 'Отправить' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  const row = parent.locator('.subtask').filter({ hasText: 'Свести цифры' })
  await expect(row.locator('.avatar')).toHaveCount(1)
  await expect(row.getByTitle('Реплик в обсуждении: 1')).toBeVisible()

  // Кого спрашивать — видно и не раскрывая список.
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(parent.getByTitle('На кого разложены части').locator('.avatar')).toHaveCount(1)
})

// Часть, которая стоит, останавливает и целое: разбили работу, одна
// часть упёрлась — задача не идёт. Знала об этом только сама часть,
// а с доски родитель выглядел идущим, и отбор «что не идёт» его
// не показывал.
test('заблокированная часть останавливает и родителя', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с упёршейся частью')
  await addCard(page, 'Очередь', 'Выпустить релиз')

  const parent = cardIn(page, 'Очередь', 'Выпустить релиз')
  await parent.getByRole('button', { name: 'Выпустить релиз' }).click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Прогнать нагрузочные')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await expect(page.getByRole('button', { name: 'Прогнать нагрузочные' }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Часть открывается из родителя — отдельной карточкой в колонке
  // она не стоит, — и блокируется в своей панели.
  await parent.getByRole('button', { name: /Подзадачи/ }).click()
  await parent.getByRole('button', { name: 'Прогнать нагрузочные' }).click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const partPanel = page.getByLabel(/Карточка .* «Прогнать нагрузочные»/)
  await partPanel.getByRole('button', { name: 'Заблокировать…' }).click()
  await partPanel.getByLabel('Причина блокировки').fill('стенд лежит')
  await partPanel.getByLabel('Причина блокировки').press('Enter')
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // На родителе видно и то, что он стоит, и почему.
  await expect(parent.getByText('Часть заблокирована: стенд лежит')).toBeVisible()

  // И отбор «что не идёт» его показывает.
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Заблокированные' }).check()
  await expect(cardIn(page, 'Очередь', 'Выпустить релиз')).toBeVisible()
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Заблокированные' }).uncheck()

  // Часть отпустили — родитель пошёл дальше.
  await parent.getByRole('button', { name: 'Прогнать нагрузочные' }).click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await partPanel.getByRole('button', { name: 'Снять блокировку' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await expect(parent.getByText(/Часть заблокирована/)).toHaveCount(0)
})

// Работу разбивают тогда же, когда на неё смотрят: до сих пор за этим
// приходилось открывать панель и искать вкладку.
test('часть заводится прямо с доски — кнопкой в списке и пунктом меню', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска разбиения с доски')
  await addCard(page, 'Очередь', 'Собрать отчёт')
  const parent = cardIn(page, 'Очередь', 'Собрать отчёт')

  // Первая часть — пунктом меню: списка ещё нет, и кнопке в нём взяться
  // неоткуда.
  await parent.hover()
  await parent.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Завести подзадачу' }).click()
  await parent.getByLabel('Название подзадачи').fill('Свести цифры')
  await parent.getByLabel('Название подзадачи').press('Enter')
  await expect(parent.getByRole('button', { name: 'Свести цифры' })).toBeVisible()

  // Вторая — кнопкой в конце списка.
  await parent.getByRole('button', { name: '+ Подзадача' }).click()
  await parent.getByLabel('Название подзадачи').fill('Проверить итог')
  await parent.getByLabel('Название подзадачи').press('Enter')
  await expect(parent.getByRole('button', { name: 'Проверить итог' })).toBeVisible()
  await expect(parent.getByRole('button', { name: /Подзадачи: готово 0 из 2/ })).toBeVisible()

  // И то и другое легло в базу, а не в память вкладки.
  await page.reload()
  const again = cardIn(page, 'Очередь', 'Собрать отчёт')
  await expect(again.getByRole('button', { name: /Подзадачи: готово 0 из 2/ })).toBeVisible()
})

// Полоса разбиения — не украшение, а ответ на «как далеко ушла работа»,
// и читают его с полосы, а не с числа рядом. На доске полоса собрана
// из двух `span` внутри кнопки: строчному элементу ширина не назначается,
// и заливка не рисовалась вовсе — при любой доле полоса стояла пустой.
// В панели те же классы висят на `div`, там всё рисовалось, и потому
// расхождение долго не было видно.
test('полоса разбиения заливается, а не только число рядом с ней', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с полосой')
  await addCard(page, 'Очередь', 'Собрать отчёт')
  const parent = cardIn(page, 'Очередь', 'Собрать отчёт')

  await parent.hover()
  await parent.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Завести подзадачу' }).click()
  await parent.getByLabel('Название подзадачи').fill('Свести цифры')
  await parent.getByLabel('Название подзадачи').press('Enter')
  await expect(parent.getByRole('button', { name: /Подзадачи: готово 0 из 1/ })).toBeVisible()

  const filled = () =>
    parent.locator('.progress-fill').evaluate((el) => {
      const bar = (el.parentElement as HTMLElement).getBoundingClientRect().width
      return bar > 0 ? el.getBoundingClientRect().width / bar : 0
    })

  expect(await filled(), 'пустая доля — пустая полоса').toBe(0)

  await parent.getByRole('checkbox', { name: 'Сделана: Свести цифры' }).click()
  await expect(parent.getByRole('button', { name: /Подзадачи: готово 1 из 1/ })).toBeVisible()
  // Через опрос: полоса доезжает переходом в двести миллисекунд,
  // и замер сразу после нажатия ловит её на полпути.
  await expect
    .poll(filled, { message: 'вся работа сделана — полоса залита целиком' })
    .toBeGreaterThan(0.9)
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

  await page.getByRole('tab', { name: 'Задачи' }).click()
  await expect(page.getByRole('heading', { name: 'Подзадачи' })).toBeVisible()

  await page.getByRole('tab', { name: 'История' }).click()
  await expect(page.getByRole('heading', { name: 'История' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Подзадачи' })).toHaveCount(0)

  // Вкладка не запоминается между карточками: заглянувший в историю
  // одной задачи открывает следующую не затем, чтобы читать историю.
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
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
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

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
  // Карточка открывается обсуждением; подзадачи живут на «Задачах».
  await page.getByRole('tab', { name: 'Задачи' }).click()

  // Название — всё, что спрашивают: подзадача это обычная карточка,
  // и заводится она тем же движением, что и карточка в колонке.
  await page.getByLabel('Название подзадачи').fill('Прогнать тесты')
  await page.getByRole('button', { name: 'Подзадача' }).click()

  // Она сразу видна в списке подзадач.
  await expect(page.getByRole('complementary').getByText('Прогнать тесты')).toBeVisible()
  await expect(page.getByRole('progressbar', { name: 'Готово 0 из 1', exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  // Отдельной карточкой в колонке она при этом не стоит: часть —
  // это часть, а колонка из трёх задач не должна выглядеть колонкой
  // из десяти. Внутри родителя она видна и оттуда же открывается.
  await expect(cardIn(page, 'Очередь', 'Прогнать тесты')).toHaveCount(0)
  const parent = cardIn(page, 'Очередь', 'Выпустить релиз')
  await parent.getByRole('button', { name: /Подзадачи: готово 0 из 1/ }).click()
  await expect(parent.getByRole('button', { name: 'Прогнать тесты' })).toBeVisible()

  // И переживает перезагрузку — то есть связь легла в базу, а не
  // в память вкладки.
  await page.reload()
  await cardIn(page, 'Очередь', 'Выпустить релиз').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await expect(page.getByRole('progressbar', { name: 'Готово 0 из 1', exact: true })).toBeVisible()

  // Связь проходится в обе стороны: из родителя — в подзадачу,
  // из подзадачи — обратно. До этого связь было видно, но пройти по ней
  // можно было только поиском по доске.
  await page.getByRole('complementary').getByRole('button', { name: 'Прогнать тесты' }).click()
  await expect(page.getByRole('heading', { name: 'Прогнать тесты' })).toBeVisible()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  // Со вкладки «Задачи», а не с пути до корня над номером: там та же
  // карточка названа второй раз.
  await page.getByLabel('Задачи', { exact: true }).getByRole('button', { name: 'Выпустить релиз' }).click()
  await expect(page.getByRole('heading', { name: 'Выпустить релиз' })).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Родитель показывает разбиение полосой, и она же раскрывает
  // подзадачи: мера и путь внутрь неё — одно управление, поэтому мера
  // читается вслух как имя кнопки, а не как отдельная полоса.
  await expect(
    cardIn(page, 'Очередь', 'Выпустить релиз').getByRole('button', {
      name: /Подзадачи: готово 0 из 1/,
    }),
  ).toBeVisible()

  // Отбор, спрятавший родителя, возвращает часть в колонку: работа
  // не должна исчезать с доски. Своей строкой она называет, чья она
  // часть, и по этой строке можно перейти к родителю.
  await page.getByPlaceholder('Найти карточку').fill('Прогнать тесты')
  const shown = cardIn(page, 'Очередь', 'Прогнать тесты')
  await expect(shown).toBeVisible()
  await expect(shown.getByRole('button', { name: 'Выпустить релиз' })).toBeVisible()
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
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // На доске видны оба — и назначение пережило перезагрузку.
  // Точное совпадение подписи: имя стоит и на самом аватаре,
  // и в подсказке стопки, которой её правят.
  await page.reload()
  const card = cardIn(page, 'Очередь', 'Делать вдвоём')
  await expect(card.getByTitle('Проверяющий', { exact: true })).toBeVisible()
  await expect(card.getByTitle('Иван Петров', { exact: true })).toBeVisible()

  // Фильтр «на мне» показывает работу, о которой договорились вдвоём:
  // иначе один из двоих не найдёт её у себя.
  await openFilters(page)
  await page.getByLabel('Исполнитель').selectOption({ label: 'Иван Петров' })
  await expect(card).toBeVisible()

  await openFilters(page)
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
  await page.getByRole('tab', { name: 'Задачи' }).click()

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
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
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
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await expect(page.getByText('Работу не взяли')).toBeVisible()
  await expect(page.getByText(/которого вам не видно/)).toHaveCount(0)
})

test('удаление насовсем спрашивает и называет карточку', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с дублями')
  await addCard(page, 'Очередь', 'Дубль сметы')

  const card = cardIn(page, 'Очередь', 'Дубль сметы')
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Удалить навсегда' }).click()

  // Подтверждение называет то, что исчезнет: вопрос «вы уверены?»
  // без имени отвечают не читая.
  const dialog = page.locator('dialog')
  await expect(dialog.getByText(/«Дубль сметы» исчезнет/)).toBeVisible()

  // Отмена ничего не делает — иначе диалог был бы декорацией.
  await dialog.getByRole('button', { name: 'Отмена' }).click()
  await expect(card).toBeVisible()

  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Удалить навсегда' }).click()
  await dialog.getByRole('button', { name: 'Удалить навсегда' }).click()
  await expect(card).toHaveCount(0)

  // И не возвращается перезагрузкой: удаление, в отличие от архива,
  // не имеет обратного действия.
  await page.reload()
  await expect(cardIn(page, 'Очередь', 'Дубль сметы')).toHaveCount(0)
})

test('закрытая итерация остаётся на экране и отвечает отчётом', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со спринтами')
  await addCard(page, 'Очередь', 'Смета по объекту')
  await addCard(page, 'Очередь', 'Регламент приёмки')

  await page.getByRole('button', { name: '+ итерация' }).click()
  await page.getByPlaceholder('Название').fill('Неделя 34')
  await page.getByLabel('Начало').fill('2026-08-10')
  await page.getByLabel('Конец').fill('2026-08-16')
  await page.getByPlaceholder('Цель').fill('Закрыть смету')
  await page.getByRole('button', { name: 'Завести итерацию', exact: true }).click()
  await expect(page.getByRole('button', { name: /^Неделя 34 ·/ })).toBeVisible()

  // Обе карточки в итерации.
  for (const title of ['Смета по объекту', 'Регламент приёмки']) {
    await cardIn(page, 'Очередь', title).click()
    await page.getByRole('tab', { name: 'Работа' }).click()
    await page.getByLabel('Итерация карточки').selectOption({ label: 'Неделя 34' })
    // Именно кнопка панели: у итерации рядом своя, «закрыть».
    await page.getByRole('complementary').getByRole('button', { name: 'Закрыть', exact: true }).click()
  }

  // Одна доведена до конца.
  const card = cardIn(page, 'Очередь', 'Смета по объекту')
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Перенести в «Готово»' }).click()
  await expect(page.getByRole('region', { name: 'Готово' }).getByText('Смета по объекту')).toBeVisible()

  // Закрываем итерацию — и она не исчезает с экрана: закрытие делается
  // ради ответа «что было в спринте», а до этого в этот же миг ответ
  // и пропадал.
  await page.getByRole('button', { name: 'Закрыть итерацию «Неделя 34»' }).click()
  // В диалоге кнопка называет то, что случится: «Закрыть» рядом
  // с «Закрыть» панели означало бы то «уйти отсюда», то «заморозить
  // состав навсегда».
  await page.locator('dialog').getByRole('button', { name: 'Закрыть итерацию' }).click()
  await expect(page.getByText('Закрытые:')).toBeVisible()

  await page.getByRole('button', { name: 'Неделя 34', exact: true }).click()
  const panel = page.getByRole('complementary')
  await expect(panel.getByText('1 из 2')).toBeVisible()
  await expect(panel.getByText(/состав застыл/)).toBeVisible()
  // Срок итерации словами: в шапке отчёта стояло
  // «2026-08-10—2026-08-16» — та же машинная запись, которую убрали
  // из шапки доски.
  await expect(panel.getByText(/10—16 авг/)).toBeVisible()
  await expect(panel.getByText(/2026-08-10/)).toHaveCount(0)
  await expect(panel.getByText(/Смета по объекту/)).toBeVisible()
})

test('убранная карточка достижима из архива и возвращается', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с архивом')
  await addCard(page, 'Очередь', 'Отменённая закупка')

  const card = cardIn(page, 'Очередь', 'Отменённая закупка')
  await card.hover()
  await card.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Убрать в архив' }).click()
  await expect(cardIn(page, 'Очередь', 'Отменённая закупка')).toHaveCount(0)

  // Перезагрузка уносит всплывающее уведомление — единственный путь
  // к убранной карточке, который был до архива.
  await page.reload()
  await page.getByRole('button', { name: 'Архив' }).click()
  const panel = page.getByRole('complementary')
  await expect(panel.getByText(/Отменённая закупка/)).toBeVisible()
  await expect(panel.getByText(/Очередь · убрана/)).toBeVisible()

  await panel.getByRole('button', { name: 'Вернуть' }).click()
  await expect(cardIn(page, 'Очередь', 'Отменённая закупка')).toBeVisible()
  await expect(panel.getByText('Архив пуст.')).toBeVisible()
})

test('таблица — второй вид на те же данные, и он присылается ссылкой', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со списком')
  await addCard(page, 'Очередь', 'Согласовать смету')
  await addCard(page, 'Очередь', 'Обновить регламент')

  await pickView(page, 'Таблица')
  const rows = page.locator('.board-table tbody tr')
  await expect(rows).toHaveCount(2)
  // Колонок на экране больше нет: прятать их стилями значило бы держать
  // в разметке невидимые карточки.
  await expect(page.getByRole('region', { name: 'Очередь' })).toHaveCount(0)

  // Столбцы — то, что сравнивают между строками. Срок и итерация
  // стоят у каждой строки, даже пустые: пустое место сравнивать
  // не с чем.
  await expect(page.getByRole('columnheader', { name: 'Срок' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'Итерация' })).toBeVisible()

  // Вид и сортировка живут в адресе и переживают перезагрузку.
  await page.getByLabel('Сортировка').selectOption('column')
  await expect(page).toHaveURL(/view=table.*sort=column/)
  await page.reload()
  await expect(page.locator('.board-table tbody tr')).toHaveCount(2)
  await page.getByLabel('Сортировка').selectOption('due')
  await expect(page).toHaveURL(/sort=due/)

  // Правка по месту: перенос прямо из строки.
  await rows.first().getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Перенести в «Готово»' }).click()
  await expect(page.locator('.board-table tbody tr td', { hasText: 'Готово' })).toHaveCount(1)

  // Возврат к доске — тем же переключателем.
  await pickView(page, 'Доска')
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
})

test('изменения — третий вид, с отбором «только про меня»', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с лентой')
  await addCard(page, 'Очередь', 'Согласовать смету')
  await addCard(page, 'Очередь', 'Чужая работа')

  // Одна карточка становится моей.
  const mine = cardIn(page, 'Очередь', 'Согласовать смету')
  await toggleAssignee(page, mine)

  await pickView(page, 'Изменения')
  const feed = page.locator('.feed li')
  await expect(feed.first()).toBeVisible()
  const all = await feed.count()
  // Ищем в самой ленте: то же название лежит и в списке палитры.
  await expect(feed.getByRole('button', { name: 'Чужая работа' }).first()).toBeVisible()

  // Отбор оставляет только то, что относится ко мне.
  await page.getByLabel('Только про меня').check()
  await expect(feed.getByRole('button', { name: 'Чужая работа' })).toHaveCount(0)
  await expect(feed.first()).toBeVisible()
  expect(await feed.count()).toBeLessThan(all)

  // Из ленты открывается карточка.
  await feed.first().getByRole('button', { name: 'Согласовать смету' }).click()
  await expect(page.getByRole('heading', { name: 'Согласовать смету' })).toBeVisible()
})

test('видно, сколько на ком висит', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с загрузкой')
  await addCard(page, 'Очередь', 'Первая')
  await addCard(page, 'Очередь', 'Вторая')

  const load = page.locator('.workload')
  // Пока никто ничего не делает, сводке нечего показывать.
  await expect(load).toHaveCount(0)

  for (const title of ['Первая', 'Вторая']) {
    await toggleAssignee(page, cardIn(page, 'Очередь', title))
  }
  await expect(load.locator('.workload-item')).toHaveCount(1)
  await expect(load).toContainText('2')

  // Сделанное не считается нагрузкой: это уже не работа.
  const done = cardIn(page, 'Очередь', 'Первая')
  await done.hover()
  await done.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Перенести в «Готово»' }).click()
  await expect(load).toContainText('1')

  // Человек назван именем, и нажатие по нему отбирает доску по нему;
  // повторное — снимает отбор.
  await expect(load).toContainText('В работе:')
  const me = load.getByRole('button', { name: /^Показать только его карточки — Проверяющий/ })
  await expect(me).toContainText('Проверяющий')
  await me.click()
  await expect(me).toHaveAttribute('aria-pressed', 'true')
  await expect(page).toHaveURL(/assignee=/)
  await me.click()
  await expect(me).toHaveAttribute('aria-pressed', 'false')
  await expect(page).not.toHaveURL(/assignee=/)
})

/**
 * Меню карточки у нижнего края достаётся целиком.
 *
 * Колонка прокручивается сама, и список, нарисованный внутри неё,
 * у нижней карточки обрезался — причём хуже, чем просто обрезался:
 * пункт за краем не был не виден, он не нажимался, потому что в его
 * точке лежала колонка. «Убрать в архив» и «Удалить навсегда» стоят
 * последними, и недостижимыми оказывались именно они.
 *
 * Проверяется попаданием (`elementFromPoint`), а не видимостью:
 * в разметке обрезанный пункт остаётся на месте и «виден» кому угодно,
 * кроме указателя.
 */
test.describe('низ колонки', () => {
  // Окно пониже: на высоком экране колонка кончается далеко от края,
  // и обрезать список нечему.
  test.use({ viewport: { width: 942, height: 700 } })

  test('меню нижней карточки нажимается целиком', async ({ page }) => {
    await register(page)
    await createBoard(page, 'Доска с длинной колонкой')
    for (const title of ['Первая', 'Вторая', 'Третья']) {
      await addCard(page, 'Очередь', title)
    }

    const card = cardIn(page, 'Очередь', 'Третья')
    await card.hover()
    await card.getByRole('button', { name: /Действия карточки/ }).click()

    const items = page.getByRole('menuitem')
    const count = await items.count()
    expect(count).toBeGreaterThan(4)

    for (let i = 0; i < count; i++) {
      const item = items.nth(i)
      const label = (await item.textContent())?.trim()
      const reachable = await item.evaluate((el) => {
        const box = el.getBoundingClientRect()
        const under = document.elementFromPoint(
          box.left + box.width / 2,
          box.top + box.height / 2,
        )
        return {
          вОкне: box.top >= 0 && box.bottom <= window.innerHeight,
          нажимается: under === el || el.contains(under),
        }
      })
      expect(reachable, `пункт «${label}»`).toEqual({ вОкне: true, нажимается: true })
    }
  })
})

/**
 * Лимит колонки задаётся там, где о нём сказано.
 *
 * В разметке колонки стояло «Жёсткий лимит (сначала задайте лимит)»,
 * а задать его оттуда было нечем: правился он нажатием по счётчику
 * в шапке колонки, и про это не было сказано нигде — человек читал
 * указание и не находил, чем его выполнить.
 */
test('лимит колонки задаётся из её разметки', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с лимитом')

  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Разметка колонки «Очередь»' }).click()

  // Пока лимита нет, жёсткость не предлагают — и говорят, где взять.
  const hard = queue.getByRole('checkbox', { name: /Жёсткий лимит/ })
  await expect(hard).toBeDisabled()
  await expect(queue.getByText(/сначала задайте лимит выше/)).toBeVisible()

  await queue.getByRole('spinbutton', { name: /Лимит карточек в колонке/ }).fill('3')
  await queue.getByRole('spinbutton', { name: /Лимит карточек в колонке/ }).blur()

  // Лимит доехал: счётчик в шапке показывает его, жёсткость доступна.
  await expect(queue.getByRole('button', { name: /0\s*\/\s*3/ })).toBeVisible()
  await expect(hard).toBeEnabled()

  // И переживает перезагрузку — то есть лежит на сервере.
  await page.reload()
  await expect(
    page.getByRole('region', { name: 'Очередь' }).getByRole('button', { name: /0\s*\/\s*3/ }),
  ).toBeVisible()
})

/**
 * Escape закрывает верхний слой, а не всё разом.
 *
 * Диалог поверх панели закрывался силами браузера, а тот же Escape
 * доходил до окна и закрывал панель под ним: человек отвечал «отмена»
 * на вопрос и терял открытую карточку заодно, а фокус после этого
 * оставался на `body` — дальше клавиатуре идти было неоткуда.
 */
test('Escape закрывает верхний слой, а не всё разом', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со слоями')
  await addCard(page, 'Очередь', 'Смета')
  await addCard(page, 'Очередь', 'Договор')

  // Панель карточки открыта, доска за ней остаётся рабочей — так
  // и задумано, поэтому меню соседней карточки отсюда достижимо.
  await cardIn(page, 'Очередь', 'Смета').getByRole('button', { name: 'Смета' }).click()
  await expect(page.getByRole('complementary')).toBeVisible()

  const other = cardIn(page, 'Очередь', 'Договор')
  await other.hover()
  await other.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Удалить навсегда' }).click()
  const dialog = page.locator('dialog')
  await expect(dialog.getByText(/«Договор» исчезнет/)).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(dialog.getByText(/«Договор» исчезнет/)).toBeHidden()
  await expect(page.getByRole('complementary')).toBeVisible()
  // Обе карточки на месте: Escape — это отмена, а не «нет, но всё-таки».
  await expect(cardIn(page, 'Очередь', 'Договор')).toBeVisible()
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe('BODY')
})

/**
 * Заведённая карточка видна сразу.
 *
 * Она встаёт в конец колонки, а конец колонки бывает за краем экрана:
 * форма закрывалась, и на экране не менялось ничего — человек заводил
 * её второй раз, думая, что промахнулся.
 */
test('новая карточка попадает на глаза, а не в конец за краем', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска длинной колонки')
  for (let i = 1; i <= 12; i++) await addCard(page, 'Очередь', `Задача ${i}`)

  const last = cardIn(page, 'Очередь', 'Задача 12')
  await expect(last).toBeInViewport()
})

/**
 * Список досок отвечает тем же, что и дерево подразделений.
 *
 * В дереве у доски написано «ПЛАТ · своей команде», а список знал одно
 * название — и выбирать доску приходилось по нему, при том что «эту
 * видят все или только мы» и есть вопрос, ради которого в список
 * заглядывают.
 */
test('в списке досок видны ключ, видимость и объём', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Платформа')
  await addCard(page, 'Очередь', 'Первая')
  await addCard(page, 'Очередь', 'Вторая')

  await page.getByRole('button', { name: 'Все доски' }).click()
  const row = page.locator('.board-list li').filter({ hasText: 'Платформа' })
  await expect(row).toContainText('ПЛАТ')
  await expect(row).toContainText('всей организации')
  await expect(row).toContainText('2 карточки')
})

// Ширину колонки тянут мышью (ROADMAP 28.2): в «В работе» длинные
// названия и метки, в «Готово» — один заголовок, и одна ширина на все
// одинаково не подходит обеим. Ширина — личное дело смотрящего и живёт
// в браузере, поэтому переживает перезагрузку.
test('ширина колонки тянется мышью и переживает перезагрузку', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с широкой колонкой')
  const doing = page.getByRole('region', { name: 'В работе' })
  const before = (await doing.boundingBox())!.width

  const handle = page.getByRole('separator', { name: 'Ширина колонки «В работе»' })
  const box = (await handle.boundingBox())!
  // Зона нажатия — не меньше цели нажатия: четырёхпиксельную полосу
  // пальцем не берут.
  expect(box.width).toBeGreaterThanOrEqual(24)
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2 + 120, box.y + box.height / 2, { steps: 6 })
  await page.mouse.up()
  const wider = (await doing.boundingBox())!.width
  expect(wider).toBeGreaterThan(before + 100)

  // Соседняя колонка не тронута: ширина у каждой своя.
  const queue = page.getByRole('region', { name: 'Очередь' })
  expect(Math.round((await queue.boundingBox())!.width)).toBe(Math.round(before))

  await page.reload()
  await expect(page.getByRole('region', { name: 'В работе' })).toBeVisible()
  expect(Math.round((await page.getByRole('region', { name: 'В работе' }).boundingBox())!.width)).toBe(
    Math.round(wider),
  )

  // Двойной щелчок возвращает исходную.
  await page.getByRole('separator', { name: 'Ширина колонки «В работе»' }).dblclick()
  await expect
    .poll(async () => Math.round((await page.getByRole('region', { name: 'В работе' }).boundingBox())!.width))
    .toBe(Math.round(before))

  // Свёрнутая колонка не тянется: ручки у неё нет.
  await page.getByRole('region', { name: 'Готово' }).getByRole('button', { name: 'Свернуть «Готово»' }).click()
  await expect(page.getByRole('separator', { name: 'Ширина колонки «Готово»' })).toHaveCount(0)
})

// У блокировки есть срок, и он сам её снимает (ROADMAP 28.1). Снятие
// проходом сервера здесь не ждётся — его проверяет сервер; здесь то,
// что видит человек: срок ставится вместе с причиной, виден на карточке
// до того, как вышел, правится во время блокировки и убирается.
test('блокировка со сроком: видна заранее, правится и становится бессрочной', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со сроками')
  await addCard(page, 'Очередь', 'Ждём поставку')
  const card = cardIn(page, 'Очередь', 'Ждём поставку')

  const local = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  const inThreeDays = new Date()
  inThreeDays.setDate(inThreeDays.getDate() + 3)
  inThreeDays.setHours(18, 0, 0, 0)

  await card.click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «Ждём поставку»/)
  await panel.getByRole('button', { name: 'Заблокировать…' }).click()
  await panel.getByLabel('Причина блокировки').fill('ждём поставку')
  // Срок необязателен и так и назван: пустое поле — «пока не снимут».
  await panel.getByLabel('Снимется само (необязательно)').fill(local(inThreeDays))
  await panel.getByLabel('Причина блокировки').press('Enter')

  const day = inThreeDays.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
  await expect(panel.getByText(`до ${day}, 18:00`)).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await expect(card.getByText(new RegExp(`до ${day}, 18:00`))).toBeVisible()

  // Меньше суток до срока — отбор «истекает» его находит.
  const soon = new Date(Date.now() + 5 * 3_600_000)
  await card.click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await panel.getByLabel('Срок блокировки').fill(local(soon))
  await panel.getByLabel('Срок блокировки').press('Enter')
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Блокировка истекает' }).check()
  await expect(cardIn(page, 'Очередь', 'Ждём поставку')).toBeVisible()
  await openFilters(page)
  await page.getByRole('checkbox', { name: 'Блокировка истекает' }).uncheck()

  // Бессрочная — кнопкой, а не стиранием поля: стёртое поле читается
  // как «не дописал», а не как решение.
  await card.click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await panel.getByRole('button', { name: 'Сделать бессрочной' }).click()
  await expect(panel.getByText('Бессрочная — пока не снимут.')).toBeVisible()
  await expect(panel.getByText(/Снимется сама/)).toHaveCount(0)
  await expect(panel.getByText('Заблокирована', { exact: true })).toBeVisible()
})

// Боковая панель оставляет доску рабочей — так обещано в стилях
// и в Panel.tsx, а код обещания не держал: панель ложилась поверх
// доски, и при открытой карточке не нажимались «Таблица», «Изменения»,
// «Поток», «Архив», тема, группировка (проход глазами 20.09.2026,
// 7–10 перекрытых органов на ширинах 1280–2560). «Не не видно,
// а не нажимается» — ровно тот облик, что у меню нижней карточки,
// и проверка устроена так же: каждый орган управления доской при
// открытой панели отвечает на elementFromPoint собой.
test('боковая панель не перекрывает управление доской', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с панелью')
  await addCard(page, 'Очередь', 'Открытая')
  await addCard(page, 'Очередь', 'Соседняя')

  for (const width of [1280, 1440, 1920]) {
    await page.setViewportSize({ width, height: 800 })
    await cardIn(page, 'Очередь', 'Открытая').click()
    const panel = page.locator('.panel-side')
    await expect(panel).toBeVisible()
    await expect(page.getByLabel('Как показывать панель')).toHaveValue('side')

    const covered = await page.evaluate(() => {
      const side = document.querySelector('.panel-side')!
      const controls = [
        ...document.querySelectorAll<HTMLElement>(
          '.board-screen button, .board-screen select, .board-screen input, .board-screen a[href], .board-screen [role="separator"]',
        ),
      ].filter(
        (el) =>
          !side.contains(el) &&
          // Колонки листаются вбок сами: ушедшее за край колонок
          // достают прокруткой, а не перекрытием.
          !el.closest('.columns') &&
          el.getBoundingClientRect().width > 1,
      )
      return controls
        .filter((el) => {
          const box = el.getBoundingClientRect()
          const under = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2)
          return !(under === el || el.contains(under))
        })
        .map((el) => el.getAttribute('aria-label') || el.textContent?.trim() || el.tagName)
    })
    expect(covered, `перекрыто при ширине ${width}`).toEqual([])

    // Сообщение о действии на доске не ложится поверх панели: у него
    // свой слой, и он выше панели, — значит, место ему нужно рядом.
    const neighbour = cardIn(page, 'Очередь', 'Соседняя')
    await neighbour.hover()
    await neighbour.getByRole('button', { name: /Действия карточки/ }).click()
    await page.getByRole('menuitem', { name: 'Убрать в архив' }).click()
    const toast = page.locator('.toast').last()
    await expect(toast).toBeVisible()
    const [t, p] = [(await toast.boundingBox())!, (await panel.boundingBox())!]
    expect(t.x + t.width, `сообщение на панели при ширине ${width}`).toBeLessThanOrEqual(p.x)
    await toast.getByRole('button', { name: 'Вернуть' }).click()
    await expect(cardIn(page, 'Очередь', 'Соседняя')).toBeVisible()

    await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
    await expect(panel).toHaveCount(0)
  }
})

// Шапка доски — не стена органов управления (разбор 21.09.2026).
// Над первой карточкой стояло около двадцати органов в четыре-пять
// рядов: отборы, тема, плотность, загрузка, виды, итерации. Доска
// начиналась с середины экрана, а на телефоне первая карточка
// оказывалась у его нижнего края. Меряется то, ради чего переделка:
// где начинается доска и сколько органов стоит над ней.
test('шапка доски не съедает экран', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска со строгой шапкой')
  await addCard(page, 'Очередь', 'Первая')

  const above = () =>
    page.evaluate(() => {
      const columns = document.querySelector('.columns')!.getBoundingClientRect().top
      const controls = [
        ...document.querySelectorAll<HTMLElement>(
          '.board-screen button, .board-screen select, .board-screen input',
        ),
      ].filter((el) => {
        const box = el.getBoundingClientRect()
        return box.width > 1 && box.bottom <= columns && !el.closest('.columns')
      })
      return { columns: Math.round(columns), controls: controls.length }
    })

  await page.setViewportSize({ width: 1440, height: 900 })
  const wide = await above()
  // 200 → 250: отбор стал открытой панелью, а поиск ушёл строкой под
  // него (решение владельца 22.09.2026). Замер — 205 на рабочей машине
  // и 243 на машине проверок GitHub: шрифт там шире, и строка
  // инструментов переносится на строку больше. Порог — по худшему.
  expect(wide.columns, 'доска на 1440 начинается ниже, чем надо').toBeLessThanOrEqual(250)
  // До переделки — 18: отборы, тема и плотность стояли каждый своим
  // органом. После — 13: назад, видимость, оформление, три вида, поиск,
  // «Отбор», группировка, палитра, «Поток», «Архив», «+ итерация».
  // 14 — с колокольчиком уведомлений (этап 29, 22.09.2026): строки он
  // не добавляет, стоит в хвосте шапки рядом с именем, а «Справка» —
  // ссылка и в счёт не идёт. Высоту шапки держит проверка выше.
  // 16 — отбор стал основной панелью и открыт на широком экране
  // (решение владельца 22.09.2026: «Отбор сделать основной панелью»):
  // исполнитель и четыре флажка на виду, кнопки «Отбор» и трёх кнопок
  // вида нет — вид стал одним списком. Замер — 16.
  expect(wide.controls, 'органов над доской на 1440').toBeLessThanOrEqual(16)

  await page.setViewportSize({ width: 360, height: 760 })
  const first = cardIn(page, 'Очередь', 'Первая')
  await expect(first).toBeVisible()
  const top = (await first.boundingBox())!.y
  // Замер 21.09.2026 на свежей доске с коротким названием: первая
  // карточка на 515 пикселях из 760 до переделки и на 371 после.
  // Здесь название длинное и переносится на вторую строку — отсюда
  // порог в 60% высоты, который прежний код не проходил.
  expect(top, 'первая карточка на телефоне').toBeLessThanOrEqual(760 * 0.6)
})

// Раскрытый «Отбор» сдвигает доску на ряд вниз, и свернуть его можно
// было только той же кнопкой: Escape из флажка не делал ничего
// (найдено на подготовке показа 21.09.2026). Флажок берётся фокусом,
// а не щелчком — щелчок включил бы отбор и поменял доску.
test('Escape сворачивает «Отбор» и возвращает фокус на кнопку', async ({ page }) => {
  // Кнопка «Отбор» — только на узком экране: на широком отбор открыт.
  await page.setViewportSize({ width: 390, height: 844 })
  await register(page)
  await createBoard(page, 'Доска с отбором')

  const toggle = page.getByRole('button', { name: /^Отбор/ })
  await openFilters(page)
  const urgent = page.getByRole('checkbox', { name: 'Горит' })
  await urgent.focus()
  await page.keyboard.press('Escape')

  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(urgent).toBeHidden()
  await expect(toggle).toBeFocused()
})

// Необратимое спрашивает (правило проекта). Отзыв ключа и удаление
// реплики срабатывали с первого нажатия, хотя вернуть не дают ни то,
// ни другое (разбор интерфейса 21.09.2026). Отмена не трогает ничего,
// подтверждение делает дело.
test('отзыв ключа и удаление реплики спрашивают', async ({ page }) => {
  await register(page)

  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByLabel('Для чего ключ').fill('Обмен со складом')
  await page.getByRole('button', { name: 'Завести ключ', exact: true }).click()
  const revoke = page.getByRole('button', { name: 'Отозвать ключ «Обмен со складом»' })
  await expect(revoke).toBeVisible()

  await revoke.click()
  const dialog = page.getByRole('dialog', { name: 'Отозвать ключ?' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Отмена' }).click()
  await expect(revoke).toBeVisible()

  await revoke.click()
  await dialog.getByRole('button', { name: 'Отозвать ключ' }).click()
  await expect(revoke).toHaveCount(0)

  await page.getByRole('button', { name: 'Доски' }).click()
  await createBoard(page, 'Доска с репликой')
  await addCard(page, 'Очередь', 'Обсуждаемая')
  await cardIn(page, 'Очередь', 'Обсуждаемая').click()
  const panel = page.getByLabel(/Карточка .* «Обсуждаемая»/)
  await panel.getByPlaceholder('Написать в обсуждение').fill('Лишнее')
  await panel.getByRole('button', { name: 'Отправить' }).click()
  await expect(panel.getByText('Лишнее', { exact: true })).toBeVisible()

  await panel.getByRole('button', { name: 'Удалить', exact: true }).click()
  const ask = page.getByRole('dialog', { name: 'Удалить реплику?' })
  await ask.getByRole('button', { name: 'Отмена' }).click()
  await expect(panel.getByText('Лишнее', { exact: true })).toBeVisible()

  await panel.getByRole('button', { name: 'Удалить', exact: true }).click()
  await ask.getByRole('button', { name: 'Удалить реплику' }).click()
  await expect(panel.getByText('Реплика удалена', { exact: true })).toBeVisible()
})

// Личные настройки (ROADMAP 30.6): имя открывает их с клавиатуры, Escape
// возвращает фокус на имя, а выбранный язык живёт у человека и встречает
// его в другом браузере — там, где он ещё ничего не выбирал.
test('имя открывает личные настройки, а язык переезжает в другой браузер', async ({
  page,
  browser,
}) => {
  const who = await register(page)
  const name = page.getByRole('button', { name: /^Личные настройки/ })

  await name.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog', { name: 'Личные настройки' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('tab', { name: 'Язык' })).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(name).toBeFocused()

  await name.click()
  await page.getByRole('radio', { name: 'English' }).click()
  // Язык применяется перезагрузкой: экран приходит уже английским.
  await expect(page.getByPlaceholder('New board name')).toBeVisible()

  // Второй браузер с русским языком системы: до входа — русский экран,
  // после входа — язык, выбранный человеком.
  const other = await browser.newContext({ locale: 'ru-RU' })
  const second = await other.newPage()
  await second.goto('/')
  await second.getByLabel('Почта').fill(who.email)
  await second.getByLabel('Пароль').fill(who.password)
  await second.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(second.getByPlaceholder('New board name')).toBeVisible()
  await expect(second.getByRole('button', { name: /^Personal settings/ })).toBeVisible()
  await other.close()
})

// Справка с экрана (ROADMAP 30.3): «Справка» ведёт в раздел про тот
// экран, где человек сейчас, и страница по этому адресу существует.
test('справка открывается на разделе текущего экрана', async ({ page, context }) => {
  await register(page)
  await createBoard(page, 'Справочная')
  const help = page.getByRole('link', { name: 'Справка' })
  await help.hover()
  await expect(help).toHaveAttribute('href', '/help/ru/howto#board')

  await page.getByRole('button', { name: 'Поток' }).click()
  await help.hover()
  await expect(help).toHaveAttribute('href', '/help/ru/reference#flow')

  // F1 — то же самое, в новой вкладке, и раздел на месте.
  const [tab] = await Promise.all([context.waitForEvent('page'), page.keyboard.press('F1')])
  await tab.waitForLoadState()
  expect(new URL(tab.url()).pathname + new URL(tab.url()).hash).toBe('/help/ru/reference#flow')
  await expect(tab.locator('#flow')).toBeVisible()
  await expect(tab.getByRole('link', { name: '← Вернуться в Takt' })).toBeVisible()
})

// Уведомления (ROADMAP 29): упомянули — счётчик вырос — открыл —
// карточка на экране — счётчик ноль.
test('упоминание доходит колокольчиком и ведёт на карточку', async ({ page, browser }) => {
  await register(page)
  await createBoard(page, 'Доска с обсуждением')
  await addCard(page, 'Очередь', 'Обсудить смету')

  // Второй человек в организации — тот, кого позовут.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await page
    .getByRole('textbox', { name: 'Почта коллеги' })
    .fill(`zovut-${Math.random().toString(36).slice(2, 8)}@example.test`)
  await page.getByRole('button', { name: 'Пригласить', exact: true }).click()
  const invite = await page.locator('input[readonly]').first().inputValue()
  const second = await browser.newContext()
  const him = await second.newPage()
  await him.goto(invite.trim())
  await him.getByLabel('Как вас зовут').fill('Иван Петров')
  await him.getByLabel('Пароль').fill('parol12345')
  await him.getByRole('button', { name: /Принять|Присоединиться/ }).click()
  const bell = him.getByRole('button', { name: /^Уведомления/ })
  await expect(bell).toHaveAccessibleName('Уведомления')

  // Владелец зовёт его в обсуждение карточки.
  await page.goto('/')
  await openBoard(page, 'Доска с обсуждением')
  await cardIn(page, 'Очередь', 'Обсудить смету').click()
  await page.getByRole('tab', { name: 'Обсуждение' }).click()
  await page.getByRole('textbox', { name: 'Написать в обсуждение' }).fill('Посмотри, пожалуйста,')
  await page.getByRole('button', { name: 'Позвать в обсуждение' }).click()
  await page.getByRole('menuitem', { name: 'Иван Петров' }).click()
  await page.getByRole('button', { name: 'Отправить' }).click()

  // У него счётчик вырос сам, без перезагрузки — потоком по человеку.
  await expect(bell).toHaveAccessibleName('Уведомления: непрочитанных 1')
  await bell.click()
  await him.getByRole('link', { name: /упоминает вас в обсуждении/ }).click()
  await expect(him.getByRole('heading', { name: 'Обсудить смету' })).toBeVisible()
  await expect(him.getByRole('button', { name: /^Уведомления/ })).toHaveAccessibleName('Уведомления')
  await second.close()
})

test('таблица переезжает на доску: предпросмотр, перенос, повтор без двойников', async ({ page }) => {
  await register(page)
  const file = {
    name: 'Склад.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(
      'Заголовок;Колонка;Исполнитель;Оценка;Срок\n' +
        'Сверить остатки;В работе;nikto@example.test;3;30.09.2026\n' +
        'Заказать тару;Очередь;;;\n' +
        ';Очередь;;;\n',
    ),
  }
  await page.getByRole('link', { name: 'Перенести задачи из таблицы…' }).click()
  await page.getByLabel('Файл CSV или Excel').setInputFiles(file)

  // Сопоставление предложено само, предпросмотр говорит, что будет,
  // и называет потери: строку без заголовка и почту, которой нет.
  await expect(page.getByRole('combobox', { name: 'Поле карточки для колонки «Срок»' })).toHaveValue('due')
  await expect(page.getByText('Переедут 2 карточки из 3 строк.')).toBeVisible()
  // Ненайденный по почте назван в «Людях» и по умолчанию не переносится.
  await expect(page.getByRole('combobox', { name: 'Что сделать: nikto@example.test' })).toHaveValue('skip')
  await expect(page.getByText(/Строка 4:.*нет заголовка/)).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Название новой доски' })).toHaveValue('Склад')

  await page.getByRole('button', { name: 'Перенести 2 карточки' }).click()
  await expect(page.getByText('Перенесено: 2 карточки.')).toBeVisible()
  await page.getByRole('button', { name: 'Открыть доску' }).click()
  await expect(cardIn(page, 'В работе', 'Сверить остатки')).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Заказать тару')).toBeVisible()

  // Перенесённое помечено: в истории карточки и в отчёте потока,
  // где его можно отключить.
  await cardIn(page, 'В работе', 'Сверить остатки').click()
  await page.getByRole('tab', { name: 'История' }).click()
  await expect(page.getByText('перенесена из таблицы')).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await page.getByRole('button', { name: 'Поток' }).click()
  await expect(page.getByText(/1 карточка этого отчёта перенесена из другой системы/)).toBeVisible()
  await expect(page.getByText(/^В работе 1\./)).toBeVisible()
  await page.getByRole('checkbox', { name: 'Считать без перенесённых' }).check()
  await expect(page.getByText(/^В работе 0\./)).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Тот же файл ещё раз — в ту же доску: переносить нечего.
  await page.goto('/import')
  await page.getByLabel('Файл CSV или Excel').setInputFiles(file)
  await page.getByRole('radio', { name: 'На существующую доску' }).check()
  await page.getByRole('combobox', { name: 'Доска' }).selectOption({ label: 'Склад' })
  await expect(page.getByText(/Уже перенесены раньше и пропущены: 2 карточки/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Переносить нечего' })).toBeDisabled()
})

test('книга Excel: лист выбирается, дата из ячейки понята', async ({ page }) => {
  await register(page)
  await page.goto('/import')
  await page.getByLabel('Файл CSV или Excel').setInputFiles({
    name: 'План.xlsx',
    mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    buffer: workbook([
      { name: 'Сводка', rows: [['Отчёт за сентябрь'], ['Всего', 2]] },
      {
        name: 'Задачи',
        rows: [
          ['Выгрузка задач от 22.09.2026'],
          [],
          ['Название', 'Статус', 'Срок'],
          ['Сверить остатки', 'В работе', 46295],
          ['Заказать тару', 'Очередь', ''],
        ],
      },
    ]),
  })

  // Первый лист — сводка без таблицы: взят второй, где таблица есть,
  // и подпись выгрузки над ней заголовками не стала.
  const sheet = page.getByRole('combobox', { name: 'Лист книги' })
  await expect(sheet).toHaveValue('Задачи')
  await expect(page.getByText('Переедут 2 карточки из 2 строк.')).toBeVisible()
  // Выбрали сводку — сказано, что с ней не так, а вернуться можно.
  await sheet.selectOption('Сводка')
  await expect(page.getByText(/в файле нет строк/)).toBeVisible()
  await sheet.selectOption('Задачи')
  await expect(page.getByText('Переедут 2 карточки из 2 строк.')).toBeVisible()
  await expect(page.getByText('«Срок» — дата ячейки Excel')).toBeVisible()
  await page.getByRole('button', { name: 'Перенести 2 карточки' }).click()
  await page.getByRole('button', { name: 'Открыть доску' }).click()
  await expect(cardIn(page, 'В работе', 'Сверить остатки')).toBeVisible()
})

test('значение из файла ложится в выбранную колонку доски, а не заводит новую', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Разработка')
  await page.goto('/import')
  await page.getByLabel('Файл CSV или Excel').setInputFiles({
    name: 'jira.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from('Summary,Status\nWrite spec,In Review\nPlan Q4,Icebox\n'),
  })
  await page.getByRole('radio', { name: 'На существующую доску' }).check()
  await page.getByRole('combobox', { name: 'Доска' }).selectOption({ label: 'Разработка' })

  // Без выбора незнакомое значение заводит свою колонку — и это видно.
  const review = page.getByRole('combobox', { name: 'Колонка доски для «In Review»' })
  await expect(review).toHaveValue('')
  await expect(page.getByRole('listitem').filter({ hasText: /^In Review/ })).toBeVisible()

  await review.selectOption({ label: 'В работе' })
  await expect(page.getByRole('listitem').filter({ hasText: /^In Review/ })).toHaveCount(0)
  await page.getByRole('button', { name: 'Перенести 2 карточки' }).click()
  await page.getByRole('button', { name: 'Открыть доску' }).click()
  await expect(cardIn(page, 'В работе', 'Write spec')).toBeVisible()
  await expect(page.getByRole('region', { name: 'In Review' })).toHaveCount(0)
  await expect(cardIn(page, 'Icebox', 'Plan Q4')).toBeVisible()
})

test('доска YouGile переезжает по API и называет, что не едет', async ({ page }) => {
  await register(page)
  await page.goto('/import')
  await page.getByRole('radio', { name: 'Из YouGile' }).check()
  await page.getByLabel('Почта в YouGile').fill('anna@yougile.test')
  await page.getByLabel('Пароль в YouGile').fill('не тот')
  await page.getByRole('button', { name: 'Найти компании' }).click()
  await expect(page.getByText(/YouGile не принял вход/)).toBeVisible()

  await page.getByLabel('Пароль в YouGile').fill('parol12345')
  await page.getByRole('button', { name: 'Найти компании' }).click()
  await expect(page.getByLabel('Компания')).toHaveValue('co-1')
  await page.getByRole('button', { name: 'Получить ключ' }).click()
  await expect(page.getByText(/В YouGile заведён ключ API/)).toBeVisible()
  // Пароля на экране больше нет — он был нужен только для ключа.
  await expect(page.getByLabel('Пароль в YouGile')).toHaveCount(0)

  await page.getByLabel('Доска YouGile').selectOption({ label: 'Логистика · Склад' })
  await expect(page.getByText('Переедут 3 карточки из 3 строк.')).toBeVisible()
  await expect(page.getByText('задачи из архива YouGile: 1')).toBeVisible()
  // Чаты и история — не потеря: их дотянут фоном после переноса.
  await expect(page.getByText(/История и обсуждение из YouGile дотянутся фоном/)).toBeVisible()
  await expect(page.getByRole('combobox', { name: /^Что сделать: .+/ }).first()).toBeVisible()
  await expect(page.getByText(/nikto@yougile\.test · 1 карточка/)).toBeVisible()
  await page.getByRole('button', { name: 'Перенести 3 карточки' }).click()
  await expect(page.getByText(/История и обсуждение дотянуты/)).toBeVisible({ timeout: 15000 })
  await page.getByRole('button', { name: 'Открыть доску' }).click()
  await expect(cardIn(page, 'В работе', 'Заказать поддоны')).toBeVisible()
  await expect(cardIn(page, 'Нужно сделать', 'Сверить остатки')).toBeVisible()
})

test('пакет переноса: подзадачи, связи и обсуждение переезжают, доски — по одной', async ({ page }) => {
  const who = await register(page)
  await page.goto('/import')
  await page.getByRole('radio', { name: 'Пакет переноса' }).check()
  await page.getByLabel('Файл пакета (.takt)').setInputFiles({
    name: 'склад.takt',
    mimeType: 'application/zip',
    buffer: taktPackage([
      {
        externalId: 'b-1',
        title: 'Склад',
        columns: [
          { externalId: 'c-1', title: 'Нужно сделать', kind: 'queue' },
          { externalId: 'c-2', title: 'Сделано', kind: 'done' },
        ],
        people: [{ externalId: 'u-2', email: null, name: 'Иван Петров' }],
        labels: [],
        cards: [
          {
            externalId: 't-1',
            title: 'Сверить остатки',
            column: 'c-1',
            assignees: ['u-2'],
            comments: [{ author: 'u-2', at: '2026-09-03T09:00:00Z', text: 'Ряд первый сверен' }],
          },
          { externalId: 't-2', title: 'Выгрузить остатки', column: 'c-1', parent: 't-1' },
          { externalId: 't-3', title: 'Отчёт', column: 'c-2', links: [{ kind: 'relates', to: 't-1' }] },
        ],
      },
      { externalId: 'b-2', title: 'Закупки', columns: [{ externalId: 'c-9', title: 'Очередь' }], people: [], labels: [], cards: [{ externalId: 'z-1', title: 'Поддоны', column: 'c-9' }] },
    ]),
  })

  await expect(page.getByText(/Из YouGile \(Северная логистика\), собран takt-fetch e2e/)).toBeVisible()
  await expect(page.getByLabel('Доска пакета')).toHaveValue('1')
  await expect(page.getByText('Переедут 3 карточки из 3 строк.')).toBeVisible()
  await expect(page.getByText('Подзадач: 1, связей: 1, реплик обсуждения: 1.')).toBeVisible()
  await expect(page.getByText('файлы вложений')).toBeVisible()
  // Человек без почты не найден и по умолчанию не переносится; а это
  // сам переносящий — сопоставляем (ROADMAP 23.6).
  const ivan = page.getByRole('combobox', { name: 'Что сделать: Иван Петров' })
  await expect(ivan).toHaveValue('skip')
  await ivan.selectOption({ label: `Проверяющий · ${who.email}` })
  await expect(ivan).toHaveValue(/^match:/)
  await page.getByRole('button', { name: 'Перенести 3 карточки' }).click()

  // Следующая доска того же пакета — без повторного выбора файла.
  await page.getByRole('button', { name: 'Перенести следующую доску пакета' }).click()
  await expect(page.getByLabel('Доска пакета')).toHaveValue('2')
  await expect(page.getByText('Переедет 1 карточка из 1 строки.')).toBeVisible()
  await page.getByRole('button', { name: 'Перенести 1 карточку' }).click()
  await page.getByRole('button', { name: 'Открыть доску' }).click()
  await expect(cardIn(page, 'Очередь', 'Поддоны')).toBeVisible()

  // Сопоставленный стал исполнителем карточки Ивана.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Склад', exact: true }).click()
  await expect(cardIn(page, 'Нужно сделать', 'Сверить остатки').getByTitle('Проверяющий', { exact: true })).toBeVisible()
})

// Ссылка «задать пароль» (ROADMAP 23.6): писем нет, поэтому владелец
// выпускает ссылку сам. Человек открывает её в своём браузере, задаёт
// пароль и оказывается внутри — второй раз ссылка не пускает.
test('ссылка для входа задаёт пароль один раз и сразу впускает', async ({ page, browser }) => {
  await register(page)
  await page.getByRole('button', { name: 'Команда' }).click()
  const email = `zabyl-${Math.random().toString(36).slice(2, 8)}@example.test`
  await page.getByRole('textbox', { name: 'Почта коллеги' }).fill(email)
  await page.getByRole('button', { name: 'Пригласить', exact: true }).click()
  const invite = await page.locator('input[readonly]').first().inputValue()

  const second = await browser.newContext()
  const guest = await second.newPage()
  await guest.goto(invite.trim())
  await guest.getByLabel('Как вас зовут').fill('Забывчивый')
  await guest.getByLabel('Пароль').fill('parol12345')
  await guest.getByRole('button', { name: /Принять|Присоединиться/ }).click()
  await expect(guest.getByPlaceholder('Название новой доски')).toBeVisible()
  await second.close()

  // Пароль забыт — владелец выпускает ссылку.
  await page.reload()
  await page.getByRole('button', { name: 'Выпустить ссылку для входа: Забывчивый' }).click()
  const link = page.getByRole('textbox', { name: 'Ссылка для входа' })
  await expect(link).toBeVisible()
  const url = (await link.inputValue()).trim()
  expect(url).toContain('/password/')

  const third = await browser.newContext()
  const person = await third.newPage()
  await person.goto(url)
  await expect(person.getByText(email, { exact: false })).toBeVisible()
  await person.getByLabel('Придумайте пароль').fill('novyy-parol-12345')
  await person.getByRole('button', { name: 'Задать пароль и войти' }).click()
  await expect(person.getByPlaceholder('Название новой доски')).toBeVisible()
  await expect(person).toHaveURL(/\/$/)

  // Второй раз — отказ со словами, что делать.
  await person.goto(url)
  await expect(person.getByText(/попросите у администратора новую/)).toBeVisible()
  await third.close()
})

// Задачи человека со всех досок (решение владельца 22.09.2026): своя
// карточка находится во вкладке «Задачи» и открывается на своей доске.
test('вкладка «Задачи» собирает карточки человека со всех досок', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Первая доска')
  await addCard(page, 'Очередь', 'Моя задача')
  await toggleAssignee(page, cardIn(page, 'Очередь', 'Моя задача'))
  await page.getByRole('button', { name: 'Все доски' }).click()
  await createBoard(page, 'Вторая доска')
  await addCard(page, 'Очередь', 'Не моя задача')

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Задачи', exact: true }).click()
  const table = page.getByRole('table', { name: /^Задачи:/ })
  await expect(table.getByRole('button', { name: 'Моя задача' })).toBeVisible()
  await expect(table.getByText('Не моя задача')).toHaveCount(0)
  await expect(table.getByRole('button', { name: 'Первая доска' })).toBeVisible()

  await table.getByRole('button', { name: 'Моя задача' }).click()
  await expect(page).toHaveURL(/\/board\/.+\/card\//)
  await expect(page.getByRole('heading', { name: 'Моя задача' })).toBeVisible()
})

test('выгрузка называет число карточек до файла и отдаёт те же строки', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Поставки')
  await addCard(page, 'Очередь', 'Заказать упаковку')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await createBoard(page, 'Соседи')
  await addCard(page, 'Очередь', 'Чужая работа')

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Отчёты', exact: true }).click()
  const status = page.getByRole('status').filter({ hasText: 'Под отбор' })
  await expect(status).toHaveText('Под отбор попадают 2 карточки.')

  // Отбор по доске сужает число и попадает в адрес — ссылку пересылают.
  await page.getByLabel('Доски').selectOption({ label: 'Поставки' })
  await expect(status).toHaveText('Под отбор попадает 1 карточка.')
  await expect(page).toHaveURL(/[?&]board=/)

  const download = page.waitForEvent('download')
  await page.getByRole('link', { name: 'CSV' }).click()
  const file = await download
  const csv = (await readFile(await file.path())).toString('utf8')
  expect(csv).toContain('Заказать упаковку')
  expect(csv).not.toContain('Чужая работа')

  // Книга — zip: первые байты PK.
  const book = page.waitForEvent('download')
  await page.getByRole('link', { name: 'Excel (XLSX)' }).click()
  const raw = await readFile(await (await book).path())
  expect(raw.subarray(0, 2).toString()).toBe('PK')
})

test('срез повторяет отчёт одной кнопкой и возвращается из тоста', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Поставки')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Отчёты', exact: true }).click()

  await page.getByRole('button', { name: 'Прошлый квартал' }).click()
  await page.getByRole('checkbox', { name: 'Сделано' }).check()
  await page.getByRole('button', { name: 'Сохранить отбор как срез' }).click()
  await page.getByLabel('Название среза').fill('Закрыто за квартал')
  await page.getByRole('button', { name: 'Сохранить', exact: true }).click()
  const slice = page.getByRole('button', { name: 'Закрыто за квартал', exact: true })
  await expect(slice).toHaveAttribute('aria-current', 'true')

  // Период хранится словом, а не датами.
  await page.getByRole('button', { name: '30 дней' }).click()
  await page.getByRole('checkbox', { name: 'Сделано' }).uncheck()
  await expect(slice).not.toHaveAttribute('aria-current', 'true')
  await slice.click()
  await expect(page).toHaveURL(/period=lastQuarter&state=done$/)
  await expect(page.getByRole('checkbox', { name: 'Сделано' })).toBeChecked()

  // Одноимённый — отказ под полем.
  await page.getByRole('button', { name: 'Сохранить отбор как срез' }).click()
  await page.getByLabel('Название среза').fill('закрыто за КВАРТАЛ')
  await page.getByRole('button', { name: 'Сохранить', exact: true }).click()
  await expect(page.getByText('срез с таким названием уже есть')).toBeVisible()
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: 'Убрать срез «Закрыто за квартал»' }).click()
  await expect(slice).toHaveCount(0)
  await page.getByRole('button', { name: 'Вернуть' }).click()
  await expect(slice).toBeVisible()
  await page.reload()
  await expect(slice).toBeVisible()
})

test('шаблон доски задаёт начало, а итерации выключаются и возвращаются без потерь', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Спринты')
  await page.getByRole('combobox', { name: 'Как работаем' }).selectOption({ label: 'Скрам' })
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()

  // Скрам: первая итерация уже заведена.
  await expect(page.getByRole('button', { name: /^Итерация 1 · / })).toBeVisible()
  const grouping = page.getByRole('combobox', { name: 'Группировка' })
  await expect(grouping.getByRole('option', { name: 'По итерации' })).toHaveCount(1)

  // Выключение — в «Потоке», рядом с обещанием доски: прячет итерации
  // отовсюду и ни о чём не спрашивает.
  await page.getByRole('button', { name: 'Поток' }).click()
  await page.getByRole('checkbox', { name: 'Работаем итерациями' }).uncheck()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()
  await expect(page.getByRole('button', { name: /^Итерация 1 · / })).toHaveCount(0)
  await expect(grouping.getByRole('option', { name: 'По итерации' })).toHaveCount(0)
  await expect(page.getByRole('combobox', { name: 'Итерация' })).toHaveCount(0)

  // Включение возвращает ту же итерацию, а не новую.
  await page.getByRole('button', { name: 'Работать итерациями' }).click()
  await expect(page.getByRole('button', { name: /^Итерация 1 · / })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('button', { name: /^Итерация 1 · / })).toHaveCount(1)
})

test('вид «Дерево» показывает эпик с фичами и задачами', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска с деревом')
  await addCard(page, 'Очередь', 'Переезд')
  await cardIn(page, 'Очередь', 'Переезд').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Фича')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await page.getByLabel('Задачи', { exact: true }).getByRole('button', { name: 'Фича' }).click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Задача')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  await page.getByRole('combobox', { name: 'Вид доски' }).selectOption({ label: 'Дерево' })
  const tree = page.getByRole('list', { name: 'Дерево работы доски' })
  await expect(tree.getByRole('button', { name: 'Переезд', exact: true })).toBeVisible()
  await expect(tree.getByRole('button', { name: 'Задача', exact: true })).toBeVisible()
  await tree.getByRole('button', { name: 'Свернуть «Переезд»' }).click()
  await expect(tree.getByRole('button', { name: 'Задача', exact: true })).toHaveCount(0)
})

test('портфель эпиков заводится шаблоном и назван в списке досок', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Эпики')
  await page.getByRole('combobox', { name: 'Как работаем' }).selectOption({ label: 'Портфель эпиков' })
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  // Первая колонка портфеля — идея, а не очередь.
  await expect(page.getByRole('region', { name: 'Идея' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Работать итерациями' })).toBeVisible()

  // Уровень меняется в «Потоке» и возвращается обратно.
  await page.getByRole('button', { name: 'Поток' }).click()
  const level = page.getByRole('combobox', { name: 'Уровень доски' })
  await expect(level).toHaveValue('portfolio')
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  await page.getByRole('button', { name: 'Все доски' }).click()
  await expect(page.getByText(/портфель эпиков/)).toBeVisible()
})

test('задача команды несёт метку эпика с портфеля, и метка отбирает доску', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Склад')
  await addCard(page, 'Очередь', 'Своя работа')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByPlaceholder('Название новой доски').fill('Эпики')
  await page.getByRole('combobox', { name: 'Как работаем' }).selectOption({ label: 'Портфель эпиков' })
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await addCard(page, 'Идея', 'Большой переезд')

  // Фича эпика — на доске команды.
  await cardIn(page, 'Идея', 'Большой переезд').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Фича переезда')
  await page.getByRole('combobox', { name: 'Доска подзадачи' }).selectOption({ label: 'Склад' })
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Склад')
  const feature = cardIn(page, 'Очередь', 'Фича переезда')
  await expect(feature.getByRole('button', { name: /Эпик «Большой переезд»/ })).toBeVisible()

  // Метка отбирает доску по эпику: своей работы вне эпика не видно.
  await feature.getByRole('button', { name: /Эпик «Большой переезд»/ }).click()
  await expect(page).toHaveURL(/[?&]epic=/)
  await expect(cardIn(page, 'Очередь', 'Своя работа')).toHaveCount(0)
  await page.getByRole('button', { name: 'Убрать из фильтра эпик «Большой переезд»' }).click()
  await expect(cardIn(page, 'Очередь', 'Своя работа')).toBeVisible()
})

test('эпик портфеля говорит, где его работа, а дерево портфеля доходит до задач команды', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Склад')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByPlaceholder('Название новой доски').fill('Эпики')
  await page.getByRole('combobox', { name: 'Как работаем' }).selectOption({ label: 'Портфель эпиков' })
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await addCard(page, 'Идея', 'Большой переезд')
  await cardIn(page, 'Идея', 'Большой переезд').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Фича переезда')
  await page.getByRole('combobox', { name: 'Доска подзадачи' }).selectOption({ label: 'Склад' })
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Задача фичи — на доске команды, портфелю её снимок не знает.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Склад')
  await cardIn(page, 'Очередь', 'Фича переезда').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByLabel('Название подзадачи').fill('Задача переезда')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Эпики', exact: true }).click()
  const teams = cardIn(page, 'Идея', 'Большой переезд').getByRole('list', { name: 'Где работа эпика' })
  const share = teams.getByRole('link', { name: /0\/1/ })
  await expect(share).toHaveAttribute('title', /^Склад: готово 0 из 1/)

  // Дерево портфеля: эпик › фича на Складе › её задача — через доску.
  await page.getByRole('combobox', { name: 'Вид доски' }).selectOption({ label: 'Дерево' })
  const tree = page.getByRole('list', { name: 'Дерево работы доски' })
  await expect(tree.getByText('Задача переезда', { exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: 'Вид доски' }).selectOption({ label: 'Доска' })

  // Значок ведёт на доску команды, отобранную по эпику.
  await share.click()
  await expect(page).toHaveURL(/[?&]epic=/)
  await expect(cardIn(page, 'Очередь', 'Фича переезда')).toBeVisible()

  // В дереве доски команды эпик с портфеля — ссылка: открывает его там.
  await page.getByRole('combobox', { name: 'Вид доски' }).selectOption({ label: 'Дерево' })
  await page
    .getByRole('list', { name: 'Дерево работы доски' })
    .getByRole('link', { name: 'Большой переезд', exact: true })
    .click()
  await expect(page.getByRole('region', { name: 'Идея' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Большой переезд' })).toBeVisible()
})

// Связь с эпиком — с любой стороны (замечено владельцем 25.09.2026):
// прежде задачу с эпиком можно было только завести с доски эпиков,
// а готовую задачу команды к эпику не подвесить — выбор связи предлагал
// карточки только своей доски.
test('задача команды подвешивается к эпику со своей доски, а эпик находит готовую задачу команды', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Склад')
  await addCard(page, 'Очередь', 'Готовая задача')
  await addCard(page, 'Очередь', 'Вторая задача')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByPlaceholder('Название новой доски').fill('Эпики')
  await page.getByRole('combobox', { name: 'Как работаем' }).selectOption({ label: 'Портфель эпиков' })
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await addCard(page, 'Идея', 'Большой переезд')

  // С эпика: найти готовую задачу на доске команды.
  await cardIn(page, 'Идея', 'Большой переезд').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByText('Связать с существующей карточкой').click()
  await page.getByRole('searchbox', { name: 'Карточка для связи' }).fill('Вторая')
  await page.getByRole('button', { name: /Вторая задача.*Склад/ }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // С задачи команды: выбрать эпик родителем.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Склад')
  await cardIn(page, 'Очередь', 'Готовая задача').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByText('Связать с существующей карточкой').click()
  // Эпик всегда родитель: подзадачей его не выбрать и подзадачу
  // на доске эпиков с задачи не завести.
  await expect(page.getByRole('combobox', { name: 'Доска подзадачи' }).locator('option', { hasText: 'Эпики' })).toHaveCount(0)
  await page.getByRole('searchbox', { name: 'Карточка для связи' }).fill('переезд')
  await expect(page.getByText('Ничего не нашлось')).toBeVisible()
  await page.getByRole('combobox', { name: 'Вид связи' }).selectOption({ label: 'Родитель' })
  await page.getByRole('button', { name: /Большой переезд.*Эпики · портфель/ }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  // Обе задачи несут метку эпика — связь та же, что у заведённых с портфеля.
  await page.reload()
  for (const title of ['Готовая задача', 'Вторая задача']) {
    await expect(
      cardIn(page, 'Очередь', title).getByRole('button', { name: /Эпик «Большой переезд»/ }),
    ).toBeVisible()
  }
})

// Карточка с подзадачами после переноса остаётся с подзадачами — без
// перезагрузки (замечено владельцем 25.09.2026): перенос возвращал
// карточку без прогресса, и блок пропадал до обновления страницы.
test('блок подзадач остаётся на карточке после переноса в другую колонку', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска переноса с подзадачами')
  await addCard(page, 'Очередь', 'Разбитая работа')
  const parent = cardIn(page, 'Очередь', 'Разбитая работа')
  await parent.hover()
  await parent.getByRole('button', { name: /Действия карточки/ }).click()
  await page.getByRole('menuitem', { name: 'Завести подзадачу' }).click()
  await parent.getByLabel('Название подзадачи').fill('Первая часть')
  await parent.getByLabel('Название подзадачи').press('Enter')
  await expect(parent.getByRole('button', { name: /Подзадачи: готово 0 из 1/ })).toBeVisible()

  await parent.hover()
  await parent.getByRole('checkbox', { name: 'Выделить «Разбитая работа»' }).check()
  await page.getByRole('status', { name: 'Действия над выделенными' })
    .getByRole('button', { name: 'Перенести выделенные' }).click()
  await page.getByRole('menuitem', { name: 'В работе' }).click()

  const moved = cardIn(page, 'В работе', 'Разбитая работа')
  await expect(moved).toBeVisible()
  await expect(moved.getByRole('button', { name: /Подзадачи: готово 0 из 1/ })).toBeVisible()
})

// Эпик — строкой наверху вкладки «Задачи» (владелец 25.09.2026 не нашёл
// привязку к эпику внизу вкладки): выбрать, увидеть, отвязать.
test('строка «Эпик» наверху вкладки выбирает эпик и отвязывает его', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Склад')
  await addCard(page, 'Очередь', 'Задача команды')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByPlaceholder('Название новой доски').fill('Эпики')
  await page.getByRole('combobox', { name: 'Как работаем' }).selectOption({ label: 'Портфель эпиков' })
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await addCard(page, 'Идея', 'Генератор данных')
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Склад')

  await cardIn(page, 'Очередь', 'Задача команды').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await expect(page.getByText('Не задан.')).toBeVisible()
  await page.getByRole('button', { name: 'Выбрать эпик…' }).click()
  await page.getByRole('searchbox', { name: 'Номер или название эпика' }).fill('генератор')
  await page.getByRole('button', { name: /Генератор данных.*Эпики/ }).click()

  const unlink = page.getByRole('button', { name: 'Убрать эпик «Генератор данных» у этой карточки' })
  await expect(unlink).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Задача команды').getByRole('button', { name: /Эпик «Генератор данных»/ })).toBeVisible()

  await unlink.click()
  await expect(page.getByRole('button', { name: 'Выбрать эпик…' })).toBeVisible()
  await expect(cardIn(page, 'Очередь', 'Задача команды').getByRole('button', { name: /Эпик «Генератор данных»/ })).toHaveCount(0)
})

// «Ждёт задачу 2» уходит, когда задачу 2 сделали (замечено владельцем
// 25.09.2026): связь «блокирует» держит, пока держащая не сделана.
test('«Ждёт» пропадает с карточки, когда то, чего она ждёт, сделано', async ({ page }) => {
  await register(page)
  await createBoard(page, 'Доска ожидания')
  await addCard(page, 'Очередь', 'Задача 1')
  await addCard(page, 'Очередь', 'Задача 2')

  await cardIn(page, 'Очередь', 'Задача 2').click()
  await page.getByRole('tab', { name: 'Задачи' }).click()
  await page.getByText('Связать с существующей карточкой').click()
  await page.getByRole('combobox', { name: 'Вид связи' }).selectOption({ label: 'Блокирует' })
  await page.getByRole('searchbox', { name: 'Карточка для связи' }).fill('Задача 1')
  await page.getByRole('button', { name: /Задача 1.*На этой доске/ }).click()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).first().click()

  const waiting = cardIn(page, 'Очередь', 'Задача 1')
  await expect(waiting.locator('.card-waits')).toContainText('Ждёт')

  const holder = cardIn(page, 'Очередь', 'Задача 2')
  await holder.hover()
  await holder.getByRole('checkbox', { name: 'Выделить «Задача 2»' }).check()
  await page.getByRole('status', { name: 'Действия над выделенными' })
    .getByRole('button', { name: 'Перенести выделенные' }).click()
  await page.getByRole('menuitem', { name: 'Готово' }).click()
  await expect(cardIn(page, 'Готово', 'Задача 2')).toBeVisible()

  await expect(waiting.locator('.card-waits')).toHaveCount(0)
})
