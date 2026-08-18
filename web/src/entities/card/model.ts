// Чистая часть карточки: во что превращаются связи для показа.
//
// Связь приходит парой идентификаторов, и половина этих идентификаторов
// может указывать куда угодно: на соседнюю карточку той же доски, на доску
// другой команды или на доску, которой спрашивающий не видит. Разложить
// это по полкам можно без сети — значит, здесь и раскладываем.

import { plural } from '../../shared/lib/plural.ts'
import type {
  Card,
  EstimateUnit,
  Link,
  LinkKind,
  LinkedCard,
  Priority,
} from '../../shared/api/index.ts'
import type { BaseState } from '../board/model.ts'

/** Куда ведёт связь и что об этом известно. */
export type Related = {
  id: string
  kind: LinkKind
  title: string
  /** Где карточка лежит: своя доска, чужая, или неизвестно. */
  where: string
  /** Что с ней происходит у соседей: «В очереди», «В работе», «Готово»,
   *  «Работу не взяли». Пусто для карточек этой доски — их положение
   *  видно на самой доске, и повторять его строкой значит показывать
   *  одно и то же дважды. */
  stage: string | null
  /** Обещание доски исполнителя словами. Ответ на «когда будет», который
   *  не требует ни поля «срок», ни переписки: он уже посчитан, и он
   *  принадлежит той доске, где работа лежит. */
  promise: string | null
  done: boolean
  blocked: boolean
  /** Причина, по которой часть стоит. Известна у своих карточек;
   *  о чужой снимок знает только сам факт. */
  blockedReason: string | null
  /** Ложь означает, что карточка есть, но доска недоступна: связь
   *  показываем, содержимое — нет. Скрывать саму связь неправильно:
   *  прогресс родителя её всё равно учитывает. */
  reachable: boolean
  /** Своя карточка открывается на этой же доске. */
  onThisBoard: boolean
  /** Кто делает именно эту часть. Части одной карточки почти всегда
   *  лежат на разных людях, и до сих пор доска отвечала «работа
   *  разбита», молча о том, кого спрашивать. */
  assignees: string[]
  /** Сколько реплик в её обсуждении. У части обсуждение своё, и без
   *  числа о нём узнают, только зайдя внутрь. */
  replies: number
}

/** Общий пустой список исполнителей: новый массив на каждую отрисовку
 *  ломает мемоизацию строк подзадач. */
const NO_ONE: string[] = []

export type CardDetails = {
  card: Card
  /** Родитель, если карточка — чья-то подзадача. Родитель ровно один:
   *  подзадачи образуют дерево, а не граф. */
  parent: Related | null
  subtasks: Related[]
  /** Блокирующие и смежные связи, в обе стороны. */
  related: Related[]
}

/**
 * Кто чья подзадача: cardId → родитель.
 *
 * Считается один раз на доску, а не отдельно для каждой карточки:
 * на пятистах карточках обход всех связей на каждую карточку — это
 * пятьсот обходов ради строки в две трети сантиметра.
 *
 * Родитель может лежать на другой доске: тогда берётся имя из списка
 * связанных карточек. Показывать «часть неизвестно чего» нельзя —
 * такая подпись хуже её отсутствия.
 */
export function parentsOf(
  base: BaseState,
): Record<string, { id: string; title: string; onThisBoard: boolean }> {
  const parents: Record<string, { id: string; title: string; onThisBoard: boolean }> = {}
  for (const link of base.links) {
    if (link.kind !== 'subtask') continue
    const own = base.cards[link.fromCard]
    const foreign = base.linked[link.fromCard]
    const title = own?.title ?? foreign?.title
    if (!title) continue
    parents[link.toCard] = { id: link.fromCard, title, onThisBoard: Boolean(own) }
  }
  return parents
}

/** Общий пустой список: `?? []` создавал бы новый массив на каждую
 *  отрисовку и ломал бы memo у каждой карточки без подзадач, то есть
 *  почти у всех. */
export const NO_SUBTASKS: Related[] = []

/**
 * Чьи это подзадачи: cardId родителя → его подзадачи.
 *
 * Считается один раз на доску по той же причине, что и parentsOf:
 * cardDetails обходит все связи ради одной карточки, и звать его
 * из отрисовки каждой означало бы пятьсот обходов на доску.
 *
 * Порядок — тот же, что в панели карточки: по названию. Порядок
 * подзадач сам по себе смысла не несёт, а два разных порядка в двух
 * местах читаются как разные списки.
 */
export function childrenOf(base: BaseState): Record<string, Related[]> {
  const children: Record<string, Related[]> = {}
  for (const link of base.links) {
    if (link.kind !== 'subtask') continue
    // Родителя, которого нет на этой доске, пропускаем: раскрывать
    // подзадачи не у чего, а сама связь видна с карточки-подзадачи.
    if (!base.cards[link.fromCard]) continue
    ;(children[link.fromCard] ??= []).push(resolve(base, link.toCard, 'subtask'))
  }
  for (const list of Object.values(children)) {
    list.sort((a, b) => a.title.localeCompare(b.title, 'ru'))
  }
  return children
}

/**
 * Кто кого держит: обе стороны связи «блокирует».
 *
 * Связь была видна только в панели и только с одной стороны — с той,
 * где её завели. На доске это значит, что порядок работ приходится
 * держать в голове: почему карточка стоит, видно, лишь открыв её,
 * а сколько работы держит эта — не видно вообще.
 *
 * Считается одним обходом связей на доску, как `parentsOf`
 * и `childrenOf`: обход на каждую карточку — это пятьсот обходов
 * на доску в пятьсот карточек.
 */
export function dependenciesOf(base: BaseState): {
  /** cardId → кого она держит. */
  holds: Record<string, Related[]>
  /** cardId → кто держит её. */
  waitsFor: Record<string, Related[]>
} {
  const holds: Record<string, Related[]> = {}
  const waitsFor: Record<string, Related[]> = {}
  for (const link of base.links) {
    if (link.kind !== 'blocks') continue
    // Направление здесь несёт смысл: from держит to.
    if (base.cards[link.fromCard]) {
      ;(holds[link.fromCard] ??= []).push(resolve(base, link.toCard, 'blocks'))
    }
    if (base.cards[link.toCard]) {
      ;(waitsFor[link.toCard] ??= []).push(resolve(base, link.fromCard, 'blocks'))
    }
  }
  return { holds, waitsFor }
}

export function cardDetails(base: BaseState, cardId: string): CardDetails | null {
  const card = base.cards[cardId]
  if (!card) return null

  const details: CardDetails = { card, parent: null, subtasks: [], related: [] }
  for (const link of base.links) {
    if (link.fromCard === cardId) {
      const other = resolve(base, link.toCard, link.kind)
      if (link.kind === 'subtask') details.subtasks.push(other)
      else details.related.push(other)
    } else if (link.toCard === cardId) {
      const other = resolve(base, link.fromCard, link.kind)
      if (link.kind === 'subtask') details.parent = other
      else details.related.push(other)
    }
  }
  details.subtasks.sort((a, b) => a.title.localeCompare(b.title, 'ru'))
  return details
}

function resolve(base: BaseState, id: string, kind: LinkKind): Related {
  const own = base.cards[id]
  if (own) {
    return {
      id,
      kind,
      title: own.title,
      where: 'На этой доске',
      stage: null,
      promise: null,
      // Готовность у части работы двух видов: карточка прошла точку
      // финиша или её отметили руками. Для строки подзадачи это одно
      // и то же — «сделана», — и прогресс родителя считает их поровну.
      done: own.outcome === 'done' || own.doneAt !== null,
      blocked: Boolean(own.blocked),
      blockedReason: own.blocked?.reason ?? null,
      reachable: true,
      onThisBoard: true,
      // Исполнители и разговор — только у своих карточек: про чужую
      // доску снимок знает название, колонку и обещание, но не состав.
      assignees: base.cardAssignees[id] ?? NO_ONE,
      replies: own.comments,
    }
  }

  const foreign: LinkedCard | undefined = base.linked[id]
  if (foreign) {
    return {
      id,
      kind,
      title: foreign.title,
      where: foreign.teamName
        ? `Доска «${foreign.boardName}» · ${foreign.teamName}`
        : `Доска «${foreign.boardName}»`,
      stage: stageOf(foreign),
      // Обещание показывается, пока работа не сделана: обещание сроков
      // на завершённой работе — это ответ на вопрос, который больше
      // никто не задаёт.
      promise: foreign.archived || foreign.outcome || foreign.done ? null : promiseOf(foreign),
      done: foreign.outcome === 'done' || foreign.done,
      blocked: foreign.blocked,
      blockedReason: null,
      reachable: true,
      onThisBoard: false,
      assignees: NO_ONE,
      replies: 0,
    }
  }

  // Карточка есть — связь на неё существует, — но доска недоступна.
  return {
    id,
    kind,
    title: 'Карточка недоступна',
    where: 'В подразделении, которого вам не видно',
    stage: null,
    promise: null,
    done: false,
    blocked: false,
    blockedReason: null,
    reachable: false,
    onThisBoard: false,
    assignees: NO_ONE,
    replies: 0,
  }
}

/**
 * Что делает с работой команда, у которой она лежит.
 *
 * Сказано про людей, а не про место, и это не украшение. Место у соседей
 * уже названо их словом — колонкой, — и назвать его вторым, своим,
 * значило бы написать «В очереди · Очередь». А вопрос, ради которого
 * сюда смотрят, про место и не спрашивает: «третью неделю лежит»
 * и «делают со вчера» — это про то, взялись или нет.
 */
const STAGES: Record<LinkedCard['columnKind'], string> = {
  queue: 'Ещё не начали',
  in_progress: 'Уже делают',
  done: 'Сделали',
}

/**
 * Что происходит с чужой карточкой: взялись ли за неё и где она стоит.
 *
 * Архив первым: убранная карточка — это отказ взять работу, и он важнее
 * того, в какой колонке она при этом стояла.
 */
function stageOf(card: LinkedCard): string {
  if (card.archived) return 'Работу не взяли'
  return `${STAGES[card.columnKind]} · ${card.columnName}`
}

/**
 * Обещание доски исполнителя словами.
 *
 * Это ответ на «когда будет», не требующий поля «срок»: канбан обещает
 * не дату, а распределение, и обещание принадлежит той доске, где работа
 * лежит. Доска без обещания молчит — подставлять ей выдуманный срок
 * значит начинать с неправды.
 */
function promiseOf(card: LinkedCard): string | null {
  if (card.sleDays === null) return null
  return `обычно ${card.sleDays} ${plural(card.sleDays, ...UNITS.days)} с вероятностью ${card.sleProbability}%`
}

// Названия единиц оценки живут здесь, а не в клиенте API: модель берёт
// оттуда только типы, и они стираются при сборке.
const UNITS: Record<EstimateUnit, [string, string, string]> = {
  points: ['очко', 'очка', 'очков'],
  hours: ['час', 'часа', 'часов'],
  days: ['день', 'дня', 'дней'],
}

/**
 * Подпись прогресса.
 *
 * Считается двумя способами, и подпись обязана их различать. По штукам —
 * «3 из 5»; по весу — «12 из 20 очков». Разница не косметическая: три
 * мелкие правки из пяти задач не означают, что работа сделана на
 * шестьдесят процентов, и подпись не должна это скрывать.
 */
/**
 * Заблокированные части работы.
 *
 * Часть, которая стоит, останавливает и целое: разбили работу, одна
 * часть упёрлась — задача не идёт, и по доске это обязано быть видно
 * с того места, где на неё смотрят. Обратное правило записано раньше
 * и остаётся: заблокированный родитель не останавливает остальные
 * части, их как раз и продолжают.
 */
export function blockedParts(subtasks: Related[]): Related[] {
  return subtasks.filter((s) => s.blocked)
}

/** Строка тревоги для родителя, у которого стоят части. */
export function blockedPartsLabel(parts: Related[]): string | null {
  if (parts.length === 0) return null
  if (parts.length > 1) return `Части заблокированы: ${parts.length}`
  // Причина своей части известна и говорит больше названия: «ждём
  // доступ к стенду» — это ответ, а «часть заблокирована» — только
  // повод открыть карточку.
  const [one] = parts
  return one.blockedReason
    ? `Часть заблокирована: ${one.blockedReason}`
    : `Часть заблокирована: ${one.title}`
}

export function progressLabel(card: Card, unit?: EstimateUnit): string | null {
  if (!card.progress || card.progress.total === 0) return null
  const { done, total, byWeight } = card.progress
  const base = `${number(done)} из ${number(total)}`
  if (!byWeight || !unit) return base
  return `${base} ${plural(total, ...UNITS[unit])}`
}

/** Как уровень называется человеку. «Средний» не показывается
 *  на карточке: это умолчание, и подпись у каждой второй карточки —
 *  шум. */
export const PRIORITY_NAMES: Record<Priority, string> = {
  highest: 'Наивысший',
  high: 'Высокий',
  medium: 'Средний',
  low: 'Низкий',
}

/**
 * Как называется уровень, каким бы он ни пришёл.
 *
 * Незнакомое значение показываем как есть — тем же правилом, что
 * и незнакомый тип события в ленте: оно уже пришло, и молчать о нём
 * хуже, чем показать непонятно. Без этой оговорки разошедшиеся клиент
 * и сервер роняли карточку целиком, а с ней и весь экран.
 */
export function priorityLabel(priority: Priority): string {
  return PRIORITY_NAMES[priority] ?? String(priority)
}

/**
 * Тот же уровень, но для доски.
 *
 * Два регистра речи, а не переименование. На карточке уровень стоит
 * в плашке высотой в девятнадцать пикселей, в одном ряду с тревогой,
 * сроком и итерацией: место там меряется знаками, и «наивысший» —
 * девять знаков плюс отступы — уходит в перенос первым. В панели
 * и в таблице место есть, и там уровни сравнивают друг с другом —
 * значит нужны полные имена, у которых виден порядок.
 *
 * Слова выбраны так, чтобы читаться без шкалы: «горит» и «фоном»
 * говорят, что делать, а «наивысший» и «низкий» — только где это
 * стоит в списке. «Горит» совпадает с одноимённым отбором намеренно:
 * отбор показывает верх шкалы, и называться они обязаны одинаково.
 */
export const PRIORITY_SHORT: Record<Priority, string> = {
  highest: 'горит',
  high: 'важно',
  medium: 'обычный',
  low: 'фоном',
}

export function priorityShort(priority: Priority): string {
  return PRIORITY_SHORT[priority] ?? String(priority)
}

/** Порядок уровней сверху вниз: наивысший первым. Список, а не
 *  сортировка по имени: по алфавиту «Высокий» встал бы выше
 *  «Наивысшего». */
export const PRIORITIES: Priority[] = ['highest', 'high', 'medium', 'low']

/** Насколько уровень выше среднего — для сортировки и сравнения.
 *  Одним местом: второе такое же однажды разойдётся с первым. */
export function priorityRank(priority: Priority): number {
  const at = PRIORITIES.indexOf(priority)
  // Незнакомый уровень уходит в конец: сортировка не имеет права
  // упасть из-за значения, которого она не знает.
  return at === -1 ? PRIORITIES.length : at
}

/**
 * Дата словами и без отсчёта от сегодня: «21 авг».
 *
 * Нужна там, где отсчёт был бы враньём, — в журнале. Запись «поставлен
 * срок» рассказывает о том, что случилось в прошлом, и превращать её
 * в «просрочено» задним числом нельзя: тогда история начинает
 * переписываться сама по мере того, как идёт время.
 */
export function dateWords(iso: string, now: Date = new Date()): string {
  const at = new Date(`${iso}T00:00:00`)
  return at.toLocaleDateString('ru-RU', {
    day: 'numeric',
    month: 'short',
    // Год — только чужой: в «21 авг 2026 г.» год читают, ничего
    // из него не узнавая, триста раз подряд.
    ...(at.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' }),
  })
}

/**
 * Промежуток словами: «13—19 авг.», «28 авг. — 3 сент.».
 *
 * Месяц не повторяется, когда он один: «13 авг. — 19 авг.» человек
 * читает дважды, чтобы убедиться, что месяц тот же. В полосе итерации
 * стояло «2026-08-13—2026-08-19» — двадцать один знак машинной записи
 * в шапке каждой доски, и на узком экране именно они разрывали строку.
 */
export function rangeWords(from: string, to: string, now: Date = new Date()): string {
  const a = new Date(`${from}T00:00:00`)
  const b = new Date(`${to}T00:00:00`)
  const sameMonth = a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth()
  // Тире без пробелов, когда концы короткие, и с пробелами, когда
  // длинные: «13—19 авг.» читается как одно, «28 авг. — 3 сент.» —
  // как два конца, и слипшись они бы спорили с точками сокращений.
  return sameMonth
    ? `${a.getDate()}—${dateWords(to, now)}`
    : `${dateWords(from, now)} — ${dateWords(to, now)}`
}

/**
 * Срок отсчётом от сегодня: «через 3 дн.», «прошёл 2 дн. назад».
 *
 * Дата в подписи была, и её не стало. Вопрос к сроку на доске один —
 * «успеваем ли», — а число месяца на него не отвечает: «до 21 авг.»
 * человек всё равно переводит в «это через четыре дня», и переводит
 * каждый раз заново, потому что сегодня сдвигается. Отсчёт отвечает
 * сразу.
 *
 * Точная дата осталась там, где спрашивают именно её: в панели, рядом
 * с полем, и в журнале, где отсчёт был бы враньём (см. `dateWords`).
 *
 * Возвращается число дней, а не готовый признак «горит», потому что
 * порогов у срока два и они разные (см. ниже). Признак пришлось бы
 * либо считать по самому широкому порогу, либо разбирать обратно
 * из подписи — а разбор собственной подписи строкой ломается о первую
 * же правку формулировки.
 */
export function dueLabel(dueOn: string, now: Date = new Date()): { text: string; days: number } {
  const due = new Date(`${dueOn}T00:00:00`)
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const days = Math.round((due.getTime() - today.getTime()) / 86_400_000)

  // Ближние дни названы словами, а не числом: «завтра» читается
  // с одного взгляда, «через 1 дн.» приходится складывать.
  if (days === 0) return { text: 'сегодня', days }
  if (days === 1) return { text: 'завтра', days }
  if (days === -1) return { text: 'прошёл вчера', days }
  if (days < 0) return { text: `прошёл ${-days} дн. назад`, days }
  return { text: `через ${days} дн.`, days }
}

/**
 * Порогов у срока два, и это не придирка.
 *
 * «Срок подходит» — вопрос планирования: что придётся разгребать
 * на неделе, кому помочь, что перестать брать. Три дня здесь верны:
 * за меньшее не успевают договориться, за большее ещё не начинают
 * беспокоиться. Отбор в строке фильтров спрашивает именно это.
 *
 * «Срок горит» — вопрос сегодняшнего дня: из-за чего звонят прямо
 * сейчас. И только это имеет право занять единственную тревожную
 * пометку на карточке.
 *
 * Пороги были одним, и от этого доска потеряла главное: карточка
 * с датой через три дня забирала тревожный слот и вытесняла
 * «идёт дольше обещанного» — сигнал, ради которого доска и считает
 * обещание. Там, где даты стоят у трети карточек, старение переставало
 * быть видно вообще.
 */
export function dueIsHot(dueOn: string | null, now?: Date): boolean {
  return dueOn === null ? false : dueLabel(dueOn, now).days <= 3
}

/** Срок горит: сегодня, завтра или уже прошёл. */
export function dueIsBurning(dueOn: string | null, now?: Date): boolean {
  return dueOn === null ? false : dueLabel(dueOn, now).days <= 1
}

// Единицы называются коротко: подпись стоит вплотную к числу, и
// «очков» в каждой строке таблицы повторять незачем.
export const UNIT_SHORT: Record<EstimateUnit, string> = {
  points: 'очк.',
  hours: 'ч',
  days: 'дн.',
}

/**
 * Оценка словами.
 *
 * Пусто — значит не оценена, и это не ноль: неоценённая карточка молча
 * выпадает из веса, а нулевая обещает, что работы в ней нет.
 */
export function estimateLabel(value: number | null, unit: EstimateUnit): string | null {
  return value === null ? null : `${number(value)} ${UNIT_SHORT[unit]}`
}

/** «5 карточек» — счётом, а не числом рядом со словом: сообщение
 *  об отмене и подпись таблицы обязаны читаться вслух как речь. */
export function cardsLabel(n: number): string {
  return `${n} ${plural(n, 'карточка', 'карточки', 'карточек')}`
}

/** Единица оценки со склонением при числе: «3 часа», «20 очков».
 *  Правило склонения одно на весь интерфейс — иначе однажды будет
 *  написано «2 очков». */
export function unitLabel(n: number, unit: EstimateUnit): string {
  return plural(n, ...UNITS[unit])
}

/** Дробные оценки существуют, но «2.00» на карточке не нужно никому. */
function number(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(2)))
}

/** Доля выполненного от нуля до единицы — для полоски. */
export function progressRatio(card: Card): number {
  if (!card.progress || card.progress.total === 0) return 0
  return card.progress.done / card.progress.total
}

/**
 * Кого можно предложить в подзадачи: карточки этой доски, кроме самой
 * карточки, её текущих подзадач и её родителя.
 *
 * Второй родитель у подзадачи невозможен — база откажет, — и предлагать
 * такое значит обещать невыполнимое.
 */
export function candidatesForSubtask(base: BaseState, details: CardDetails): Card[] {
  const taken = new Set<string>([details.card.id])
  for (const s of details.subtasks) taken.add(s.id)
  if (details.parent) taken.add(details.parent.id)

  const hasParent = new Set(
    base.links.filter((l: Link) => l.kind === 'subtask').map((l) => l.toCard),
  )

  return Object.values(base.cards)
    .filter((c) => !taken.has(c.id) && !hasParent.has(c.id))
    .sort((a, b) => a.title.localeCompare(b.title, 'ru'))
}
