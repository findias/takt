// Кто вошёл — и что бывает, когда это перестаёт быть правдой.
//
// Проверка есть потому, что оба случая проходом глазами ловятся один
// раз и потом молчат. Сессия кончается посреди работы — и человек
// остаётся на экране, где отказывает всё; страница возвращается кнопкой
// «назад» из памяти браузера — и показывает имя, организацию и роль
// того, кто уже вышел. Ни то, ни другое не видно ни в разметке,
// ни в типах: видно только по тому, какой экран остался на месте
// и что на нём написано.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { App } from './App.tsx'
import type { Principal } from '../shared/api/index.ts'

const ANNA: Principal = {
  id: 'u-1',
  email: 'anna@example.test',
  name: 'Анна Королёва',
  orgId: 'org-1',
  orgName: 'Северный проект',
  orgSlug: 'sever',
  role: 'owner',
  estimateUnit: 'points',
}

const BORIS: Principal = {
  ...ANNA,
  id: 'u-2',
  email: 'boris@example.test',
  name: 'Борис Дятлов',
  role: 'member',
}

/** Кто отвечает на «кто вошёл»: подменяется по ходу теста, потому что
 *  ровно это в жизни и меняется — сессия была и кончилась, за одним
 *  человеком в тот же браузер вошёл другой. */
let signedIn: Principal | null = ANNA

function reply(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

/** Сколько раз спросили этот адрес: по нему видно, перечитал ли экран
 *  своё после смены человека, или остался с чужими данными. */
function asked(path: string) {
  const mock = fetch as unknown as { mock: { calls: unknown[][] } }
  return mock.mock.calls.filter((c) => String(c[0]) === path).length
}

function restore(persisted: boolean) {
  window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted }))
}

beforeEach(() => {
  signedIn = ANNA
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/methods')
        return reply({ password: { enabled: true }, oidc: { enabled: false } })
      if (!signedIn) return reply({ error: 'сессия истекла, войдите заново' }, 401)
      if (path === '/api/me' || path === '/api/auth/login') return reply(signedIn)
      if (path === '/api/orgs')
        return reply({
          orgs: [{ orgId: signedIn.orgId, orgName: signedIn.orgName }],
          activeOrgId: signedIn.orgId,
        })
      return reply({ boards: [] })
    }),
  )
})

afterEach(() => vi.unstubAllGlobals())

it('отказ «нужно войти» на любом запросе возвращает на вход и объясняет, почему', async () => {
  render(<App />)
  await screen.findByText(ANNA.orgName)

  // Сессия кончилась на сервере — узнаём об этом, как и человек:
  // из ответа на очередной обычный запрос обычного экрана.
  signedIn = null
  await userEvent.click(screen.getByRole('button', { name: 'Показать архив' }))

  await waitFor(() => expect(screen.getByRole('heading', { name: 'Вход' })).toBeTruthy())
  expect(screen.getByText(/Сессия истекла/)).toBeTruthy()
  // Экрана организации не осталось: ни названия, ни имени, ни роли.
  expect(screen.queryByText(ANNA.orgName)).toBeNull()
  expect(screen.queryByText(ANNA.name)).toBeNull()
  // Фокус ушёл с кнопки, которой больше нет: обход с клавиатуры
  // не должен начинаться заново со всей страницы.
  expect(document.activeElement).toBe(screen.getByRole('heading', { name: 'Вход' }))
})

it('о конце сессии диктор узнаёт из живого региона, а не из появившегося узла', async () => {
  render(<App />)
  await screen.findByText(ANNA.orgName)

  signedIn = null
  await userEvent.click(screen.getByRole('button', { name: 'Показать архив' }))
  await screen.findByRole('heading', { name: 'Вход' })

  // Регион приезжает вместе с экраном и поначалу пуст: диктор объявляет
  // изменение существующей области, а узел, появившийся сразу с текстом,
  // пропускает. Плюс объявление, начатое в тот же миг, что и смена
  // экрана, перебивается ею же — потому текст ставится позже.
  const region = document.querySelector('.sr-only[role="status"]')
  expect(region).toBeTruthy()
  expect(region?.textContent).toBe('')
  await waitFor(() => expect(region?.textContent).toMatch(/Сессия истекла/), { timeout: 2000 })
})

it('«назад» после смены человека показывает нового, а не прежнего', async () => {
  render(<App />)
  await screen.findByText(ANNA.name)
  const before = asked('/api/boards')

  // Страница пролежала в памяти браузера, пока в этом же браузере
  // вошёл другой человек.
  signedIn = BORIS
  restore(true)

  await screen.findByText(BORIS.name)
  expect(screen.queryByText(ANNA.name)).toBeNull()
  // Список досок перечитан: экран собран заново, а не подписан
  // новым именем поверх чужих данных.
  await waitFor(() => expect(asked('/api/boards')).toBeGreaterThan(before))
})

it('«назад» после выхода возвращает на вход, а не рабочий экран вышедшего', async () => {
  render(<App />)
  await screen.findByText(ANNA.name)

  signedIn = null
  restore(true)

  await screen.findByRole('heading', { name: 'Вход' })
  expect(screen.queryByText(ANNA.name)).toBeNull()
})

it('обычный переход на страницу ничего не переспрашивает', async () => {
  render(<App />)
  await screen.findByText(ANNA.name)
  const before = asked('/api/me')

  restore(false)
  await new Promise((r) => setTimeout(r, 0))

  expect(asked('/api/me')).toBe(before)
})

it('пока никто не вошёл, вход не рассказывает про кончившуюся сессию', async () => {
  signedIn = null
  render(<App />)

  await screen.findByRole('heading', { name: 'Вход' })
  expect(screen.queryByText(/Сессия истекла/)).toBeNull()
})

it('объяснение снимается: на форме новой организации его нет, после входа — тоже', async () => {
  render(<App />)
  await screen.findByText(ANNA.orgName)
  signedIn = null
  await userEvent.click(screen.getByRole('button', { name: 'Показать архив' }))
  await screen.findByText(/Сессия истекла/)

  // Создание организации — не продолжение прерванной работы:
  // рассказывать там про чужую кончившуюся сессию незачем.
  await userEvent.click(screen.getByRole('button', { name: 'Создать новую организацию' }))
  expect(screen.queryByText(/Сессия истекла/)).toBeNull()

  await userEvent.click(screen.getByRole('button', { name: 'У меня уже есть аккаунт' }))
  signedIn = ANNA
  await userEvent.type(screen.getByLabelText('Почта'), ANNA.email)
  await userEvent.type(screen.getByLabelText('Пароль'), 'parol12345')
  await userEvent.click(screen.getByRole('button', { name: 'Войти' }))

  await screen.findByText(ANNA.name)
  expect(screen.queryByText(/Сессия истекла/)).toBeNull()
})
