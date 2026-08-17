// Что видно, когда отрисовка упала.
//
// Проверка есть потому, что проверить это глазами нельзя: белый экран
// появляется только тогда, когда что-то уже сломалось, и в этот момент
// им никто не любуется. А случается он от чего угодно — разошедшихся
// клиента и сервера, неизвестного значения, чужой ошибки в библиотеке.

import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ErrorBoundary } from './ErrorBoundary.tsx'

function Broken(): never {
  throw new Error('поле приехало пустым')
}

beforeEach(() => {
  // React печатает пойманную ошибку сам; в выводе тестов это шум,
  // который читается как настоящая поломка.
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => vi.restoreAllMocks())

it('вместо белого экрана — слова, кнопка и текст ошибки', () => {
  render(
    <ErrorBoundary>
      <Broken />
    </ErrorBoundary>,
  )

  expect(screen.getByRole('heading', { name: /не отрисовался/i })).toBeTruthy()
  // Текст ошибки виден целиком: «что-то пошло не так» без подробностей
  // превращает починку в допрос.
  expect(screen.getByText('поле приехало пустым')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Перезагрузить' })).toBeTruthy()
})

it('пока ничего не упало, показывает то, что внутри', () => {
  render(
    <ErrorBoundary>
      <p>Доска</p>
    </ErrorBoundary>,
  )
  expect(screen.getByText('Доска')).toBeTruthy()
})
