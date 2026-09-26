// Выбор файла подписан языком интерфейса, а не браузера (35.4): родное
// поле на английском экране говорило «Выберите файл». Проверяется, что
// подписи наши, что поле осталось настоящим — названо подписью и
// принимает файл, — и что видимое имя следует за выбором.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { FilePick } from './FilePick.tsx'

it('подписан каталогом, передаёт файл наружу и показывает его имя', async () => {
  const onChoose = vi.fn()
  render(<FilePick label="Файл пакета" accept=".takt" onChoose={onChoose} />)
  expect(screen.getByText('Выбрать файл')).toBeTruthy()
  expect(screen.getByText('Файл не выбран')).toBeTruthy()

  // Поле по-прежнему названо подписью. Поиск не точный: библиотека
  // тестов берёт в имя и скрытые от диктора отражения, браузер — нет.
  const input = screen.getByLabelText('Файл пакета', { exact: false }) as HTMLInputElement
  expect(input.type).toBe('file')
  expect(input.accept).toBe('.takt')

  const file = new File(['{}'], 'yougile.takt')
  await userEvent.upload(input, file)
  expect(onChoose).toHaveBeenCalledWith(file)
  expect(screen.getByText('yougile.takt')).toBeTruthy()
  expect(screen.queryByText('Файл не выбран')).toBeNull()
})

it('недоступный не принимает файл', async () => {
  const onChoose = vi.fn()
  render(<FilePick label="Файл" accept=".csv" disabled onChoose={onChoose} />)
  const input = screen.getByLabelText('Файл', { exact: false }) as HTMLInputElement
  expect(input.disabled).toBe(true)
  await userEvent.upload(input, new File(['a'], 'a.csv'))
  expect(onChoose).not.toHaveBeenCalled()
})
