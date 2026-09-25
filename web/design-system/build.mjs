// Сборка дизайн-системы для Claude Design из кода продукта.
//
// Значения берутся из исходников, а не переписываются руками: токены —
// из `src/app/styles.css`, иконки — из `src/shared/ui/icons.tsx`,
// компоненты — из `src/shared/ui`, стиль — из свежей сборки `dist/`.
// Руками здесь написаны только пояснения к токенам: в CSS они живут
// комментариями, которые машиной не прочесть.
//
// Запуск: `make design-system` (сперва пересоберёт клиент). Результат —
// `design-system/out/`: дерево `project/` для публикации и иконки
// отдельными файлами (они публикуются загрузкой, а не файлами дерева).

import { build } from 'esbuild'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const WEB = path.join(HERE, '..')
const OUT = path.join(HERE, 'out')
const PROJECT = path.join(OUT, 'project')

fs.rmSync(OUT, { recursive: true, force: true })
fs.mkdirSync(path.join(PROJECT, 'components'), { recursive: true })

// --- токены -------------------------------------------------------------

const css = fs.readFileSync(path.join(WEB, 'src/app/styles.css'), 'utf8')
const block = (selector) => {
  const at = css.indexOf(`\n${selector} {`)
  if (at < 0) throw new Error(`в styles.css нет блока ${selector}`)
  const body = css.slice(at, css.indexOf('\n}', at))
  const decls = {}
  for (const [, name, value] of body.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/--([\w-]+):\s*([^;]+);/g))
    decls[name] = value.replace(/\s+/g, ' ').trim()
  return decls
}
const root = block(':root')
const dark = block(":root[data-theme='dark']")

// Формат системы не читает `rgb(0 0 0 / .35)` через косую черту
// надёжно — переводим в запятые, значение то же.
const rgba = (v) => v.replace(/rgb\((\d+) (\d+) (\d+) \/ ([\d.]+)\)/g, 'rgba($1, $2, $3, $4)')
const need = (name) => {
  if (!(name in root)) throw new Error(`в :root нет токена --${name}: он переименован или удалён`)
  used.add(name)
  return root[name]
}
const used = new Set()
const px = (name) => {
  const v = need(name)
  const calc = /^calc\((\d+)px \* var\(--scaling\)\)$/.exec(v)
  if (calc) return `${calc[1]}px`
  const rem = /^([\d.]+)rem$/.exec(v)
  if (rem) return `${Math.round(+rem[1] * 16)}px`
  return v
}
const pair = (name) => {
  const m = /^light-dark\((\S+), (\S+)\)$/.exec(need(name))
  if (!m) throw new Error(`--${name} — не light-dark(…): ${root[name]}`)
  return { light: m[1], dark: m[2] }
}

// Пояснения к цветам. Порядок — порядок в системе.
const COLORS = {
  paper: 'Фон страницы под всем остальным; полотно доски.',
  surface: 'Карточки, панели, поля, кнопки — всё, что лежит на фоне.',
  'surface-2': 'Заливка наведения и вторичные подложки: липкие шапки, выбранные строки.',
  ink: 'Основной текст и иконки. Тёмная шкала расставлена по APCA: Lc 93.',
  'ink-2': 'Второстепенный текст: строки сведений, ячейки при ключевом столбце. В тёмной Lc 78.',
  'ink-3': 'Приглушённые подписи и пояснения — самый мелкий текст на экране, поэтому и он держит 4.5:1 на колонке. В тёмной Lc 64.',
  rule: 'Только волосяные разделители строк и разделов. Не граница элемента управления: 3:1 не держит.',
  'rule-strong': 'Граница полей, флажков и кнопок (WCAG 1.4.11: 3:1 на любой подложке).',
  column: 'Заливка колонки доски — углубление: темнее фона в обеих темах, чтобы карточка читалась приподнятой.',
  'column-rule': 'Обводка колонки: колонки разделяет она, а не заливка (3:1 к фону и к заливке колонки).',
  accent: 'Единственное главное действие экрана, ссылки, кольцо фокуса, отмеченный флажок.',
  'accent-soft': 'Выделенная карточка, подложка текущей вкладки, подсветка места сброса.',
  warn: 'Работа стоит или обещание нарушено: блокировка, просроченное обязательство, ошибка. Беречь для этого.',
  'warn-soft': 'Подложка под текстом warn: сводка отказов, пометка блокировки.',
  caution: '«Обратите внимание, но работа идёт»: дольше обещанного, превышен лимит колонки. 4.5:1 к surface и caution-soft.',
  'caution-soft': 'Подложка под текстом caution.',
}
const colors = Object.entries(COLORS).map(([name, usage]) => ({ name, value: pair(name), usage }))
colors.push({ name: 'scrim', value: rgba(need('scrim')), usage: 'Гасит содержимое под модальной панелью или диалогом. Одно в обеих темах.' })

// Метки: тон различает, светлота и насыщенность общие по рецепту `.chip`.
// Рецепт посчитан здесь в готовые oklch(): формат системы var() не читает.
const TONES = ['slate', 'green', 'blue', 'violet', 'rose', 'amber', 'teal', 'brown']
const recipe = (from) => ({ bgL: from['chip-bg-l'], bgC: from['chip-bg-c'], fgL: from['chip-fg-l'], fgC: from['chip-fg-c'] })
const light = recipe(Object.fromEntries(['chip-bg-l', 'chip-bg-c', 'chip-fg-l', 'chip-fg-c'].map((n) => [n, need(n)])))
const darkRecipe = recipe(dark)
if (Object.values(darkRecipe).some((v) => v === undefined)) throw new Error('в тёмной теме нет рецепта меток --chip-*')
const slate = /\.chip--slate \{[^}]*--chip-c: ([\d.]+);[^}]*--chip-fg-c-own: ([\d.]+);/.exec(css)
if (!slate) throw new Error('не найдена своя насыщенность .chip--slate')
for (const tone of TONES) {
  const h = need(`hue-${tone}`)
  const own = tone === 'slate'
  const c = (theme, key) => (own ? (key === 'bgC' ? slate[1] : slate[2]) : theme[key])
  colors.push({
    name: `label-${tone}-bg`,
    value: { light: `oklch(${light.bgL} ${c(light, 'bgC')} ${h})`, dark: `oklch(${darkRecipe.bgL} ${c(darkRecipe, 'bgC')} ${h})` },
    usage: own
      ? 'Заливка нейтральной метки. Насыщенность своя: от green его отделяют 2° тона, различает только она.'
      : `Заливка метки, тон ${tone} (${h}°). Светлота общая по рецепту для всех восьми.`,
  })
  colors.push({
    name: `label-${tone}-fg`,
    value: { light: `oklch(${light.fgL} ${c(light, 'fgC')} ${h})`, dark: `oklch(${darkRecipe.fgL} ${c(darkRecipe, 'fgC')} ${h})` },
    usage: `Текст на label-${tone}-bg.`,
  })
}
for (let i = 0; i < 8; i++) {
  const h = need(`avatar-hue-${i}`)
  colors.push({
    name: `avatar-${i}`,
    value: `oklch(${need('avatar-l')} ${need('avatar-c')} ${h})`,
    usage: `Заливка аватара, тон ${i} (${h}°): выбирается по имени, буквы белые, одна в обеих темах.`,
  })
}

const token = (name, value, usage) => ({ name, value, usage })
const shadow = (name, usage) => {
  need(name)
  if (!dark[name]) throw new Error(`тёмная тема не перекрывает --${name}`)
  return { name, value: { light: rgba(root[name]), dark: rgba(dark[name]) }, usage }
}
// Длительности ролей — ссылки на две величины; в систему идут величины,
// а роли названы в пояснениях.
for (const role of ['motion-moved', 'motion-arrived', 'motion-grew']) need(role)

const tokens = {
  name: 'Takt',
  version: 1,
  meta: { source: 'local', repo: 'findias/takt', package: 'web', paths: { tokens: ['web/src/app/styles.css'], docs: ['.claude/skills/board-design/SKILL.md', 'web/src/shared/ui/'] }, synced: new Date().toISOString().slice(0, 10) },
  color: { themes: [{ id: 'light', name: 'Light' }, { id: 'dark', name: 'Dark' }], tokens: colors },
  type: {
    fonts: [],
    families: { ui: need('font-ui'), mono: need('font-mono') },
    groups: [
      { name: 'Interface', family: 'ui', styles: [
        // Насыщенность и интерлиньяж заголовков заданы в правилах h1 и панели, а не токенами.
        { name: 'text-xl', fontSize: px('text-xl'), lineHeight: '28px', fontWeight: 650, sample: 'Платформа · Доска', usage: 'Заголовок страницы (h1), один на экран.' },
        { name: 'text-lg', fontSize: px('text-lg'), lineHeight: '24px', fontWeight: 650, sample: 'Итерация 14 закрыта', usage: 'Заголовки панелей и диалогов.' },
        { name: 'text', fontSize: px('text'), lineHeight: '24px', fontWeight: 400, sample: 'Перенести оплату на новый шлюз', usage: 'Основной текст и названия карточек. Интерлиньяж 1.6 = 24px: тот же шаг, что у отступов и цели нажатия.' },
        { name: 'text-strong', fontSize: px('text'), lineHeight: '24px', fontWeight: 600, sample: 'Завести карточку', usage: 'Подпись главной кнопки, названия колонок, выделение.' },
        { name: 'text-sm', fontSize: px('text-sm'), lineHeight: '20px', fontWeight: 400, sample: 'ПОСТ-128 · 5 дн. в работе', usage: 'Строки сведений, метки, пояснения. Табличные цифры там, где числа сравнивают.' },
      ] },
      { name: 'Machine', family: 'mono', styles: [
        { name: 'mono', fontSize: px('text-sm'), lineHeight: '20px', fontWeight: 400, sample: 'POST v1.14.2', usage: 'Ключ доски, версия, идентификатор — не для прозы.' },
      ] },
    ],
  },
  spacing: { note: 'Каждый шаг — calc(N × --scaling); компактная плотность ставит --scaling: 0.85. Промежуточных значений нет.', tokens: [
    token('space-1', px('space-1'), 'Зазор внутри меток и между иконкой и подписью.'),
    token('space-2', px('space-2'), 'Между карточками в колонке; боковой отступ метки.'),
    token('space-3', px('space-3'), 'Боковой отступ кнопки; поле карточки.'),
    token('space-4', px('space-4'), 'Поля панели и колонки.'),
    token('space-5', px('space-5'), 'Между группами формы.'),
    token('space-6', px('space-6'), 'Между разделами страницы; столько же — цель нажатия (--target).'),
    token('space-8', px('space-8'), 'Поля страницы на широком экране.'),
  ] },
  radius: { tokens: [
    token('radius', px('radius'), 'Кнопки, поля, карточки, кольцо фокуса.'),
    token('radius-lg', px('radius-lg'), 'Панели, диалоги, колонки.'),
    // Токена в CSS нет: метки и аватары пишут 999px прямо в правиле.
    token('radius-pill', '999px', 'Только метки и аватары.'),
  ] },
  shadow: { note: 'Три, и больше не нужно. Тёмная тема перекрывает все три: тень на тёмном работает иначе.', tokens: [
    shadow('shadow', 'Карточка под курсором.'),
    shadow('shadow-drag', 'Карточка, которую тащат.'),
    shadow('shadow-lg', 'Край боковой панели.'),
  ] },
  zIndex: { note: 'Шаг десятками. Верхний слой браузера (dialog showModal, popover) выше любого из них: диалог закрывается до тоста, который докладывает о его действии.', tokens: [
    token('layer-raised', need('layer-raised'), 'Карточка под курсором, липкие вкладки.'),
    token('layer-notice', need('layer-notice'), 'Полоса уведомлений приложения; панель не перекрывает.'),
    token('layer-panel', need('layer-panel'), 'Боковая панель и её затемнение.'),
    token('layer-control', need('layer-control'), 'Меню, полоса массовых действий: закрываются по Escape и возвращают фокус.'),
    token('layer-toast', need('layer-toast'), 'Всплывающие сообщения.'),
  ] },
  duration: { note: 'Длительностей две; движение только объясняет смену состояния. Роли: moved и grew берут slow, arrived — fast, pending — пульс заглушки.', tokens: [
    token('fast', need('fast'), 'motion-arrived: появилось сообщение.'),
    token('slow', need('slow'), 'motion-moved («карточка уехала — найди её там») и motion-grew («доля выросла»).'),
    token('motion-pending', need('motion-pending'), 'Пульс заглушки — единственное движение без перехода.'),
  ] },
  size: { tokens: [token('target', need('target'), 'Минимальная цель нажатия (WCAG 2.5.8 AA). Проверять кнопки-ссылки в плотных рядах.')] },
}

// Токены, которые в систему не идут, и почему. Всякий другой токен
// :root, не попавший в систему, — новый, и сборка о нём спрашивает:
// молча отстающая система хуже, чем упавшая сборка.
const SKIP = {
  scaling: 'множитель плотности; отступы записаны при 1',
  check: 'картинка галочки, не цвет',
  'panel-side-width': 'размер элемента, а не токен языка',
}
const unknown = Object.keys(root).filter((n) => !used.has(n) && !(n in SKIP))
if (unknown.length) throw new Error(`в styles.css новые токены: ${unknown.map((n) => '--' + n).join(', ')} — впишите их в build.mjs или в SKIP с причиной`)

fs.writeFileSync(path.join(PROJECT, 'tokens.json'), JSON.stringify(tokens, null, 2) + '\n')

// --- иконки -------------------------------------------------------------

// Файл-иконка залит цветом ink светлой темы: превью через <img> не знает currentColor.
const ICON_INK = pair('ink').light
const icons = fs.readFileSync(path.join(WEB, 'src/shared/ui/icons.tsx'), 'utf8')
const iconDir = path.join(OUT, 'icons')
fs.mkdirSync(iconDir)
let iconCount = 0
for (const [, name, body] of icons.matchAll(/export function (\w+)Icon\(props: IconProps\) \{\s*return \(\s*<Icon \{\.\.\.props\}>([\s\S]*?)<\/Icon>/g)) {
  if (/[{}]/.test(body)) throw new Error(`иконка ${name}: в разметке выражение JSX, перевести в SVG нельзя`)
  const file = name.replace(/([a-z])([A-Z])/g, '$1-$2').toLowerCase() + '.svg'
  const inner = body.trim().replace(/\n\s*/g, '\n  ')
  fs.writeFileSync(path.join(iconDir, file), `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="${ICON_INK}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">\n  ${inner}\n</svg>\n`)
  iconCount++
}

// --- тексты и превью ------------------------------------------------------

fs.cpSync(path.join(HERE, 'content'), PROJECT, { recursive: true })

// --- стиль ----------------------------------------------------------------

const assets = path.join(WEB, 'dist/assets')
const built = fs.existsSync(assets) ? fs.readdirSync(assets).filter((f) => /^index-.*\.css$/.test(f)) : []
if (built.length !== 1) throw new Error(`в dist/assets ждали один index-*.css, нашли ${built.length}: сперва npm run build`)
const bundleCss = fs.readFileSync(path.join(assets, built[0]), 'utf8')
if (/<\/style/i.test(bundleCss)) throw new Error('в стиле есть </style: он закрыл бы вставку')
fs.writeFileSync(path.join(PROJECT, 'components/bundle.css'), bundleCss)

// --- компоненты -----------------------------------------------------------

// React берёт у страницы (window.React 18 с jsDelivr): так требует формат
// системы, и превью не тащат вторую копию. JSX идёт через createElement.
const globals = {
  name: 'globals',
  setup(b) {
    b.onResolve({ filter: /^react(-dom)?(\/.*)?$/ }, (a) => ({ path: a.path, namespace: 'g' }))
    b.onLoad({ filter: /.*/, namespace: 'g' }, (a) => {
      if (a.path === 'react/jsx-runtime' || a.path === 'react/jsx-dev-runtime')
        return {
          loader: 'js',
          contents: `const R = window.React
const mk = (type, props, key) => R.createElement(type, key === undefined ? props : { ...props, key })
export const jsx = mk, jsxs = mk, jsxDEV = mk
export const Fragment = R.Fragment`,
        }
      return { loader: 'js', contents: `module.exports = window.${a.path.startsWith('react-dom') ? 'ReactDOM' : 'React'}` }
    })
  },
}
const result = await build({
  entryPoints: [path.join(HERE, 'entry.ts')],
  bundle: true,
  format: 'iife',
  globalName: 'Takt',
  minify: true,
  jsx: 'automatic',
  target: 'es2022',
  define: { 'process.env.NODE_ENV': '"production"' },
  plugins: [globals],
  write: false,
  legalComments: 'none',
})
// Карточка компонента в системе — та папка content/components, чьё превью
// рисует живой компонент; остальные (метка, карточка доски) статичны.
const live = fs
  .readdirSync(path.join(HERE, 'content/components'))
  .filter((d) => fs.readFileSync(path.join(HERE, 'content/components', d, 'preview.html'), 'utf8').includes('window.Takt'))
const header = `/* @ds-bundle: ${JSON.stringify({ format: 4, namespace: 'Takt', components: live.map((name) => ({ name })) })} */\n`
const js = header + result.outputFiles[0].text.replace(/^var Takt=/, 'window.Takt=')
for (const bad of ['</script', '<!--', 'import(', 'eval(', 'new Function'])
  if (js.includes(bad)) throw new Error(`в скрипте компонентов «${bad}»: формат системы такое запрещает`)
fs.writeFileSync(path.join(PROJECT, 'components/bundle.js'), js)

console.log(
  `дизайн-система: ${colors.length} цветов, ${iconCount} иконок, ${live.length} живых компонентов, ` +
    `скрипт ${(js.length / 1024).toFixed(0)} КБ → ${path.relative(process.cwd(), OUT)}`,
)
