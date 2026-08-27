// Печать документации в PDF.
//
// Печатает тот же файл, что читают в браузере: `docs/html/all.html`.
// Отдельный набор текстов «для печати» разошёлся бы с экранным —
// как разошлись бы и HTML с markdown, если бы их набирали порознь.
//
// Браузер берётся из Playwright, который уже стоит ради сквозных
// проверок: ставить второй инструмент ради одной кнопки «печать»
// значило бы платить за него при каждой установке зависимостей.
import { chromium } from '@playwright/test'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const источник = resolve('..', 'docs', 'html', 'all.html')
const цель = resolve('..', 'docs', 'html', 'takt.pdf')

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
    '<span>Takt — документация</span>' +
    '<span class="pageNumber"></span></div>',
})
// Памятка обещает быть одной страницей — обещание проверяется здесь же.
// Лист A4 при 96 точках на дюйм — 794×1123 пикселя; поля вычитаем те же,
// с какими печатаем.
//
// Памяток две: оригинал и перевод, и лист перерастает обычно перевод —
// русский текст длиннее. Но проверять только его значит не заметить,
// как вырос оригинал, поэтому меряются обе. Путь к переводу — через
// `ru/`: с тех пор как оригиналом стал английский, русские страницы
// собираются в свой каталог, и прежний путь `docs/html/памятка.html`
// не существует. Печать этим и падала, а вместе с ней — сборка
// комплекта для закрытого контура, куда PDF входит.
const памятки = [
  ['cheatsheet.html'],
  ['ru', 'памятка.html'],
]
const тесные = []
for (const части of памятки) {
  const памятка = await браузер.newPage()
  await памятка.setViewportSize({ width: 794 - 2 * 60, height: 1123 - 2 * 68 })
  await памятка.goto(pathToFileURL(resolve('..', 'docs', 'html', ...части)).href)
  await памятка.emulateMedia({ media: 'print' })
  const высота = await памятка.evaluate(() => ({
    нужно: document.documentElement.scrollHeight,
    влезает: window.innerHeight,
  }))
  const имя = части.join('/')
  if (высота.нужно > высота.влезает) {
    тесные.push(`${имя}: ${высота.нужно} px при ${высота.влезает} px`)
  } else {
    console.log(`  ${имя} помещается на лист: ${высота.нужно} из ${высота.влезает} px`)
  }
}
await браузер.close()

console.log(`  ${цель.split('/').slice(-3).join('/')}`)
if (тесные.length > 0) {
  console.error(
    `памятка не помещается на лист:\n  ${тесные.join('\n  ')}\n` +
      'Она обещает быть одной страницей — сократите её или снимите обещание.',
  )
  process.exit(1)
}
