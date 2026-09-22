// Поддельный YouGile для сквозных сценариев и снимков (ROADMAP 23.3).
//
// Отвечает так, как описан API v2 в двух независимых клиентах (см.
// internal/importer/yougile): вход по почте и паролю, компании, ключ,
// проекты, доски, колонки, люди, стикеры, задачи постранично. Настоящий
// YouGile в проверках недоступен и не нужен: проверяется наш экран,
// а не их сервер. Поднимается Playwright'ом рядом с приложением.
import { createServer } from 'node:http'

const PORT = Number(process.env.FAKE_YOUGILE_PORT ?? 8097)
const KEY = 'fake-yougile-key'
const day = 24 * 3600 * 1000
const ago = (days) => Date.now() - days * day

const data = {
  projects: [{ id: 'p-1', title: 'Логистика' }],
  boards: [
    { id: 'b-1', title: 'Склад', projectId: 'p-1' },
    { id: 'b-2', title: 'Закупки', projectId: 'p-1' },
  ],
  columns: [
    { id: 'c-1', title: 'Нужно сделать', boardId: 'b-1' },
    { id: 'c-2', title: 'В работе', boardId: 'b-1' },
    { id: 'c-3', title: 'Готово', boardId: 'b-1' },
  ],
  users: [
    { id: 'u-1', email: 'anna@example.test', realName: 'Анна' },
    { id: 'u-2', email: 'nikto@yougile.test', realName: 'Никто' },
  ],
  stickers: [
    { id: 's-1', name: 'Приоритет', states: [{ id: 'st-1', name: 'Высокий' }] },
    { id: 's-2', name: 'Участок', states: [{ id: 'st-2', name: 'Склад №2' }] },
  ],
  tasks: [
    { id: 't-1', title: 'Сверить остатки', columnId: 'c-1', timestamp: ago(20), assigned: ['u-1', 'u-2'],
      description: '<p>По складу <b>№2</b></p>', stickers: { 's-1': 'st-1', 's-2': 'st-2' },
      deadline: { deadline: Date.now() + 3 * day },
      checklists: [{ title: 'Шаги', items: [{ title: 'Выгрузить', isCompleted: true }, { title: 'Сверить' }] }] },
    { id: 't-2', title: 'Заказать поддоны', columnId: 'c-2', timestamp: ago(12), subtasks: ['t-9'] },
    { id: 't-3', title: 'Отчёт за август', columnId: 'c-3', timestamp: ago(40), completed: true, completedTimestamp: ago(30) },
    { id: 't-4', title: 'Старая задача', columnId: 'c-1', archived: true },
  ],
}

const send = (res, status, body) => {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(body === undefined ? '' : JSON.stringify(body))
}
const page = (res, items, url) => {
  const offset = Number(url.searchParams.get('offset') ?? 0)
  const limit = Number(url.searchParams.get('limit') ?? 50)
  const content = items.slice(offset, offset + limit)
  send(res, 200, { paging: { count: items.length, offset, limit, next: offset + limit < items.length }, content })
}

createServer((req, res) => {
  let raw = ''
  req.on('data', (c) => (raw += c))
  req.on('end', () => {
    const url = new URL(req.url, 'http://x')
    const path = url.pathname.replace(/^\/api-v2\//, '')
    const body = raw ? JSON.parse(raw) : {}
    if (path.startsWith('auth/')) {
      if (body.login !== 'anna@yougile.test' || body.password !== 'parol12345') return send(res, 401, {})
      if (path === 'auth/companies') return page(res, [{ id: 'co-1', name: 'Северная логистика' }], url)
      if (path === 'auth/keys/get') return send(res, 200, [])
      if (path === 'auth/keys') return send(res, 201, { key: KEY })
    }
    if (req.headers.authorization !== `Bearer ${KEY}`) return send(res, 401, {})
    const q = (name) => url.searchParams.get(name)
    switch (path) {
      case 'projects': return page(res, data.projects, url)
      case 'boards': return page(res, data.boards, url)
      case 'columns': return page(res, data.columns.filter((c) => !q('boardId') || c.boardId === q('boardId')), url)
      case 'users': return page(res, data.users, url)
      case 'string-stickers': return page(res, data.stickers, url)
      case 'tasks': return page(res, data.tasks.filter((t) => t.columnId === q('columnId')), url)
      default: return send(res, 404, {})
    }
  })
}).listen(PORT, '127.0.0.1')
