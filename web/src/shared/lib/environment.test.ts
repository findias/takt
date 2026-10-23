import assert from 'node:assert/strict'
import { test } from 'node:test'
import { environmentLabel, setEnvironmentLabel, withEnvironment } from './environment.ts'

test('метка среды стоит первым словом заголовка и не удваивается', () => {
  setEnvironmentLabel(' ИФТ ')
  assert.equal(environmentLabel(), 'ИФТ')
  assert.equal(withEnvironment('ПОСТ-14 · Доска'), '[ИФТ] ПОСТ-14 · Доска')
  assert.equal(withEnvironment('[ИФТ] ПОСТ-14 · Доска'), '[ИФТ] ПОСТ-14 · Доска')
  setEnvironmentLabel('')
  assert.equal(withEnvironment('ПОСТ-14 · Доска'), 'ПОСТ-14 · Доска')
})
