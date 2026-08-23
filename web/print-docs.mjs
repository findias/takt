// Печать документации в PDF.
//
// Печатает тот же файл, что читают в браузере: `docs/html/всё.html`.
// Отдельный набор текстов «для печати» разошёлся бы с экранным —
// как разошлись бы и HTML с markdown, если бы их набирали порознь.
//
// Браузер берётся из Playwright, который уже стоит ради сквозных
// проверок: ставить второй инструмент ради одной кнопки «печать»
// значило бы платить за него при каждой установке зависимостей.
import { chromium } from '@playwright/test'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const источник = resolve('..', 'docs', 'html', 'всё.html')
const цель = resolve('..', 'docs', 'html', 'доска.pdf')

// Системный Chrome, а не скачанный Chromium: так же делают сквозные
// проверки — в закрытом контуре второй браузер взять неоткуда.
const браузер = await chromium.launch({ channel: 'chrome' })
const страница = await браузер.newPage()
await страница.goto(pathToFileURL(источник).href, { waitUntil: 'load' })
// Печатный стиль — тот же, что у страницы: цвета, разрывы и скрытое
// оглавление описаны в `@media print`, а не вторым набором правил.
await страница.emulateMedia({ media: 'print' })
await страница.pdf({
  path: цель,
  format: 'A4',
  printBackground: true,
  margin: { top: '18mm', bottom: '18mm', left: '16mm', right: '16mm' },
  displayHeaderFooter: true,
  headerTemplate: '<div></div>',
  // Номер страницы обязателен: печатное содержание без него
  // отвечает «раздел где-то там».
  footerTemplate:
    '<div style="width:100%;font-size:8pt;color:#666;padding:0 16mm;' +
    'display:flex;justify-content:space-between">' +
    '<span>Доска — документация</span>' +
    '<span class="pageNumber"></span></div>',
})
// Памятка обещает быть одной страницей — обещание проверяется здесь же.
// Лист A4 при 96 точках на дюйм — 794×1123 пикселя; поля вычитаем те же,
// с какими печатаем.
const памятка = await браузер.newPage()
await памятка.setViewportSize({ width: 794 - 2 * 60, height: 1123 - 2 * 68 })
await памятка.goto(pathToFileURL(resolve('..', 'docs', 'html', 'памятка.html')).href)
await памятка.emulateMedia({ media: 'print' })
const высота = await памятка.evaluate(() => ({
  нужно: document.documentElement.scrollHeight,
  влезает: window.innerHeight,
}))
await браузер.close()

console.log(`  ${цель.split('/').slice(-3).join('/')}`)
if (высота.нужно > высота.влезает) {
  console.error(
    `памятка не помещается на лист: ${высота.нужно} px при ${высота.влезает} px. ` +
      'Она обещает быть одной страницей — сократите её или снимите обещание.',
  )
  process.exit(1)
}
console.log(`  памятка помещается на лист: ${высота.нужно} из ${высота.влезает} px`)
