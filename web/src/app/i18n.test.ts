// Ни одной русской подписи вне каталога языка (ROADMAP 30.2).
//
// Строка, набранная прямо в разметке, на английском экране осталась бы
// русской — и заметили бы её только глазами, на том экране, где она
// стоит. Проверка читает исходники так же, как `actions.test.ts`:
// строковые литералы и текст между тегами, комментарии вырезаны.
//
// Исключения названы поимённо, и причина у каждого своя: это не подписи.

import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { test } from 'node:test'
import { en as enMain } from '../shared/i18n/en.ts'
import { ru as ruMain } from '../shared/i18n/ru.ts'
import { ALL_SECTIONS } from '../shared/i18n/index.ts'

/** Каталог целиком — основная часть и разделы экранов. */
async function целиком(язык: 'ru' | 'en'): Promise<Record<string, unknown>> {
  const out: Record<string, unknown> = { ...(язык === 'ru' ? ruMain : enMain) }
  for (const раздел of ALL_SECTIONS) {
    const модуль = (await import(`../shared/i18n/${язык}/${раздел}.ts`)) as Record<string, unknown>
    out[раздел] = модуль[раздел]
  }
  return out
}
const ru = await целиком('ru')
const en = await целиком('en')

const КИРИЛЛИЦА = /[А-Яа-яЁё]/

/** Не подписи, а клавиши: Ctrl+K и E в русской раскладке — «л» и «у». */
const РАСКЛАДКА = new Set(["'л'", "'у'"])

function исходники(): { путь: string; текст: string }[] {
  const корень = new URL('../', import.meta.url)
  const out: { путь: string; текст: string }[] = []
  const обойти = (каталог: URL, префикс: string) => {
    for (const запись of readdirSync(каталог, { withFileTypes: true })) {
      if (запись.isDirectory()) {
        if (запись.name === 'i18n') continue
        обойти(new URL(`${запись.name}/`, каталог), `${префикс}${запись.name}/`)
        continue
      }
      if (!/\.tsx?$/.test(запись.name) || запись.name.includes('.test.')) continue
      out.push({ путь: `${префикс}${запись.name}`, текст: readFileSync(new URL(запись.name, каталог), 'utf8') })
    }
  }
  обойти(корень, '')
  return out
}

/** Комментарии — в пробелы той же длины: номера строк в отчёте остаются
 *  настоящими. Строки внутри литералов не трогаются. */
function безКомментариев(текст: string): string {
  let out = ''
  let i = 0
  let кавычка: string | null = null
  while (i < текст.length) {
    const c = текст[i]
    if (кавычка) {
      out += c
      if (c === '\\') {
        out += текст[i + 1] ?? ''
        i += 2
        continue
      }
      if (c === кавычка) кавычка = null
      i++
      continue
    }
    if (текст.startsWith('//', i)) {
      const конец = текст.indexOf('\n', i)
      const до = конец < 0 ? текст.length : конец
      out += ' '.repeat(до - i)
      i = до
      continue
    }
    if (текст.startsWith('/*', i)) {
      const конец = текст.indexOf('*/', i + 2)
      const до = конец < 0 ? текст.length : конец + 2
      out += текст.slice(i, до).replace(/[^\n]/g, ' ')
      i = до
      continue
    }
    if (c === "'" || c === '"' || c === '`') кавычка = c
    out += c
    i++
  }
  return out
}

test('русские подписи живут только в каталоге', () => {
  const найдено: string[] = []
  let файлов = 0
  for (const { путь, текст } of исходники()) {
    файлов++
    const чистый = безКомментариев(текст)
    const строкаОт = (at: number) => чистый.slice(0, at).split('\n').length
    for (const m of чистый.matchAll(/'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"|`(?:[^`\\]|\\.)*`/g)) {
      // Подстановки `${…}` — код, а не текст: русское имя переменной
      // внутри шаблона подписью не является.
      const текст = m[0].replace(/\$\{[^}]*\}/g, '')
      if (КИРИЛЛИЦА.test(текст) && !РАСКЛАДКА.has(m[0])) {
        найдено.push(`${путь}:${строкаОт(m.index ?? 0)} ${m[0].slice(0, 60)}`)
      }
    }
    // Текст между тегами. Без фигурных скобок: `{t.x.y}` — уже ссылка.
    for (const m of чистый.matchAll(/>([^<>{}]*[А-Яа-яЁё][^<>{}]*)[<{]/g)) {
      // Выражения со своими русскими именами переменных — не текст разметки.
      if (/[=;()]/.test(m[1])) continue
      найдено.push(`${путь}:${строкаОт(m.index ?? 0)} ${m[1].trim().slice(0, 60)}`)
    }
  }
  assert.ok(файлов > 50, `прочитано ${файлов} файлов — обход потерял исходники`)
  assert.deepEqual(найдено, [], `русский текст вне каталога:\n${найдено.join('\n')}`)
})

/** Ключи английского каталога совпадают с русскими — это проверяет и
 *  TypeScript, но только при сборке; здесь — тем же прогоном, что
 *  и остальные проверки, и с именем недостающего ключа. */
function ключи(узел: unknown, путь = ''): string[] {
  if (!узел || typeof узел !== 'object') return [путь]
  return Object.entries(узел as Record<string, unknown>).flatMap(([k, v]) =>
    typeof v === 'function' ? [`${путь}.${k}()`] : ключи(v, `${путь}.${k}`),
  )
}

test('английский каталог повторяет русский ключ в ключ', () => {
  assert.deepEqual(ключи(en).sort(), ключи(ru).sort())
})

test('английский каталог не содержит кириллицы, кроме названия русского языка', () => {
  const русские: string[] = []
  const обойти = (узел: unknown, путь: string) => {
    if (typeof узел === 'string') {
      if (КИРИЛЛИЦА.test(узел) && путь !== '.lang.ru') русские.push(`${путь}: ${узел}`)
      return
    }
    if (typeof узел === 'function') {
      const пример = (узел as (...a: unknown[]) => unknown)('x', 1, 1, 1)
      if (typeof пример === 'string' && КИРИЛЛИЦА.test(пример)) русские.push(`${путь}(): ${пример}`)
      return
    }
    if (узел && typeof узел === 'object') {
      for (const [k, v] of Object.entries(узел)) обойти(v, `${путь}.${k}`)
    }
  }
  обойти(en, '')
  assert.deepEqual(русские, [])
})
