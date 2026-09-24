// Параметры выгрузки: чистые функции, проверяются без браузера.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { addressQuery, downloadHref, parseReportFilter, presetPeriod, reportQuery } from './reportFilter.ts'

const NOW = new Date(2026, 8, 24, 21, 30)

test('без параметров — последние 90 дней по местному календарю, и словом', () => {
  const f = parseReportFilter(new URLSearchParams(), NOW)
  assert.equal(f.to, '2026-09-24')
  assert.equal(f.from, '2026-06-27')
  assert.equal(f.period, 'days90')
  assert.deepEqual(f.boards, [])
})

test('период словом скользит с календарём, а серверу уходит датами', () => {
  const saved = 'period=lastQuarter&state=done'
  const september = parseReportFilter(new URLSearchParams(saved), NOW)
  const january = parseReportFilter(new URLSearchParams(saved), new Date(2027, 0, 10))
  assert.equal(september.from, '2026-04-01')
  assert.equal(january.from, '2026-10-01')
  assert.equal(addressQuery(january).toString(), saved)
  assert.equal(reportQuery(january).toString(), 'from=2026-10-01&to=2026-12-31&state=done')
})

test('адрес разбирается и собирается обратно тем же', () => {
  const raw = 'from=2026-07-01&to=2026-09-30&board=a%2Cb&priority=high&state=done&iteration=i&archived=1'
  const f = parseReportFilter(new URLSearchParams(raw), NOW)
  assert.deepEqual(f.boards, ['a', 'b'])
  assert.deepEqual(f.states, ['done'])
  assert.equal(f.iteration, 'i')
  assert.equal(f.period, null)
  assert.equal(addressQuery(f).toString(), raw)
})

test('испорченная ссылка даёт отчёт, а не отказ', () => {
  const f = parseReportFilter(new URLSearchParams('from=вчера&state=closed,done&priority=urgent&period=decade'), NOW)
  assert.equal(f.from, '2026-06-27')
  assert.deepEqual(f.states, ['done'])
  assert.deepEqual(f.priorities, [])
})

test('повтор и запятая — одно и то же', () => {
  const f = parseReportFilter(new URLSearchParams('label=x&label=y,z'), NOW)
  assert.deepEqual(f.labels, ['x', 'y', 'z'])
})

test('прошлый квартал — целиком, и в январе он прошлогодний', () => {
  assert.deepEqual(presetPeriod('lastQuarter', NOW), { from: '2026-04-01', to: '2026-06-30' })
  assert.deepEqual(presetPeriod('lastQuarter', new Date(2027, 0, 10)), {
    from: '2026-10-01',
    to: '2026-12-31',
  })
  assert.deepEqual(presetPeriod('quarter', NOW), { from: '2026-07-01', to: '2026-09-24' })
})

test('ссылка на файл несёт формат и отбор', () => {
  const f = parseReportFilter(new URLSearchParams('from=2026-09-01&to=2026-09-24&team=t'), NOW)
  assert.equal(downloadHref(f, 'csv'), '/api/reports/cards?from=2026-09-01&to=2026-09-24&team=t&format=csv')
})
