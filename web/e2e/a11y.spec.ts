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

/**
 * Элементы управления без имени.
 *
 * `placeholder` именем не считается — и это правило проверка сама
 * нарушала до 23.08.2026: он стоял в списке носителей имени, и оттого
 * тринадцать полей, у которых имени не было вовсе, проходили её
 * зелёными. Подсказка в поле исчезает с первым набранным символом,
 * а часть дикторов не читает её вовсе; поле, названное только ею,
 * называется «поле ввода».
 */
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
        el.textContent?.trim()
      if (!labelled) out.push(`${el.tagName.toLowerCase()}.${el.className || 'без класса'}`)
    }
    return out
  })
}

async function register(page: Page) {
  const id = Math.random().toString(36).slice(2, 8)
  await page.goto('/')
  await page.getByRole('button', { name: 'Завести новую организацию' }).click()
  await page.getByLabel('Название организации').fill('Проверка доступности')
  await page.getByLabel('Как вас зовут').fill('Проверяющий')
  await page.getByLabel('Почта').fill(`a11y-${id}@example.test`)
  await page.getByLabel('Пароль').fill('parol12345')
  await page.getByRole('button', { name: 'Завести организацию' }).click()
  // Срок больше обычного: организацию заводит каждая проверка этого
  // файла, идут они разом, а на создании считается хеш пароля —
  // и вчетвером они не укладываются в пять секунд на занятой машине.
  //
  // Мигало здесь ещё и не от сроков: одноимённые организации, заведённые
  // в одну секунду, выбирали один свободный адрес, и вторая падала
  // «внутренней ошибкой». Чинилось в `org.Create`, а не здесь.
  await expect(page.getByPlaceholder('Название новой доски')).toBeVisible({ timeout: 20_000 })
}

test('на всех экранах цели нажатия не мельче 24 пикселей', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })

  await page.goto('/')
  expect(await tinyTargets(page), 'экран входа').toEqual([])

  await register(page)
  expect(await tinyTargets(page), 'список досок').toEqual([])

  await page.getByPlaceholder('Название новой доски').fill('Доступность')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()

  // Действия карточки появляются по наведению — до этого их не измерить.
  await queue.getByRole('group', { name: /Карточка «Карточка»/ }).hover()
  expect(await tinyTargets(page), 'доска').toEqual([])

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await expect(page.getByRole('heading', { name: 'В организации' })).toBeVisible()
  expect(await tinyTargets(page), 'команда').toEqual([])

  // Метка на карточке — отдельный замер: без меток поле-метки показывает
  // слово и в цель нажатия укладывается, а с одной меткой съёживается
  // до точки в шесть пикселей. Проход глазами нашёл на стенде кнопку
  // шириной шестнадцать — на пустой доске такой не бывает, и проверка
  // её не видела.
  await page.getByPlaceholder('Название метки').fill('Горит')
  await page.getByRole('button', { name: 'Завести метку' }).click()
  await expect(page.getByText('Горит')).toBeVisible()

  await page.getByRole('button', { name: 'Доски' }).click()
  await page.getByRole('button', { name: 'Доступность', exact: true }).click()
  const карточка = queue.getByRole('group', { name: /Карточка «Карточка»/ })
  await карточка.getByRole('button', { name: 'Карточка', exact: true }).click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel('Повесить метку').selectOption({ label: 'Горит' })
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  await queue.getByRole('group', { name: /Карточка «Карточка»/ }).hover()
  expect(await tinyTargets(page), 'доска с меткой').toEqual([])
})

/**
 * Размеры текста — целые пиксели.
 *
 * Шкала набрана в rem, но выбрана так, чтобы при обычном корне ложиться
 * в целые: 12/13/15/18/22. Прежние 0.85 и 0.95 rem давали 13.6 и 15.2 —
 * подгонка на глаз, растянутая на весь экран; дробная строка вдобавок
 * не ложится на пиксельную сетку и мылит текст на обычном экране.
 *
 * Проверяется на настоящем экране, а не в разметке: в правиле стоит
 * rem, и увидеть получившийся пиксель можно только посчитав.
 */
test('размеры текста ложатся в целые пиксели', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Типографика')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()

  const дробные = await page.evaluate(() => {
    const out = new Set<string>()
    for (const el of document.querySelectorAll('*')) {
      if (el.children.length) continue
      if (!(el.textContent ?? '').trim()) continue
      const box = el.getBoundingClientRect()
      if (!box.width || !box.height) continue
      const size = parseFloat(getComputedStyle(el).fontSize)
      if (!Number.isInteger(size)) out.add(`${size}px «${(el.textContent ?? '').trim().slice(0, 16)}»`)
    }
    return [...out]
  })
  expect(дробные, 'дробные размеры текста').toEqual([])

  // Строка кладётся на ту же сетку, что отступы и цели нажатия: 24.
  const строка = await page.evaluate(() => getComputedStyle(document.body).lineHeight)
  expect(строка, 'высота строки').toBe('24px')
})

/**
 * Контраст в обеих темах.
 *
 * Меряется двумя приборами сразу, и это не перестраховка.
 *
 * WCAG 1.4.3 — 4.5:1 обычному тексту и 3:1 крупному; 1.4.11 — 3:1
 * границе элемента управления. Это порог сдачи: WCAG 2.2 — действующая
 * рекомендация и правовой стандарт.
 *
 * Но отношение яркостей около чёрного завышает контраст, и авторы APCA
 * говорят прямо, что для проектирования тёмной темы оно не годится.
 * Проверено на этой палитре: приглушённый текст в тёмной теме давал
 * WCAG 5.79 — выше, чем 5.43 у той же роли в светлой, — при APCA Lc 44
 * против 77. Прибор говорил «лучше», видно было вдвое хуже, и замер
 * это пропускал, потому что считал только отношением.
 *
 * Поэтому рядом считается APCA (кандидат в WCAG 3): перцептивно
 * и с оглядкой на размер шрифта. Порог сдачи остаётся за WCAG, порог
 * проектирования — за APCA. Считается по-настоящему: цвет текста
 * против первой непрозрачной подложки над ним.
 *
 * Нашлось проходом глазами и обеими темами сразу: подписи аватаров
 * красились цветом поверхности — в светлой теме это белым по цветному
 * кружку (5.4, годно), а в тёмной почти чёрным (3.15). Границы полей
 * брали цвет волосяной черты между строками: 1.42 в светлой и 1.28
 * в тёмной вместо трёх.
 */
async function lowContrast(page: Page, тема: 'Светлая' | 'Тёмная') {
  await page.getByLabel('Тема').selectOption({ label: тема })
  return await page.evaluate(() => {
    const числа = (s: string) => (s.match(/[\d.]+/g) ?? []).map(Number)
    const яркость = (c: number[]) => {
      const [r, g, b] = c.slice(0, 3).map((v) => {
        v /= 255
        return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
      })
      return 0.2126 * r + 0.7152 * g + 0.0722 * b
    }
    const отношение = (a: number[], b: number[]) => {
      const [x, y] = [яркость(a), яркость(b)]
      return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
    }
    // APCA W3 0.1.9. Своя яркость: простая степень 2.4 без излома,
    // и мягкий подъём почти-чёрного — там восприятие сжимается,
    // и именно этого не видит отношение WCAG.
    const яркостьA = (c: number[]) => {
      const [r, g, b] = c.slice(0, 3).map((v) => (v / 255) ** 2.4)
      const Y = 0.2126729 * r + 0.7151522 * g + 0.072175 * b
      return Y < 0.022 ? Y + (0.022 - Y) ** 1.414 : Y
    }
    const Lc = (текст: number[], фон: number[]) => {
      const [t, f] = [яркостьA(текст), яркостьA(фон)]
      if (Math.abs(f - t) < 0.0005) return 0
      if (f > t) {
        const S = (f ** 0.56 - t ** 0.57) * 1.14
        return S < 0.1 ? 0 : (S - 0.027) * 100
      }
      const S = (f ** 0.65 - t ** 0.62) * 1.14
      return S > -0.1 ? 0 : Math.abs((S + 0.027) * 100)
    }
    // Первая непрозрачная подложка: полупрозрачный фон сам по себе
    // ничего не говорит о том, на чём в итоге лежит текст.
    const подложка = (el: Element | null): number[] => {
      for (let n = el; n; n = n.parentElement) {
        const c = числа(getComputedStyle(n).backgroundColor)
        if (c.length < 4 || c[3] > 0.9) return c.slice(0, 3)
      }
      return [255, 255, 255]
    }
    const out: string[] = []
    for (const el of document.querySelectorAll('*')) {
      if (el.children.length) continue
      const текст = (el.textContent ?? '').trim()
      if (!текст) continue
      const box = el.getBoundingClientRect()
      if (!box.width || !box.height) continue
      const cs = getComputedStyle(el)
      const размер = parseFloat(cs.fontSize)
      const крупный = размер >= 24 || (размер >= 18.66 && parseInt(cs.fontWeight) >= 700)
      const порог = крупный ? 3 : 4.5
      const фон = подложка(el)
      const k = отношение(числа(cs.color), фон)
      if (k < порог) out.push(`текст «${текст.slice(0, 20)}» ${k.toFixed(2)} < ${порог}`)
      // Порог APCA зависит от размера: мелкому нужно больше. Lc 60 —
      // минимум читаемого, ниже него текст не ставим вовсе.
      const порогLc = крупный ? 45 : 60
      const lc = Lc(числа(cs.color), фон)
      if (lc < порогLc) out.push(`текст «${текст.slice(0, 20)}» Lc ${lc.toFixed(0)} < ${порогLc}`)
    }
    for (const el of document.querySelectorAll('input, select, textarea')) {
      const cs = getComputedStyle(el)
      if (!parseFloat(cs.borderTopWidth)) continue
      const фон = подложка(el.parentElement)
      const k = отношение(числа(cs.borderTopColor), фон)
      if (k < 3) out.push(`граница ${el.tagName} ${k.toFixed(2)} < 3`)
      const lc = Lc(числа(cs.borderTopColor), фон)
      if (lc < 45) out.push(`граница ${el.tagName} Lc ${lc.toFixed(0)} < 45`)
    }
    return [...new Set(out)]
  })
}

test('контраст держится в обеих темах', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Контраст')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()
  // Исполнитель на карточке — ради подписи аватара: она и падала
  // в тёмной теме. Без исполнителя аватара на экране нет вовсе,
  // и проверять было бы нечего.
  await queue
    .getByRole('group', { name: /Карточка «Карточка»/ })
    .getByRole('button', { name: 'Карточка', exact: true })
    .click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel('Добавить исполнителя').selectOption({ index: 1 })
  await page.keyboard.press('Escape')
  // Аватар скрыт от диктора (имя рядом сказано словами), поэтому ищем
  // его разметкой, а не ролью.
  await expect(queue.locator('.avatar').first()).toBeVisible()

  expect(await lowContrast(page, 'Светлая'), 'доска, светлая тема').toEqual([])
  expect(await lowContrast(page, 'Тёмная'), 'доска, тёмная тема').toEqual([])

  // Флажки обязаны попадать в этот же замер. У родного флажка
  // вычисленной границы нет вовсе — замер молча проходил мимо, и в
  // тёмной теме невыбранный квадрат сливался с полосой отбора.
  // Поэтому границу флажка рисуем мы, а проверка следит, что рисуем:
  // вернётся родной — счётчик разойдётся, а не промолчит.
  const безГраницы = await page.evaluate(() =>
    [...document.querySelectorAll('input[type="checkbox"]')].filter((el) => {
      const box = el.getBoundingClientRect()
      if (!box.width) return false
      return parseFloat(getComputedStyle(el).borderTopWidth) === 0
    }).length,
  )
  expect(безГраницы, 'флажки без вычисленной границы — мимо замера контраста').toBe(0)

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await expect(page.getByRole('heading', { name: 'В организации' })).toBeVisible()
  expect(await lowContrast(page, 'Светлая'), 'команда, светлая тема').toEqual([])
  expect(await lowContrast(page, 'Тёмная'), 'команда, тёмная тема').toEqual([])

  // Форма с отказом — экран, которого этот замер не видел ни разу,
  // и оттого цвет отказа единственным из всей палитры остался
  // не пересчитанным после перехода на APCA: Lc 47 в тёмной теме
  // при пороге 60. Ошибку показывали реже, чем всё остальное, — и она
  // единственная жила без проверки.
  await page.getByRole('button', { name: 'Доски' }).click()
  await page.getByPlaceholder('Название новой доски').fill('Отказ')
  await page.getByPlaceholder('Ключ').fill('Ы')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await expect(page.getByLabel(/Ключ доски/)).toHaveAttribute('aria-invalid', 'true')
  expect(await lowContrast(page, 'Светлая'), 'отказ формы, светлая тема').toEqual([])
  expect(await lowContrast(page, 'Тёмная'), 'отказ формы, тёмная тема').toEqual([])
})

/**
 * Узкое окно: ничего не уезжает за край.
 *
 * Требование WCAG 1.4.10 и оно же — проверка зума: страница на 1280
 * при увеличении вдвое видна ровно так же, как на 640. Меряется
 * горизонтальная прокрутка всего документа: доска колонки листает сама,
 * это её работа, а вот страница листаться вбок не должна.
 *
 * Нашлось проходом глазами: панельное правило было написано на голом
 * `.tabs`, а тем же классом названы разделы приложения — полоса
 * «Доски · Команда · Структура» получала отрицательные поля панели,
 * вылезала на четыре пикселя за оба края и таскала за собой всю
 * страницу.
 */
test('на узком экране страница не листается вбок', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 760 })
  await register(page)

  const шире = () =>
    page.evaluate(() => ({
      прокрутка: document.documentElement.scrollWidth,
      окно: window.innerWidth,
    }))

  expect(await шире(), 'список досок').toEqual({ прокрутка: 360, окно: 360 })

  await page.getByPlaceholder('Название новой доски').fill('Узкое окно')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()
  expect(await шире(), 'доска').toEqual({ прокрутка: 360, окно: 360 })

  // Часть внутри родителя: она уходит с доски внутрь задачи, но
  // из счёта колонки не уходит — на ней и расходились два числа.
  await queue
    .getByRole('group', { name: /Карточка «Карточка»/ })
    .getByRole('button', { name: 'Карточка', exact: true })
    .click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  await page.getByLabel('Название подзадачи').fill('Часть работы')
  await page.getByRole('button', { name: 'Подзадача' }).click()
  await expect(page.getByRole('button', { name: 'Часть работы' }).first()).toBeVisible()
  await page.keyboard.press('Escape')

  await queue
    .getByRole('group', { name: /Карточка «Карточка»/ })
    .getByRole('button', { name: 'Карточка', exact: true })
    .click()
  await expect(page.getByRole('tab', { name: 'Работа' })).toBeVisible()
  expect(await шире(), 'карточка').toEqual({ прокрутка: 360, окно: 360 })
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: 'Все доски' }).click()
  await page.getByRole('button', { name: 'Команда' }).click()
  await expect(page.getByRole('heading', { name: 'В организации' })).toBeVisible()
  expect(await шире(), 'команда').toEqual({ прокрутка: 360, окно: 360 })

  await page.getByRole('button', { name: 'Структура' }).click()
  expect(await шире(), 'структура').toEqual({ прокрутка: 360, окно: 360 })

  // На узком экране колонка одна, а остальные — в переключателе над ней.
  // Число в переключателе обязано совпадать с числом в шапке самой
  // колонки: на снимке стенда рядом стояли «Очередь 2» и «Очередь 5» —
  // переключатель не считал части, спрятанные внутрь родителей,
  // а шапка считала (их считает в лимите сервер).
  await page.getByRole('button', { name: 'Доски' }).click()
  await page.getByRole('button', { name: 'Узкое окно', exact: true }).click()
  const очередь = page.getByRole('tab', { name: /Очередь/ })
  await expect(очередь).toBeVisible()
  const вПереключателе = (await очередь.textContent())?.replace(/\D+/g, '')
  const вШапке = (
    await page.getByRole('region', { name: 'Очередь' }).locator('.column-header').textContent()
  )?.replace(/\D+/g, '')
  expect(вПереключателе, 'число в переключателе колонок').toBe(вШапке)
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
/**
 * Отказ формы словами и у поля, к которому относится.
 *
 * В настоящем браузере, а не в jsdom: `validity.tooShort` там
 * не считается вовсе (прогон 23.08.2026), и порог длины пароля
 * проверить нечем — поле с восемью обязательными символами и семью
 * набранными объявляется годным.
 */
test('короткий пароль отвергается словами, а не пузырьком браузера', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Завести новую организацию' }).click()
  await page.getByLabel('Как вас зовут').fill('Проверяющий')
  await page.getByLabel('Почта').fill('short@example.test')
  await page.getByLabel('Придумайте пароль').fill('korotko')
  await page.getByRole('button', { name: 'Завести организацию' }).click()

  const пароль = page.getByLabel('Придумайте пароль')
  await expect(пароль).toHaveAttribute('aria-invalid', 'true')
  // Отказ читается диктором вместе с полем: он связан с ним описанием,
  // а не просто стоит рядом на экране.
  const описание = await пароль.getAttribute('aria-describedby')
  expect(описание, 'отказ не связан с полем').toBeTruthy()
  const текст = (
    await Promise.all(
      описание!.split(' ').map((id) => page.locator(`#${id}`).textContent()),
    )
  ).join(' ')
  // Отказ называет числа, а не повторяет правило: правило уже стоит
  // подсказкой под тем же полем.
  expect(текст).toContain('Сейчас 7 символов, нужно 8')
  // И фокус там же: править надо это поле.
  await expect(пароль).toBeFocused()
})

test('открытое меню отдаёт фокус первому пункту', async ({ page }) => {
  await register(page)
  await page.getByPlaceholder('Название новой доски').fill('Меню с клавиатуры')
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()

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
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Карточка')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()

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
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue
    .getByPlaceholder('Что нужно сделать?')
    .fill('Собрать отчёт по всему кварталу и году')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()

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
  // Часть отдельной карточкой в колонке не стоит — она видна внутри
  // родителя. Своей строкой, со ссылкой на родителя, она появляется
  // тогда, когда родителя не видно: отбором его и убираем.
  await page.getByPlaceholder('Найти карточку').fill('Свести цифры')
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
  'Завести карточку',
  'Доступ',
  'закрыть',
  'Убрать',
  'Отозвать',
  'Скопировать',
  'Снять',
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
  await page.getByRole('button', { name: 'Завести доску', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Очередь' })).toBeVisible()

  // На доске столько «Разметок» и «Завести карточку», сколько колонок.
  expect(await contextless(page), 'доска').toEqual([])

  // В панели карточки «Снять» стоит у исполнителя, обязательства, метки
  // и блокировки — у четырёх разных объектов сразу.
  const queue = page.getByRole('region', { name: 'Очередь' })
  await queue.getByRole('button', { name: 'Завести карточку' }).click()
  await queue.getByPlaceholder('Что нужно сделать?').fill('Работа')
  await queue.getByRole('button', { name: 'Завести карточку в «Очередь»', exact: true }).click()
  const card = queue.getByRole('group', { name: /Карточка «Работа»/ })
  await card.hover()
  await card.getByRole('button', { name: /Исполнител/ }).click()
  await page.getByRole('menuitemcheckbox').first().click()
  await page.keyboard.press('Escape')
  await card.click()
  await page.getByRole('tab', { name: 'Работа' }).click()
  const panel = page.getByLabel(/Карточка .* «Работа»/)
  await panel.getByLabel('Дата обязательства').fill('2026-09-01')
  await expect(panel.getByRole('button', { name: 'Снять обязательство' })).toBeVisible()
  expect(await contextless(page), 'панель карточки').toEqual([])
  await page.getByRole('button', { name: 'Закрыть' }).first().click()

  await page.getByRole('button', { name: '+ итерация' }).click()
  await page.getByPlaceholder('Название').fill('Неделя 34')
  await page.getByLabel('Начало').fill('2026-08-10')
  await page.getByLabel('Конец').fill('2026-08-16')
  await page.getByRole('button', { name: 'Завести итерацию', exact: true }).click()
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
  await page.getByRole('button', { name: 'Завести подразделение', exact: true }).click()
  await page.getByRole('button', { name: '▸ Продажи' }).click()
  await page.getByLabel('Добавить в подразделение').selectOption({ index: 1 })
  await expect(page.getByRole('button', { name: /^Убрать из состава/ })).toBeVisible()
  expect(await contextless(page), 'структура').toEqual([])

  // На «Команде» «Отозвать» стояло у приглашения и у ключа сразу.
  await page.getByRole('button', { name: 'Команда' }).click()
  await page.getByPlaceholder('Почта коллеги').fill('kolya@example.test')
  await page.getByRole('button', { name: 'Пригласить' }).click()
  // По подписи: у поля теперь есть настоящая подпись, а `placeholder`
  // с него снят — см. правило про имя поля выше.
  await page.getByLabel('Для чего ключ').fill('Обмен')
  await page.getByRole('button', { name: 'Завести ключ', exact: true }).click()
  await expect(page.getByRole('button', { name: /^Отозвать ключ/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /^Отозвать приглашение/ })).toBeVisible()
  expect(await contextless(page), 'команда').toEqual([])
})
