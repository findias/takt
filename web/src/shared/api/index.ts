// Клиент API. Одна точка входа, чтобы обработка ошибок и таймаутов
// не расползалась по компонентам.

// Названия ролей и видимостей вынесены отдельно: этот модуль читают
// и там, где нельзя тянуть весь api, — разбор записей журнала идёт
// в проверках без сборщика. Реэкспорт, чтобы список остался один.
export { ROLE_NAMES, VISIBILITY_NAMES } from './names.ts'

/**
 * Короче этого пароль не примут.
 *
 * Повторяет `auth.MinPasswordLen` на сервере — судит по-прежнему он,
 * а здесь число нужно затем, чтобы сказать правило до отказа, а не
 * после: придумывать пароль и узнавать требование по отказу — значит
 * придумывать дважды.
 */
export const MIN_PASSWORD = 8

export type Role = 'owner' | 'member' | 'viewer'

/** Кто выполняет запрос и от имени какой организации. */
export type Principal = {
  id: string
  email: string
  name: string
  orgId: string
  orgName: string
  orgSlug: string
  role: Role
  /** В чём организация оценивает работу. Свойство организации, а не
   *  карточки: складывать часы с очками бессмысленно, а прогресс —
   *  это сложение. */
  estimateUnit: EstimateUnit
}

export type EstimateUnit = 'points' | 'hours' | 'days'

export type Membership = {
  orgId: string
  orgName: string
  orgSlug: string
  role: Role
}

export type Member = {
  userId: string
  name: string
  email: string
  role: Role
  /** Человек или ключ интеграции. Ключ состоит в организации ровно как
   *  человек — оттого его и видно в списке, — но роли ему не назначают
   *  и данных у него нет: он убирается отзывом самого ключа. */
  kind: 'person' | 'service'
  joinedAt: string
}

export type Invite = {
  id: string
  email: string
  role: Role
  expiresAt: string
  createdAt: string
  /** Приходит только в ответе на создание: в базе лежит лишь хеш токена. */
  link?: string
}

export type InviteInfo = {
  orgName: string
  email: string
  role: Role
  needsAccount: boolean
}


/** Подразделение. Дерево строится по parentId; depth приходит с сервера,
 *  чтобы показать, что глубже вкладывать уже нельзя. */
export type Team = {
  id: string
  name: string
  parentId: string | null
  depth: number
  members: number
  boards: number
  /** Узел заведён каталогом по SCIM: состав ведёт каталог, и вписанные
   *  руками исчезнут при следующей синхронизации. */
  fromDirectory: boolean
}

/** Убранное подразделение: из чего выбирают, что вернуть. */
export type ArchivedTeam = {
  id: string
  name: string
  /** Пусты у корневого. По идентификатору экран отвечает на вопрос
   *  «моя ли это область», по имени человек — на «чьё это». */
  parentId: string | null
  parentName: string
  /** Старший тоже в архиве — вернуть сейчас нельзя, сперва его. */
  parentArchived: boolean
  archivedAt: string
}

/** Доска подразделения: чем занят узел структуры. */
export type TeamBoard = {
  id: string
  name: string
  key: string
  visibility: Visibility
}

export type TeamMember = {
  userId: string
  name: string
  email: string
  /** Ведущий — тот, у кого есть полномочие администратора именно на этом
   *  узле. Отдельной пометки в составе нет: два поля, описывающие одно
   *  и то же, рано или поздно расходятся. */
  lead: boolean
  addedAt: string
}

/** Кто отвечает за поддерево. Полномочие над узлом, а не свойство
 *  человека: один и тот же человек бывает администратором направления
 *  и рядовым участником соседнего, а роль у него одна. */
export type TeamAdmin = {
  id: string
  userId: string
  name: string
  email: string
  teamId: string
  teamName: string
}

/** Наблюдение за поддеревом; без команды — за всей организацией. */
export type Observer = {
  id: string
  userId: string
  name: string
  email: string
  teamId: string | null
  teamName: string | null
}

/** Событие журнала переходов. */
export type BoardEvent = {
  id: number
  cardId: string
  /** Название карточки текущее, а не на момент события: иначе в ленте
   *  доски не понять, о чём речь. */
  cardTitle: string
  actor: string | null
  type: string
  payload: Record<string, unknown>
  at: string
}

export type Feed = { events: BoardEvent[]; next: number | null }

/** Запись административного журнала. */
export type AuditEntry = {
  id: number
  actor: string | null
  action: 'insert' | 'update' | 'delete'
  subject: string
  subjectId: string | null
  payload: Record<string, unknown>
  at: string
}

export type AuditPage = { entries: AuditEntry[]; next: number | null }

/** Реплика обсуждения. Ветки глубиной в один уровень: ответ на ответ
 *  читать невозможно. Удалённая реплика остаётся — на неё ссылаются
 *  ответы, — но без текста. */
export type Comment = {
  id: string
  cardId: string
  parentId: string | null
  author: string | null
  authorId: string | null
  body: string
  createdAt: string
  editedAt: string | null
  deleted: boolean
  mentions: string[]
}

/** Сервисный клиент: доступ к API не от человека. Токен принадлежит
 *  организации — интеграция живёт дольше того, кто её завёл. */
export type ApiClient = {
  id: string
  name: string
  prefix: string
  scopes: string[]
  /** Приходит только в ответе на создание: в базе лежит лишь хеш. */
  token?: string
  createdAt: string
  expiresAt: string | null
  lastUsedAt: string | null
}

/**
 * Подписка на события.
 *
 * Ключ подписи приходит один раз, при создании: подписываем им мы,
 * а хранить его обязан получатель — в базе у нас он есть, но показывать
 * его второй раз незачем.
 */
export type Webhook = {
  id: string
  name: string
  url: string
  events: string[]
  secret?: string
  /** Отключена после того, как получатель долго не отвечал. */
  disabled: boolean
  lastError: string | null
  createdAt: string
}

/** Одна попытка доставить событие. Журнал нужен затем, чтобы видеть,
 *  что именно не доехало, и досдать это, когда получателя починят. */
export type Delivery = {
  id: string
  webhookId: string
  event: string
  attempts: number
  delivered: boolean
  failed: boolean
  lastStatus: number | null
  lastError: string | null
  createdAt: string
  nextTry: string | null
}

export { WEBHOOK_EVENT_NAMES } from './events.ts'

export const SCOPE_NAMES: Record<string, string> = {
  'boards:read': 'Читать доски',
  'boards:write': 'Изменять доски',
  'structure:read': 'Читать структуру',
  'audit:read': 'Читать журнал',
  'scim:write': 'Заводить людей из каталога',
}

/** Разрешение каталога. Ключу с ним сервер выдаёт права владельца
 *  организации — их требуют политики базы при заведении людей, — и
 *  поэтому же не пускает такой ключ дальше /scim/v2 и не даёт
 *  сочетать это разрешение с остальными. */
export const DIRECTORY_SCOPE = 'scim:write'

/** Метрики потока. Считаются из отметок карточки, ничего не хранится
 *  посчитанным: хранимый показатель — это поле, которое никто
 *  не обновляет. */
export type FlowReport = {
  days: number
  /** Проценты, а не среднее: у времени цикла всегда длинный хвост. */
  cycleTime: { p50: number; p85: number; p95: number; count: number } | null
  /** Сами точки, а не только проценты: три случая по двадцать дней
   *  и двадцать по три дают одинаковую медиану и совершенно разный
   *  разговор на разборе. */
  finished: { id: string; title: string; finishedOn: string; days: number }[]
  throughput: { week: string; count: number }[]
  wip: number
  aging: { id: string; title: string; column: string; days: number; blocked: boolean }[]
  flow: { day: string; queued: number; inProgress: number; done: number }[]
  forecast: { cards: number; p50: number; p85: number; p95: number }[] | null
  /** Выброшенные не входят в пропускную способность — но молчать об их
   *  числе значит скрывать половину картины. */
  discarded: number
}

export type Visibility = 'org' | 'team' | 'private'


export type BoardAccess = {
  visibility: Visibility
  teamId: string | null
  teamName: string | null
  members: { userId: string; name: string; email: string }[]
  /** Весь ли состав в members. У закрытой доски политика показывает
   *  весь состав владельцу организации, остальным — их собственную
   *  строку; без этого признака список из одного читался бы как весь. */
  rosterComplete: boolean
}

/** Ответ на «что я пропустил». Каждый патч назван своей версией —
 *  выводить её из порядка на этой стороне значило бы хранить одно
 *  и то же в двух местах. full означает «патчами не догнать». */
export type Catchup = {
  version: number
  results: OperationResult[]
  full: boolean
}

export type BoardInfo = {
  id: string
  name: string
  /** Префикс номеров карточек доски: ПРО в ПРО-142. Задаётся при
   *  создании и не меняется. */
  key: string
  version: number
  /** Обещание доски: «85% работы проходит доску за 8 дней». Пусто — обещания нет. */
  sleDays: number | null
  sleProbability: number
  /** Можно ли в доску писать. Отвечает список досок — из него выбирают,
   *  куда поставить работу соседям. В снимке доски поля нет: там на этот
   *  вопрос не отвечают, и «нельзя» отличается от «не спрашивали». */
  writable?: boolean
  /** Кому доска видна и сколько на ней работы. Отвечает список досок:
   *  в дереве подразделений у той же доски написано «ПЛАТ · своей
   *  команде», а список знал одно название. Снимку доски эти ответы
   *  не нужны — там их и нет. */
  visibility?: Visibility
  /** Какому подразделению доска видна при видимости «своей команде».
   *  Без него строка списка отвечает «своей» тому, кто состоит
   *  в нескольких, — а это не ответ. */
  teamId?: string | null
  cards?: number
}
/** Вид колонки. Границы потока задаются отдельно: очередей и стадий работы
 *  бывает много, а началом и концом объявляются конкретные колонки. */
export type ColumnKind = 'queue' | 'in_progress' | 'done'

export type Column = {
  id: string
  name: string
  position: string
  kind: ColumnKind
  isStartedPoint: boolean
  isFinishedPoint: boolean
  policy: string
  wipLimit: number | null
  /** Мягкий лимит только подсвечивается, жёсткий отвечает конфликтом. */
  wipLimitHard: boolean
}

export type Card = {
  id: string
  /** Номер задачи: ПРО-142. Единственное имя карточки, которое можно
   *  назвать вслух и прислать в переписке. Не меняется никогда. */
  number: string
  columnId: string
  position: string
  title: string
  description: string
  version: number
  /** Момент входа в текущую колонку — из него считается старение карточки. */
  columnEnteredAt: string
  startedAt: string | null
  finishedAt: string | null
  /** Исход работы: done или discarded. Пока работа идёт — пусто.
   *  Отказ и завершение — разные факты: иначе пропускная способность
   *  считает выброшенное за сделанное. */
  outcome: 'done' | 'discarded' | null
  /** Оценка в единицах организации. Пусто — не оценена. */
  estimate: number | null
  /** Уровень приоритета: что важнее. Порядок карточек в колонке при
   *  этом остаётся ручным — он говорит, что взято следующим. */
  priority: Priority
  /** Дата обязательства: то, что обещано наружу. Пусто у большинства
   *  карточек, и это не «дата неизвестна», а «обязательства нет». */
  dueOn: string | null
  /** Отметка «сделано»: готовность, объявленная руками и не зависящая
   *  от колонки. Нужна разбиению — пункты вида «согласовать с юристами»
   *  по доске не ездят, — и входит в прогресс родителя. Поток считается
   *  по `finishedAt` и `outcome`: отметка их не подменяет. */
  doneAt: string | null
  /** Прогресс по подзадачам; пусто, если подзадач нет. `byWeight`
   *  говорит, чем он измерен: суммой оценок или штуками. */
  progress?: { done: number; total: number; byWeight: boolean }
  /** Открытая блокировка, если есть. `blockingCard` — работа, которая
   *  держит: чаще всего собственная часть этой же задачи. Ссылка, а не
   *  название: карточку переименуют, а по ссылке видно ещё и то, что
   *  с ней стало — сделана ли, стоит ли сама. */
  blocked?: { id: string; reason: string; blockedAt: string; blockingCard?: string }
  /** Сколько реплик в обсуждении. Нужно строке подзадачи на доске:
   *  у подзадачи своё обсуждение, и без числа о нём узнают, только
   *  зайдя внутрь. */
  comments: number
}

export type Priority = 'highest' | 'high' | 'medium' | 'low'

export type LinkKind = 'subtask' | 'blocks' | 'relates'

export const LINK_KIND_NAMES: Record<LinkKind, string> = {
  subtask: 'Подзадача',
  blocks: 'Блокирует',
  relates: 'Связана с',
}

export type Link = { fromCard: string; toCard: string; kind: LinkKind }

/** Карточка с другой доски: столько, сколько нужно, чтобы её узнать
 *  и понять, чья это команда. Недоступной доски здесь не будет. */
export type LinkedCard = {
  id: string
  title: string
  boardId: string
  boardName: string
  teamName: string | null
  outcome: 'done' | 'discarded' | null
  /** Отмечена сделанной руками. Отдельно от `outcome`: у соседей свои
   *  колонки, и «эта часть готова» они могут объявить, не двигая
   *  карточку. */
  done: boolean
  blocked: boolean
  /** Где она стоит у них. «Сделана или нет» — слишком грубо: «третью
   *  неделю в очереди» и «делают со вчера» так выглядят одинаково. */
  columnName: string
  columnKind: ColumnKind
  /** Обещание доски исполнителя. Срок принадлежит доске, на которой
   *  работа лежит: заказчик его видит и не двигает. */
  sleDays: number | null
  sleProbability: number
  /** Убрана в архив — то есть работу не взяли. Отличается от «доски вам
   *  не видно»: искать недоступную бесполезно, после отказа идут
   *  договариваться. */
  archived: boolean
}

/** Карточка, убранная с доски: чем она была и откуда ушла. */
export type ArchivedCard = {
  id: string
  number: string
  title: string
  columnId: string
  /** Название колонки, а не только идентификатор: колонку могли
   *  заархивировать следом, и тогда по идентификатору сказать нечего. */
  columnName: string
  archivedAt: string
  actor: string | null
  outcome: 'done' | 'discarded' | null
  /** Вернётся ли карточка. Ложь означает, что её колонка тоже в архиве:
   *  знать об этом надо до нажатия, а не после отказа. */
  restorable: boolean
}

/** Карточка в отчёте по итерации вместе с тем, что с ней стало
 *  к моменту отсчёта. */
export type IterationCard = {
  id: string
  number: string
  title: string
  estimate: number | null
  /** Доведена до конца к моменту отсчёта — не вообще: работа, законченная
   *  через неделю после закрытия спринта, в этом спринте не сделана. */
  done: boolean
  /** Вошла позже начала итерации. По первому входу: карточка, вышедшая
   *  и вернувшаяся, добавлена не дважды. */
  lateAdd: boolean
  /** Убрана из итерации до момента отсчёта — в составе её нет, но она
   *  в нём была, и об этом спрашивают на разборе. */
  dropped: boolean
  archived: boolean
}

/** Отчёт по итерации: то, ради чего вхождение сделано интервалом. */
export type IterationReport = {
  iteration: Iteration
  /** Момент, на который посчитано: закрытие у закрытой, «сейчас»
   *  у открытой. Без него числа нельзя ни перепроверить, ни сравнить. */
  at: string
  cards: IterationCard[]
  totals: {
    committed: number
    done: number
    lateAdded: number
    dropped: number
    /** Можно ли верить весу: одна неоценённая карточка состава делает
     *  сумму меньше, чем было. */
    byWeight: boolean
    committedWeight: number
    doneWeight: number
  }
}

/** Итерация доски. Вхождение карточки — интервал, а не поле, поэтому
 *  «что было в итерации тогда» читается отдельно от «что в ней сейчас». */
export type Iteration = {
  id: string
  name: string
  goal: string
  startsOn: string
  endsOn: string
  closedAt: string | null
  cardCount: number
}

/** Своё поле карточки. Определения принадлежат организации: одинаково
 *  названное поле на двух досках — это одно поле, иначе сводный отчёт
 *  сложит разные сущности с общим названием. */
export type FieldKind = 'text' | 'number' | 'date' | 'select' | 'checkbox'

export const FIELD_KIND_NAMES: Record<FieldKind, string> = {
  text: 'Текст',
  number: 'Число',
  date: 'Дата',
  select: 'Выбор',
  checkbox: 'Да или нет',
}

export type CardField = {
  id: string
  name: string
  kind: FieldKind
  options: string[]
}

export type FieldValue = { fieldId: string; value: string | number | boolean }

export type Snapshot = {
  board: BoardInfo
  columns: Column[]
  cards: Card[]
  links: Link[]
  linked: LinkedCard[]
  iterations: Iteration[]
  /** cardId → iterationId для открытых вхождений. */
  cardIterations: Record<string, string>
  fields: CardField[]
  /** cardId → значения его полей. */
  fieldValues: Record<string, FieldValue[]>
  /** Кого можно назначить. Приезжает со снимком: иначе исполнитель
   *  на карточке остался бы идентификатором. */
  people: Person[]
  /** Словарь меток организации и то, что чем помечено. Раздельно:
   *  иначе название метки уезжало бы в снимок столько раз, на скольких
   *  карточках оно висит. */
  labels: Label[]
  cardLabels: Record<string, string[]>
  /** Кто над чем работает: cardId → люди в порядке назначения.
   *  Первый — тот, кого назначили первым: порядок несёт смысл. */
  cardAssignees: Record<string, string[]>
}

export type Person = { userId: string; name: string }

/** Сохранённый вид: строка запроса, в которой уже лежат фильтры
 *  и группировка. Вид — это сохранённая ссылка, второго представления
 *  ему не нужно. */
export type BoardView = { id: string; name: string; query: string }

/** Метка организации. Цвет — имя оттенка из закрытого набора, а не
 *  значение: сырой цвет в тёмной теме начал бы светиться. */
export type Label = { id: string; name: string; tone: LabelTone }
export type LabelTone = 'slate' | 'green' | 'blue' | 'violet' | 'rose' | 'amber' | 'teal' | 'brown'

export const TONE_NAMES: Record<LabelTone, string> = {
  slate: 'Серый',
  green: 'Зелёный',
  blue: 'Синий',
  violet: 'Фиолетовый',
  rose: 'Розовый',
  amber: 'Янтарный',
  teal: 'Бирюзовый',
  brown: 'Коричневый',
}

export type Patch = {
  /** Метки карточки целиком: «вот как теперь», а не «добавили такую-то».
   *  Такой патч можно применить дважды без вреда. */
  cardLabels?: Record<string, string[]>
  cardAssignees?: Record<string, string[]>
  cards?: Card[]
  columns?: Column[]
  removedCardIds?: string[]
}
export type OperationResult = { version: number; patch: Patch }

export type Placement =
  { place: 'start' } | { place: 'end' } | { place: 'after'; afterCardId: string }

/** Конфликт: операция опиралась на устаревшее представление доски. */
export type Conflict = {
  error: string
  columnId?: string
  currentOrder?: { id: string; position: string }[]
  version: number
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: any,
  ) {
    super(body?.error ?? `Ошибка ${status}`)
  }
  get isConflict() {
    return this.status === 409
  }
}

/** Сеть недоступна или сервер не ответил вовремя — повтор имеет смысл. */
export class NetworkError extends Error {
  constructor(message: string) {
    super(message)
  }
}

const TIMEOUT_MS = 10_000

/**
 * keepalive — «доведи запрос до конца, даже если страницы уже нет».
 *
 * Нужен ровно операциям над доской. Перемещение применяется мгновенно,
 * а уходит на сервер следом; человек, нажавший F5 сразу после переноса,
 * иначе теряет своё действие — браузер отменяет незавершённый запрос
 * вместе со страницей. Нашлось сквозным сценарием: перезагрузка сразу
 * после переноса возвращала карточку на прежнее место.
 *
 * Остальным запросам это не нужно: их ответ читают тут же и без него
 * ничего не происходит.
 */
async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  keepalive = false,
): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS)
  let response: Response
  try {
    response = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
      credentials: 'same-origin',
      keepalive,
    })
  } catch (e) {
    throw new NetworkError(
      controller.signal.aborted ? 'Сервер не ответил за 10 секунд' : 'Нет связи с сервером',
    )
  } finally {
    clearTimeout(timer)
  }

  const text = await response.text()
  const parsed = text ? JSON.parse(text) : null
  // Кто вошёл — знает сервер, и «уже никто» он говорит одним кодом
  // на любой запрос. Разбирать это каждому экрану порознь значит
  // оставить человека там, где отказывает всё: экран рисует прежние
  // данные, показывает «нужно войти» и никуда не ведёт.
  if (response.status === 401) sessionLost()
  if (!response.ok) throw new ApiError(response.status, parsed)
  return parsed as T
}

const sessionLostHandlers = new Set<() => void>()

/**
 * Подписка на «сеанса больше нет».
 *
 * Слушатель один — каркас приложения; список нужен, чтобы отписка
 * в тестах и при перемонтировании не отключала чужую подписку.
 */
export function onSessionLost(handler: () => void): () => void {
  sessionLostHandlers.add(handler)
  return () => {
    sessionLostHandlers.delete(handler)
  }
}

function sessionLost() {
  for (const handler of sessionLostHandlers) handler()
}

export type AuthMethods = {
  password: { enabled: boolean }
  oidc: { enabled: boolean; label?: string }
}

export const api = {
  me: () => request<Principal>('GET', '/api/me'),
  authMethods: () => request<AuthMethods>('GET', '/api/auth/methods'),
  login: (email: string, password: string) =>
    request<Principal>('POST', '/api/auth/login', { email, password }),
  register: (org: string, name: string, email: string, password: string) =>
    request<Principal>('POST', '/api/auth/register', { org, name, email, password }),
  logout: () => request<void>('POST', '/api/auth/logout'),
  /** Смена пароля обрывает все прочие сессии: пароль меняют, когда он мог
   *  утечь, а к этому времени он мог стать чужой сессией. */
  changePassword: (current: string, next: string) =>
    request<void>('PUT', '/api/me/password', { current, next }),
  /** «Выйти на всех устройствах»: этот браузер остаётся, остальные — нет. */
  signOutElsewhere: () => request<void>('DELETE', '/api/me/sessions'),

  listOrgs: () => request<{ orgs: Membership[]; activeOrgId: string }>('GET', '/api/orgs'),
  createOrg: (name: string) => request<Membership>('POST', '/api/orgs', { name }),
  switchOrg: (orgId: string) => request<Principal>('POST', '/api/session/org', { orgId }),
  /** В чём организация оценивает работу. Числа не пересчитываются:
   *  меняется подпись, а не значение. */
  setEstimateUnit: (unit: EstimateUnit) =>
    request<Principal>('PUT', '/api/org/estimate-unit', { unit }),

  team: () => request<{ members: Member[]; invites: Invite[] }>('GET', '/api/team'),
  invite: (email: string, role: Role) => request<Invite>('POST', '/api/invites', { email, role }),
  revokeInvite: (id: string) => request<void>('DELETE', `/api/invites/${id}`),
  setRole: (userId: string, role: Role) =>
    request<void>('PUT', `/api/members/${userId}/role`, { role }),
  removeMember: (userId: string) => request<void>('DELETE', `/api/members/${userId}`),
  /** Обезличить: личность остаётся, персональных данных в ней не остаётся.
   *  Удалить строку нельзя — на неё ссылаются подписи под работой. */
  eraseMember: (userId: string) =>
    request<void>('DELETE', `/api/members/${userId}/identity`),

  /** Токен приглашения едет в теле, а не в адресе: адреса попадают
   *  в логи прокси и в историю браузера, а этот токен даёт членство
   *  в организации. Чтение через POST — плата за это. */
  inviteInfo: (token: string) => request<InviteInfo>('POST', '/api/invites/lookup', { token }),
  acceptInvite: (token: string, account?: { name: string; password: string }) =>
    request<Principal>('POST', '/api/invites/accept', { token, ...account }),

  listTeams: () => request<{ teams: Team[] }>('GET', '/api/teams'),
  createTeam: (name: string, parentId: string | null) =>
    request<Team>('POST', '/api/teams', { name, parentId }),
  renameTeam: (id: string, name: string) => request<void>('PATCH', `/api/teams/${id}`, { name }),
  /** Перенос: родитель или явный корень — «оставить как есть» выражается
   *  тем, что запрос вообще не отправляется. */
  moveTeam: (id: string, parentId: string | null) =>
    request<void>('PATCH', `/api/teams/${id}`, parentId ? { parentId } : { root: true }),
  archiveTeam: (id: string) => request<void>('DELETE', `/api/teams/${id}`),
  archivedTeams: () => request<{ teams: ArchivedTeam[] }>('GET', '/api/teams/archived'),
  restoreTeam: (id: string) => request<void>('POST', `/api/teams/${id}/restore`),

  teamMembers: (id: string) =>
    request<{ members: TeamMember[] }>('GET', `/api/teams/${id}/members`),
  teamBoards: (id: string) => request<{ boards: TeamBoard[] }>('GET', `/api/teams/${id}/boards`),
  addTeamMember: (id: string, userId: string) =>
    request<void>('PUT', `/api/teams/${id}/members/${userId}`),
  removeTeamMember: (id: string, userId: string) =>
    request<void>('DELETE', `/api/teams/${id}/members/${userId}`),

  listClients: () => request<{ clients: ApiClient[] }>('GET', '/api/clients'),
  createClient: (name: string, scopes: string[], expiresAt: string) =>
    request<ApiClient>('POST', '/api/clients', { name, scopes, expiresAt }),
  revokeClient: (id: string) => request<void>('DELETE', `/api/clients/${id}`),

  listWebhooks: () =>
    request<{ webhooks: Webhook[]; events: string[] }>('GET', '/api/webhooks'),
  createWebhook: (name: string, url: string, events: string[]) =>
    request<Webhook>('POST', '/api/webhooks', { name, url, events }),
  deleteWebhook: (id: string) => request<void>('DELETE', `/api/webhooks/${id}`),
  deliveries: (id: string) =>
    request<{ deliveries: Delivery[] }>('GET', `/api/webhooks/${id}/deliveries`),
  retryDelivery: (id: string) => request<void>('POST', `/api/deliveries/${id}/retry`),

  listAdmins: () => request<{ admins: TeamAdmin[] }>('GET', '/api/team-admins'),
  grantAdmin: (userId: string, teamId: string) =>
    request<TeamAdmin>('POST', '/api/team-admins', { userId, teamId }),
  revokeAdmin: (id: string) => request<void>('DELETE', `/api/team-admins/${id}`),

  listObservers: () => request<{ observers: Observer[] }>('GET', '/api/observers'),
  grantObservation: (userId: string, teamId: string | null) =>
    request<Observer>('POST', '/api/observers', { userId, teamId }),
  revokeObservation: (id: string) => request<void>('DELETE', `/api/observers/${id}`),

  /** Сохранённые виды доски — «фильтры плюс группировка» одним нажатием. */
  listViews: (boardId: string) =>
    request<{ views: BoardView[] }>('GET', `/api/boards/${boardId}/views`),
  saveView: (boardId: string, name: string, query: string) =>
    request<BoardView>('POST', `/api/boards/${boardId}/views`, { name, query }),
  deleteView: (id: string) => request<void>('DELETE', `/api/views/${id}`),

  listLabels: () => request<{ labels: Label[]; tones: LabelTone[] }>('GET', '/api/labels'),
  createLabel: (name: string, tone: LabelTone) =>
    request<Label>('POST', '/api/labels', { name, tone }),
  archiveLabel: (id: string) => request<void>('DELETE', `/api/labels/${id}`),

  /** Догнать пропущенное патчами вместо целого снимка. */
  changes: (boardId: string, since: number) =>
    request<Catchup>('GET', `/api/boards/${boardId}/changes?since=${since}`),
  boardAccess: (boardId: string) => request<BoardAccess>('GET', `/api/boards/${boardId}/access`),
  setSLE: (boardId: string, days: number | null, probability: number) =>
    request<void>('PUT', `/api/boards/${boardId}/sle`, { days, probability }),
  /** Пустая строка снимает подразделение, null — не трогает его:
   *  «кому видно» и «чья доска» меняют порознь. */
  setBoardAccess: (boardId: string, visibility: Visibility, teamId: string | null) =>
    request<void>('PUT', `/api/boards/${boardId}/access`, { visibility, teamId }),
  addBoardMember: (boardId: string, userId: string) =>
    request<void>('PUT', `/api/boards/${boardId}/members/${userId}`),
  removeBoardMember: (boardId: string, userId: string) =>
    request<void>('DELETE', `/api/boards/${boardId}/members/${userId}`),

  /** Ленты листаются курсором: журнал растёт, и смещение по номеру
   *  страницы показывало бы одно и то же дважды. */
  /** Лента доски. mine оставляет только то, что относится к спрашивающему:
   *  его карточки и реплики, где его упомянули. */
  boardEvents: (boardId: string, cardId?: string, before?: number, mine?: boolean) =>
    request<Feed>(
      'GET',
      `/api/boards/${boardId}/events?` +
        new URLSearchParams({
          ...(cardId ? { cardId } : {}),
          ...(before ? { before: String(before) } : {}),
          ...(mine ? { mine: '1' } : {}),
        }).toString(),
    ),
  audit: (before?: number) =>
    request<AuditPage>('GET', '/api/audit' + (before ? `?before=${before}` : '')),

  listBoards: () => request<{ boards: BoardInfo[] }>('GET', '/api/boards'),
  comments: (boardId: string, cardId: string) =>
    request<{ comments: Comment[] }>('GET', `/api/boards/${boardId}/cards/${cardId}/comments`),
  addComment: (
    boardId: string,
    cardId: string,
    body: string,
    parentId: string | null,
    mentions: string[],
  ) =>
    request<Comment>('POST', `/api/boards/${boardId}/cards/${cardId}/comments`, {
      body,
      parentId,
      mentions,
    }),
  editComment: (commentId: string, body: string) =>
    request<void>('PATCH', `/api/comments/${commentId}`, { body }),
  deleteComment: (commentId: string) => request<void>('DELETE', `/api/comments/${commentId}`),
  /** Прежние версии текста: «изменено» без них бесполезно. */
  commentRevisions: (commentId: string) =>
    request<{ revisions: string[] }>('GET', `/api/comments/${commentId}/revisions`),

  metrics: (boardId: string, days = 90) =>
    request<FlowReport>('GET', `/api/boards/${boardId}/metrics?days=${days}`),

  listFields: () => request<{ fields: CardField[] }>('GET', '/api/fields'),
  createField: (name: string, kind: FieldKind, options: string[]) =>
    request<CardField>('POST', '/api/fields', { name, kind, options }),
  /** Убирает поле из обихода, не трогая значения карточек. */
  archiveField: (id: string) => request<void>('DELETE', `/api/fields/${id}`),

  createIteration: (
    boardId: string,
    body: { name: string; goal: string; startsOn: string; endsOn: string },
  ) => request<Iteration>('POST', `/api/boards/${boardId}/iterations`, body),
  /** Обратного действия нет намеренно: закрытие — утверждение о том, что
   *  было сделано, и переоткрытие превратило бы отчёты в движущуюся мишень. */
  /** Отчёт по итерации: состав на момент закрытия и что с ним стало. */
  iterationReport: (boardId: string, iterationId: string) =>
    request<IterationReport>('GET', `/api/boards/${boardId}/iterations/${iterationId}/report`),
  closeIteration: (boardId: string, iterationId: string) =>
    request<void>('POST', `/api/boards/${boardId}/iterations/${iterationId}/close`),

  archivedBoards: () => request<{ boards: BoardInfo[] }>('GET', '/api/boards/archived'),
  /** Архив карточек доски. Курсор — момент архивации: архив дописывается,
   *  и смещение по номеру страницы однажды покажет карточку дважды. */
  archivedCards: (boardId: string, before?: string, query = '') => {
    // Поиск идёт на сервере: в архиве бывают сотни карточек, а на руках
    // у клиента только показанная порция — искать по ней значило бы
    // отвечать «не найдено» о том, что просто не долистали.
    const params = new URLSearchParams()
    if (before) params.set('before', before)
    if (query.trim()) params.set('q', query.trim())
    const tail = params.toString()
    return request<{ cards: ArchivedCard[]; next: string | null }>(
      'GET',
      `/api/boards/${boardId}/archived-cards` + (tail ? `?${tail}` : ''),
    )
  },
  /** Вернуть карточку из архива. Обычная операция доски — здесь только
   *  ради архива, где своего состояния доски нет. */
  restoreCard: (boardId: string, cardId: string) =>
    request<OperationResult>('POST', `/api/boards/${boardId}/operations`, {
      operationId: crypto.randomUUID(),
      type: 'RESTORE_CARD',
      payload: { cardId },
    }),
  /** Убирает доску с глаз, не удаляя: карточки и журнал остаются, по ним
   *  считается поток. Поэтому у действия есть обратное. */
  archiveBoard: (boardId: string) => request<void>('DELETE', `/api/boards/${boardId}`),
  restoreBoard: (boardId: string) => request<void>('POST', `/api/boards/${boardId}/restore`),
  /** Удалить доску насовсем. Название передаётся не для удобства: сервер
   *  сверяет его сам, иначе подтверждение было бы обещанием клиента. */
  deleteBoard: (boardId: string, name: string) =>
    request<void>('DELETE', `/api/boards/${boardId}/permanently`, { name }),
  /** Пустой ключ означает «выведи из названия»: так решает сервер,
   *  и повторять это правило здесь нельзя — разъедется. */
  createBoard: (name: string, key = '') =>
    request<BoardInfo>('POST', '/api/boards', { name, key }),
  snapshot: (boardId: string) => request<Snapshot>('GET', `/api/boards/${boardId}`),

  operation: (boardId: string, operationId: string, type: string, payload: unknown) =>
    request<OperationResult>(
      'POST',
      `/api/boards/${boardId}/operations`,
      { operationId, type, payload },
      // Операция доезжает до сервера, даже если страницу закрыли следом:
      // тело крошечное, а потерянное действие человек воспринимает как
      // «программа съела мою карточку».
      true,
    ),
}
