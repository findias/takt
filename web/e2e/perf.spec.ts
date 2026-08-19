import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Замеры на большой доске.
//
// Пятьсот карточек — не выдуманный предел: столько накапливается
// за квартал у команды из десяти человек, если ничего не убирать
// с доски, а убирать с доски не любят. Всё, что мы проверяли до сих
// пор, проверялось на трёх карточках, и это ровно тот размер, на
// котором незаметно ничего.
//
// Пороги взяты из Core Web Vitals и из руководств по отзывчивости:
// 200 мс на отклик (INP «хорошо») и секунда до первой отрисовки —
// граница, за которой человек перестаёт считать действие мгновенным.
//
// Замер — не эталонное сравнение снимков: числа на разных машинах
// разные, поэтому пороги взяты с запасом, а падение теста означает
// «стало ощутимо хуже», а не «на два процента медленнее».

type Newcomer = { email: string; password: string; org: string }

function newcomer(): Newcomer {
  const id = Math.random().toString(36).slice(2, 10)
  return { email: `perf-${id}@example.test`, password: 'parol12345', org: `Нагрузка ${id}` }
}

async function register(page: Page): Promise<Newcomer> {
  const who = newcomer()
  await page.goto('/')
  await page.getByRole('button', { name: 'Завести новую организацию' }).click()
  await page.getByLabel('Название организации').fill(who.org)
  await page.getByLabel('Как вас зовут').fill('Замерщик')
  await page.getByLabel('Почта').fill(who.email)
  await page.getByLabel('Пароль').fill(who.password)
  await page.getByRole('button', { name: 'Завести организацию' }).click()
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible()
  return who
}

/**
 * Наполнить доску через API, а не через экран.
 *
 * Пятьсот карточек, заведённых кнопкой, — это полчаса теста и проверка
 * не того: наполнение здесь подготовка, а не предмет замера.
 */
async function fillBoard(page: Page, boardId: string, columnId: string, count: number) {
  const created = await page.evaluate(
    async ({ boardId, columnId, count }) => {
      const started = performance.now()
      // Пачками: пятьсот последовательных запросов упираются в сеть,
      // а пятьсот одновременных — в пул соединений базы.
      for (let from = 0; from < count; from += 25) {
        await Promise.all(
          Array.from({ length: Math.min(25, count - from) }, (_, i) =>
            fetch(`/api/boards/${boardId}/operations`, {
              method: 'POST',
              headers: { 'content-type': 'application/json' },
              body: JSON.stringify({
                operationId: crypto.randomUUID(),
                type: 'CREATE_CARD',
                payload: {
                  columnId,
                  title: `Задача номер ${from + i + 1}`,
                  place: 'end',
                },
              }),
            }),
          ),
        )
      }
      return performance.now() - started
    },
    { boardId, columnId, count },
  )
  return created
}

/** Доска и первая колонка через API: экран для этого не нужен. */
async function boardWithColumn(page: Page, name: string) {
  return page.evaluate(async (name) => {
    const board = await fetch('/api/boards', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name }),
    }).then((r) => r.json())
    const snapshot = await fetch(`/api/boards/${board.id}`).then((r) => r.json())
    return { boardId: board.id as string, columnId: snapshot.columns[0].id as string }
  }, name)
}

const CARDS = 500

test('доска в пятьсот карточек открывается и отвечает', async ({ page }) => {
  test.slow()
  await register(page)
  const { boardId, columnId } = await boardWithColumn(page, 'Большая доска')
  await fillBoard(page, boardId, columnId, CARDS)

  // Первая отрисовка: от перехода по адресу до видимой доски.
  const started = Date.now()
  await page.goto(`/board/${boardId}`)
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()
  const firstPaint = Date.now() - started
  console.log(`первая отрисовка: ${firstPaint} мс`)
  expect(firstPaint).toBeLessThan(1000)

  // Столько карточек в DOM держать незачем: в окне их два десятка,
  // остальные — работа для стилей, разметки и сборщика мусора.
  const inDom = await page.locator('.card').count()
  console.log(`карточек в разметке: ${inDom} из ${CARDS}`)

  // Отклик на ввод. Меряем не «через сколько появился результат»,
  // а задержку самого события: именно её человек ощущает как
  // «залипло». Порог INP «хорошо» — 200 мс.
  await page.evaluate(() => {
    const w = window as unknown as { __slowest: number }
    w.__slowest = 0
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        w.__slowest = Math.max(w.__slowest, entry.duration)
      }
    }).observe({ type: 'event', durationThreshold: 16, buffered: true })
  })

  const search = page.getByRole('searchbox', { name: 'Найти карточку' })
  await search.pressSequentially('Задача номер 4', { delay: 30 })
  await expect(page.getByText('скрыто', { exact: false })).toBeVisible()

  const slowest = await page.evaluate(
    () => (window as unknown as { __slowest: number }).__slowest,
  )
  console.log(`самое медленное событие: ${Math.round(slowest)} мс`)
  expect(slowest).toBeLessThan(200)

  // Отбор снимаем и меряем то, что перерисовывает доску целиком:
  // переключение плотности и изменение одной карточки. Второе важнее:
  // если изменение одной карточки стоит как отрисовка всей доски,
  // работать на большой доске нельзя, а заметно это только здесь.
  await search.fill('')
  await page.evaluate(() => ((window as unknown as { __slowest: number }).__slowest = 0))

  await page.getByRole('checkbox', { name: 'Плотнее' }).check()
  const onDensity = await page.evaluate(
    () => (window as unknown as { __slowest: number }).__slowest,
  )
  console.log(`переключение плотности: ${Math.round(onDensity)} мс`)
  expect(onDensity).toBeLessThan(200)

  await page.evaluate(() => ((window as unknown as { __slowest: number }).__slowest = 0))
  // Правим с клавиатуры: наведение на большой доске неустойчиво —
  // список перерисовывается, и указатель успевает съехать. Путь тот же
  // самый, просто без мыши.
  const card = page.getByRole('group', { name: /Карточка «Задача номер 1»/ })
  await card.focus()
  await page.keyboard.press('e')
  await page.keyboard.press('End')
  await page.keyboard.type(' — правка')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('group', { name: /Задача номер 1 — правка/ })).toBeVisible()

  const onEdit = await page.evaluate(
    () => (window as unknown as { __slowest: number }).__slowest,
  )
  console.log(`правка одной карточки: ${Math.round(onEdit)} мс`)
  expect(onEdit).toBeLessThan(200)
})

test('длинная колонка дорисовывается по мере прокрутки', async ({ page }) => {
  test.slow()
  await register(page)
  const { boardId, columnId } = await boardWithColumn(page, 'Длинная колонка')
  await fillBoard(page, boardId, columnId, CARDS)
  await page.goto(`/board/${boardId}`)

  const column = page.getByRole('region', { name: 'Очередь' })
  await expect(column.locator('.card')).toHaveCount(100)
  // Счётчик колонки говорит правду о работе, а не о разметке: он берётся
  // из данных, а не из числа отрисованных карточек.
  await expect(column.getByTitle(/Карточек в колонке/)).toHaveText(String(CARDS))
  await expect(column.getByText(/Ещё 400/)).toBeVisible()

  await column.getByText(/Ещё 400/).scrollIntoViewIfNeeded()
  await expect(column.locator('.card')).toHaveCount(200)
})

test('сборка не разрастается', () => {
  // Потолок в 400 КБ на скрипты: доска должна открываться по мобильной
  // сети, а не только на рабочем месте. Проверка стоит здесь потому,
  // что разрастание происходит не сразу и не одним куском — его
  // замечают, когда уже поздно.
  //
  // Считается то, что браузер берёт при открытии: входной кусок и всё,
  // что он тянет за собой предзагрузкой. Экраны организации вынесены
  // в отдельные куски и приезжают тогда, когда за ними приходят;
  // складывать их сюда значило бы мерить не то, чего человек ждёт,
  // — и наказывать за разделение, ради которого оно и сделано.
  const dist = join(import.meta.dirname, '..', 'dist')
  const html = readFileSync(join(dist, 'index.html'), 'utf8')
  const entry = [...html.matchAll(/(?:src|href)="\/([^"]+\.js)"/g)].map((m) => m[1])
  expect(entry.length).toBeGreaterThan(0)
  const total = entry.reduce((sum, name) => sum + statSync(join(dist, name)).size, 0)

  // Отложенные куски называются рядом: пусть их не считают, но пусть
  // будет видно, что они есть и сколько их.
  const lazy = readdirSync(join(dist, 'assets'))
    .filter((name) => name.endsWith('.js') && !entry.some((e) => e.endsWith(name)))
    .map((name) => `${name} ${Math.round(statSync(join(dist, 'assets', name)).size / 1024)} КБ`)
  console.log(`при открытии: ${Math.round(total / 1024)} КБ; отложено: ${lazy.join(', ') || 'нет'}`)
  expect(total).toBeLessThan(400 * 1024)
})
