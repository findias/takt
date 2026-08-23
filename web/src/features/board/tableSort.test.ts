// Порядок строк в таблице.
//
// Проверяется не сама сортировка — сравнить два числа умеет каждый, —
// а правило про пустое значение. Оно из типа не выводится: «нет срока»
// и «нет оценки» это не ноль и не самая ранняя дата, а отсутствие
// ответа, и наверху ему делать нечего. Ошибка здесь не падает
// и не видна: список просто выстраивается не тем концом, а заметить
// это можно, только зная, как он должен был выстроиться.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SORT_DIRECTION, comparator, parseSort, sortToQuery } from './tableSort.ts'
import type { Card } from '../../shared/api/index.ts'

const NOW = Date.parse('2026-08-17T12:00:00Z')

function card(id: string): Card {
  return {
    id,
    number: `ДОСК-${id}`,
    columnId: 'первая',
    position: 'a0',
    title: id,
    description: '',
    version: 1,
    columnEnteredAt: '2026-08-15T12:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
    comments: 0,
    priority: 'medium',
    dueOn: null,
    doneAt: null,
  }
}

/** Карточка, начатая столько-то дней назад. */
function started(id: string, days: number, extra: Partial<Card> = {}): Card {
  return {
    ...card(id),
    startedAt: new Date(NOW - days * 86_400_000).toISOString(),
    ...extra,
  }
}

const POSITION = { первая: 0, вторая: 1 }

function order(sort: Parameters<typeof comparator>[0], cards: Card[]): string[] {
  return [...cards].sort(comparator(sort, POSITION)).map((c) => c.id)
}

test('по сроку: ближний срок сверху, а «без срока» — вниз', () => {
  const cards = [
    started('без срока', 20),
    started('дальний', 1, { dueOn: '2026-09-01' }),
    started('ближний', 1, { dueOn: '2026-08-18' }),
  ]

  // «Без срока» не встаёт первым, хотя пустая строка меньше любой даты
  // и хотя карточка старше всех: обязательства у неё нет, и в разговоре
  // «что мы обещали» ей место последнее.
  assert.deepEqual(order('due', cards), ['ближний', 'дальний', 'без срока'])
})

test('по сроку: одинаковые даты разводит возраст', () => {
  // Иначе строки с одной датой выстраиваются в случайном порядке
  // и глазу не за что взяться — тем же доводом, что у колонки
  // и уровня.
  const cards = [
    started('младшая', 2, { dueOn: '2026-08-20' }),
    started('старшая', 9, { dueOn: '2026-08-20' }),
  ]
  assert.deepEqual(order('due', cards), ['старшая', 'младшая'])
})

test('по возрасту: неначатая внизу — она не стареет, она ждёт', () => {
  const cards = [card('неначатая'), started('младшая', 2), started('старшая', 9)]
  assert.deepEqual(order('age', cards), ['старшая', 'младшая', 'неначатая'])
})

test('по оценке: неоценённая внизу, а не наравне с нулём', () => {
  const cards = [
    started('неоценённая', 5),
    started('мелкая', 5, { estimate: 1 }),
    started('крупная', 5, { estimate: 8 }),
  ]
  assert.deepEqual(order('estimate', cards), ['крупная', 'мелкая', 'неоценённая'])
})

test('по колонке — порядком на доске, а не по имени', () => {
  const cards = [started('вторая колонка', 1, { columnId: 'вторая' }), started('первая колонка', 1)]
  assert.deepEqual(order('column', cards), ['первая колонка', 'вторая колонка'])
})

test('по уровню: наивысший сверху, внутри уровня — по возрасту', () => {
  const cards = [
    started('фоновая', 30, { priority: 'low' }),
    started('младшая срочная', 1, { priority: 'highest' }),
    started('старшая срочная', 9, { priority: 'highest' }),
  ]
  assert.deepEqual(order('priority', cards), [
    'старшая срочная',
    'младшая срочная',
    'фоновая',
  ])
})

test('сортировка живёт в адресе, а умолчание в нём не пишется', () => {
  assert.equal(parseSort(new URLSearchParams('sort=due')), 'due')
  // Выдуманное значение не роняет вид и не остаётся в состоянии:
  // адрес правят руками и присылают ссылкой.
  assert.equal(parseSort(new URLSearchParams('sort=выдуманная')), 'age')

  assert.equal(sortToQuery('due').toString(), 'sort=due')
  assert.equal(sortToQuery('age', new URLSearchParams('sort=due')).toString(), '')
})

test('заголовок объявляет то направление, в котором строки и правда идут', () => {
  // Направление читает диктор, а порядок строк видит зрячий — и до
  // сегодня они расходились в трёх случаях из пяти: в разметке стояло
  // «по возрастанию» на все сортировки разом, включая возраст, оценку
  // и приоритет, которые идут сверху вниз от большего к меньшему.
  // Ошибка не падает и не видна: чтобы её заметить, надо было слушать
  // столбец и одновременно на него смотреть.
  //
  // Проверяется не текст, а совпадение: у каждой сортировки берутся две
  // карточки с заведомо разной величиной в её столбце, и порядок,
  // который даёт `comparator`, сверяется с объявленным направлением.
  const пары: Record<Parameters<typeof comparator>[0], [Card, Card]> = {
    // Слева — та, у которой величина столбца меньше.
    age: [started('младшая', 2), started('старшая', 9)],
    column: [started('слева', 3), started('справа', 3, { columnId: 'вторая' })],
    due: [started('раньше', 3, { dueOn: '2026-08-18' }), started('позже', 3, { dueOn: '2026-09-01' })],
    estimate: [started('мелкая', 3, { estimate: 1 }), started('крупная', 3, { estimate: 8 })],
    priority: [started('фоном', 3, { priority: 'low' }), started('наивысшая', 3, { priority: 'highest' })],
  }

  for (const [sort, [меньше, больше]] of Object.entries(пары) as [
    Parameters<typeof comparator>[0],
    [Card, Card],
  ][]) {
    const первая = order(sort, [больше, меньше])[0]
    const ожидаем = SORT_DIRECTION[sort] === 'ascending' ? меньше.id : больше.id
    assert.equal(
      первая,
      ожидаем,
      `${sort}: объявлено ${SORT_DIRECTION[sort]}, а сверху оказалась «${первая}»`,
    )
  }
})
