import { workbook } from './xlsx.ts'
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

test('снимки экранов', async ({ page, browser }) => {
  // Один сценарий снимает все экраны подряд, и тридцати секунд
  // по умолчанию перестало хватать: 21.09.2026 он шёл 29 с и следующим
  // же снимком упал по времени — не на поломке, а на числе экранов.
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1440, height: 900 })

  await page.goto('/')
  await page.screenshot({ path: `${SHOTS}/01-вход.png` })

  // Отказ формы — состояние, которого в наборе не было, а смотреть
  // на форму имеет смысл именно в нём: в покое любая форма выглядит
  // хорошо. Здесь видно сразу и то, где стоит отказ, и то, что
  // подсказка от него не пропала.
  await page.getByLabel('Почта').fill(OWNER)
  await page.getByLabel('Пароль').fill('ne-tot-parol')
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByRole('alert')).toContainText('неверная')
  await page.screenshot({ path: `${SHOTS}/01в-вход-отказ.png` })

  // Тот же отказ в тёмной теме: цвет отказа единственным из палитры
  // остался не пересчитанным по APCA после этапа 2 — потому что экрана
  // с ошибкой не было ни в одном снимке и ни в одном замере.
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: `${SHOTS}/01д-вход-отказ-тёмный.png` })
  await page.emulateMedia({ colorScheme: 'light' })

  // Заведение организации: с этого экрана всё начинается, а снимка
  // у него не было вовсе — как и у приглашения ниже.
  await page.getByRole('button', { name: 'Завести новую организацию' }).click()
  await page.screenshot({ path: `${SHOTS}/01б-новая-организация.png` })

  // Отказ у поля: пустое имя и короткий пароль разом. Пузырёк браузера
  // показал бы первый и промолчал об остальных.
  await page.getByLabel('Придумайте пароль').fill('korotko')
  await page.getByRole('button', { name: 'Завести организацию' }).click()
  await expect(page.getByLabel('Как вас зовут')).toHaveAttribute('aria-invalid', 'true')
  await page.screenshot({ path: `${SHOTS}/01г-регистрация-отказ-полей.png` })

  await page.getByRole('button', { name: 'У меня уже есть аккаунт' }).click()

  await signIn(page)
  await page.screenshot({ path: `${SHOTS}/02-список-досок.png`, fullPage: true })

  // Отказ сервера, адресованный полю: ключ занят соседней доской.
  // Отказ стоит под тем полем, которое переписывать, а не общей
  // строкой под всей формой.
  await page.getByPlaceholder('Название новой доски').fill('Ещё поставки')
  await page.getByPlaceholder('Ключ').fill('ПОСТ')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await expect(page.getByLabel(/Ключ доски/)).toHaveAttribute('aria-invalid', 'true')
  await page.screenshot({ path: `${SHOTS}/02б-занятый-ключ.png` })
  await page.reload()

  await openBoard(page, 'Поставки')
  const boardUrl = page.url()
  await page.screenshot({ path: `${SHOTS}/03-доска.png` })

  // Доска под отбором: колонки, из которых отбор убрал всё, обязаны
  // сказать об этом, а не притворяться пустыми.
  await page.getByRole('button', { name: /^Отбор/ }).click()
  await page.getByRole('checkbox', { name: 'Заблокированные' }).check()
  await expect(page.getByText(/Под отбор ничего не подошло/).first()).toBeVisible()
  await page.screenshot({ path: `${SHOTS}/03б-колонка-под-отбором.png` })
  await page.getByRole('button', { name: 'Показать все' }).click()
  await page.getByRole('button', { name: /^Отбор/ }).click()

  // Тёмная тема — системная.
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: `${SHOTS}/04-доска-тёмная.png` })
  await page.emulateMedia({ colorScheme: 'light' })

  // Как это уходит на бумагу. Печатают отсюда доску списком
  // и отчёт по итерации; лист — тот же экран, но без прокруток,
  // без управления и во всю ширину, и посмотреть на него глазами
  // больше негде.
  await page.emulateMedia({ media: 'print' })
  await page.screenshot({ path: `${SHOTS}/04в-доска-на-печать.png`, fullPage: true })
  await page.emulateMedia({ media: 'screen' })

  // Режим высокой контрастности: третья палитра, которой в наборе
  // не было вовсе. Система подменяет все цвета своими, и на снимке
  // видно то, чего не видно ни в светлой теме, ни в тёмной, — какие
  // состояния держались одной подложкой.
  await page.emulateMedia({ forcedColors: 'active' })
  await page.screenshot({ path: `${SHOTS}/04б-доска-высокий-контраст.png` })
  await page.emulateMedia({ forcedColors: 'none' })

  // Плотный режим — тот самый множитель.
  // Плотность — в личных настройках за именем, вкладка «Оформление».
  const density = async () => {
    await page.getByRole('button', { name: /^Личные настройки/ }).click()
    await page.getByRole('tab', { name: 'Оформление' }).click()
    await page.getByRole('checkbox', { name: 'Плотнее' }).click()
    await page.keyboard.press('Escape')
  }
  await density()
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${SHOTS}/05-доска-плотная.png` })
  await density()

  // Дорожки: та же доска, разложенная не по колонкам, а по исполнителю,
  // метке, итерации и важности. В снимках не было ни одной, а раскладка
  // у каждой своя — и пустая дорожка, и человек без работы, и карточка
  // с двумя метками, попадающая в две дорожки сразу.
  const grouping = page.getByLabel('Группировка')
  for (const [value, file] of [
    ['assignee', '05б-дорожки-по-исполнителю'],
    ['label', '05в-дорожки-по-метке'],
    ['iteration', '05г-дорожки-по-итерации'],
    ['priority', '05д-дорожки-по-важности'],
  ] as const) {
    await grouping.selectOption(value)
    await page.waitForTimeout(300)
    await page.screenshot({ path: `${SHOTS}/${file}.png`, fullPage: true })
  }
  await grouping.selectOption('none')
  await page.waitForTimeout(200)

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

  // Таблица в узком окне: столбцов больше, чем влезает, и видно, что
  // остаётся на месте при прокрутке вбок — ключ, которым названа
  // строка. Вида в наборе не было, а ломается таблица именно здесь:
  // на широком мониторе она влезает целиком и выглядит безупречно.
  await page.setViewportSize({ width: 380, height: 760 })
  await page.evaluate(() => {
    const wrap = document.querySelector('.table-wrap') as HTMLElement
    wrap.scrollLeft = 600
  })
  await page.screenshot({ path: `${SHOTS}/12б-таблица-узкая-прокручена.png` })
  await page.setViewportSize({ width: 1440, height: 900 })

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
  await page.getByRole('button', { name: '▸ Продажи' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/20б-узел-структуры.png`, fullPage: true })

  // Узел без людей, за которым осталась доска. Так бывает не только
  // руками: каталог отключает последнего в группе, и состав пустеет сам.
  // Доска при этом никуда не девается, а видеть её может стать некому —
  // об этом сказано в составе, и снимок нужен, чтобы это было чем
  // разобрать глазами.
  await page.getByRole('button', { name: '▸ Разработка' }).click()
  await page.waitForTimeout(200)
  await page.getByRole('button', { name: '▸ Платформа' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/20в-узел-без-людей.png`, fullPage: true })

  // Перенос из таблицы: предпросмотр с потерями — ненайденной почтой,
  // строкой без заголовка, незнакомым приоритетом. Только предпросмотр:
  // переносить на демонстрационные доски снимку незачем.
  await page.goto('/import')
  await page.getByLabel('Файл CSV или Excel').setInputFiles({
    name: 'Выгрузка из старой доски.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(
      'Название;Статус;Ответственный;Теги;Оценка;Приоритет;Дедлайн;Описание\n' +
        'Сверить остатки на складе;В работе;anna@example.test;склад;3;высокий;30.09.2026;По итогам инвентаризации\n' +
        'Заказать тару на октябрь;Очередь;nikto@example.test;склад, закупки;;обычный;;\n' +
        'Согласовать договор с перевозчиком;На согласовании;vera@example.test;;5;как можно скорее;15.10.2026;\n' +
        ';Очередь;;;;;;\n',
    ),
  })
  await expect(page.getByText(/Переедут 3 карточки/)).toBeVisible()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/21-перенос-из-таблицы.png`, fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/21б-перенос-на-телефоне.png`, fullPage: true })
  await page.setViewportSize({ width: 1440, height: 900 })

  // Книга Excel: выбор листа виден, только когда листов больше одного.
  await page.goto('/import')
  await page.getByLabel('Файл CSV или Excel').setInputFiles({
    name: 'План на квартал.xlsx',
    mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    buffer: workbook([
      { name: 'Обложка', rows: [['План на квартал']] },
      {
        name: 'Задачи',
        rows: [
          ['Название', 'Статус', 'Ответственный', 'Срок'],
          ['Сверить остатки на складе', 'В работе', 'anna@example.test', 46295],
          ['Заказать тару на октябрь', 'Очередь', 'boris@example.test', ''],
        ],
      },
      { name: 'Архив', rows: [['Название'], ['Старая задача']] },
    ]),
  })
  await expect(page.getByText(/Переедут 2 карточки/)).toBeVisible()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/21в-перенос-книги-excel.png`, fullPage: true })

  // На существующую доску: значения колонки файла сопоставляются нашим
  // колонкам. «На проверке» на «Поставках» нет — по умолчанию она
  // заведётся новой, пока человек не отдаст её в «В работе».
  await page.goto('/import')
  await page.getByLabel('Файл CSV или Excel').setInputFiles({
    name: 'Выгрузка из Kaiten.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(
      'Название;Статус\nСверить акты;В работе\nСогласовать тариф;На проверке\nЗаказать поддоны;Бэклог\nЗакрыть квартал;Готово\n',
    ),
  })
  await page.getByRole('radio', { name: 'На существующую доску' }).check()
  await page.getByRole('combobox', { name: 'Доска' }).selectOption({ label: 'Поставки' })
  await expect(page.getByRole('combobox', { name: 'Колонка доски для «На проверке»' })).toBeVisible()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/21г-перенос-на-существующую-доску.png`, fullPage: true })

  // Из YouGile — против поддельного YouGile (e2e/fake-yougile.mjs):
  // вход, затем предпросмотр со списком того, что не едет.
  await page.goto('/import')
  await page.getByRole('radio', { name: 'Из YouGile' }).check()
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${SHOTS}/21д-перенос-из-yougile-вход.png`, fullPage: true })
  await page.getByLabel('Почта в YouGile').fill('anna@yougile.test')
  await page.getByLabel('Пароль в YouGile').fill('parol12345')
  await page.getByRole('button', { name: 'Найти компании' }).click()
  await page.getByRole('button', { name: 'Получить ключ' }).click()
  await page.getByLabel('Доска YouGile').selectOption({ label: 'Логистика · Склад' })
  await expect(page.getByText(/Переедут 3 карточки/)).toBeVisible()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/21е-перенос-из-yougile.png`, fullPage: true })

  // Структура глазами администратора области. Вид, которого в наборе
  // не было: все снимки организации снимались владельцем, а у него
  // на этом экране можно всё, и разницы не видно. Борис отвечает
  // за «Платформу» — у неё и у всего под ней действия есть, у соседних
  // ветвей их нет, корневое подразделение не заводится вовсе.
  const area = await browser.newContext({ locale: 'ru-RU' })
  const areaPage = await area.newPage()
  await areaPage.setViewportSize({ width: 1440, height: 900 })
  await areaPage.goto('/')
  await areaPage.getByLabel('Почта').fill('boris@example.test')
  await areaPage.getByLabel('Пароль').fill(PASSWORD)
  await areaPage.getByRole('button', { name: 'Войти' }).click()
  // Организация выбирается явно, если их несколько: на обжитой базе
  // Борис попадает и в организации проверок, и какая откроется по
  // умолчанию — не наше дело угадывать. На свежем стенде организация
  // у него одна, и выбирать нечего — ждать выбора значило бы висеть
  // до конца отведённого времени.
  await expect(areaPage.getByRole('button', { name: 'Структура' })).toBeVisible()
  const orgs = areaPage.getByLabel('Организация')
  if (await orgs.count()) await orgs.selectOption({ label: 'Северный проект' })
  await areaPage.getByRole('button', { name: 'Структура' }).click()
  await expect(areaPage.getByRole('button', { name: '▸ Платформа' })).toBeVisible({
    timeout: 10_000,
  })
  await areaPage.waitForTimeout(400)
  await areaPage.screenshot({ path: `${SHOTS}/20г-структура-администратора.png`, fullPage: true })
  await area.close()

  // Приглашение — единственный экран, который видит не хозяин стенда,
  // а тот, кого позвали. Ссылка живёт один показ, поэтому снимается
  // в чужом окне, а приглашение потом отзывается: стенд обязан остаться
  // таким же, каким был.
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByPlaceholder('Почта коллеги').fill('novichok@example.test')
  await page.getByRole('button', { name: 'Пригласить' }).click()
  const link = page.getByRole('textbox', { name: 'Ссылка-приглашение' })
  await expect(link).toBeVisible()
  const invite = await link.inputValue()

  const guest = await browser.newContext({ locale: 'ru-RU' })
  const guestPage = await guest.newPage()
  await guestPage.setViewportSize({ width: 1440, height: 900 })
  await guestPage.goto(invite)
  await guestPage.waitForTimeout(400)
  await guestPage.screenshot({ path: `${SHOTS}/22-приглашение.png` })
  await guest.close()

  await page.getByRole('button', { name: /^Отозвать приглашение/ }).click()
  await expect(page.getByRole('button', { name: /^Отозвать приглашение/ })).toHaveCount(0)

  // Тёмная тема на остальных экранах. В снимках она была только
  // у доски — а расходится тема как раз на мелочах: подписи аватаров,
  // рамки полей, тени. Проход по дизайн-языку нашёл в тёмной теме два
  // нарушения контраста, которых в светлой не было вовсе, и увидеть их
  // было негде: тёмных снимков этих экранов не существовало.
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.getByRole('button', { name: 'Доски' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/23-список-досок-тёмный.png`, fullPage: true })

  await openBoard(page, 'Поставки')
  const dark = page.getByRole('group', { name: /Выпустить релиз склада/ })
  await dark.click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/24-карточка-тёмная.png` })
  await backToBoard(page, boardUrl)

  await page.getByRole('button', { name: 'Таблица' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/25-таблица-тёмная.png` })
  await page.getByRole('button', { name: 'Доска' }).click()

  await page.getByRole('button', { name: 'Поток' }).click()
  await page.waitForTimeout(600)
  await page.screenshot({ path: `${SHOTS}/26-поток-тёмный.png`, fullPage: true })
  await backToBoard(page, boardUrl)

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/27-команда-тёмная.png`, fullPage: true })
  await page.getByRole('button', { name: 'Структура' }).click()
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/28-структура-тёмная.png`, fullPage: true })
  await page.emulateMedia({ colorScheme: 'light' })

  // Зум 200%: страница шириной 1440 при увеличении вдвое видна ровно
  // так же, как окно в 720. Требование WCAG 1.4.10, и ловит оно те же
  // переполнения, что узкое окно, — только на настольном экране,
  // где их никто не ищет.
  await page.setViewportSize({ width: 720, height: 450 })
  await page.getByRole('button', { name: 'Доски' }).click()
  await openBoard(page, 'Поставки')
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/29-зум-200.png` })

  // Узкий экран: колонки не помещаются рядом, показывается одна.
  // Триста шестьдесят, а не триста девяносто: это самое узкое из того,
  // на чём вообще смотрят, и переполнение видно на нём первым.
  await page.setViewportSize({ width: 360, height: 760 })
  await page.getByRole('button', { name: 'Все доски' }).click()
  await openBoard(page, 'Поставки')
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOTS}/21-узкий-экран.png` })
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: `${SHOTS}/21б-узкий-экран-тёмный.png` })
  await page.emulateMedia({ colorScheme: 'light' })

  // Личные настройки за именем (ROADMAP 30.6). Шапка на 360: имя —
  // кнопка и не должно выталкивать остальное; дальше сам диалог на обоих
  // языках — английские подписи длиннее и ломают вёрстку там, где
  // русские помещались.
  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/30-шапка-узкая.png` })
  await page.getByRole('button', { name: /^Личные настройки/ }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/31-личные-настройки-узкие.png` })
  await page.keyboard.press('Escape')

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.getByRole('button', { name: /^Личные настройки/ }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/32-личные-настройки-язык.png` })
  await page.getByRole('tab', { name: 'Вход' }).click()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/33-личные-настройки-вход.png` })
  await page.keyboard.press('Escape')

  // Английский: браузер с английским языком системы, человек язык
  // не выбирал — значит, решает браузер.
  const english = await browser.newContext({ locale: 'en-GB' })
  const en = await english.newPage()
  await en.setViewportSize({ width: 1440, height: 900 })
  await en.goto('/')
  await en.getByLabel('E-mail').fill(OWNER)
  await en.getByLabel('Password').fill(PASSWORD)
  await en.getByRole('button', { name: 'Sign in', exact: true }).click()
  await en.getByRole('button', { name: /^Personal settings/ }).click()
  await en.getByRole('tab', { name: 'Appearance' }).click()
  await en.waitForTimeout(300)
  await en.screenshot({ path: `${SHOTS}/34-личные-настройки-en.png` })
  await en.getByRole('tab', { name: 'Language' }).click()
  await en.waitForTimeout(300)
  await en.screenshot({ path: `${SHOTS}/34б-личные-настройки-en-язык.png` })
  await english.close()
})
