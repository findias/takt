// Чистая часть меток: откуда метка и как их раскладывать по
// происхождению. Отдельно от экранов по той же причине, что и модель
// доски: проверяется без браузера и без сети.

import type { BoardLabel, Label, LabelPlace } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Класс чипа и точки метки. Метка человека из переноса — техническая
 * и рисуется контуром без заливки: она замещает исполнителя, которого
 * не нашли, и не должна спорить с настоящими метками за внимание.
 */
export function chipClass(label: Pick<Label, 'tone' | 'kind'>): string {
  return label.kind === 'person' ? 'chip chip--person' : `chip chip--${label.tone}`
}
export function dotClass(label: Pick<Label, 'tone' | 'kind'>): string {
  return label.kind === 'person' ? 'label-dot label-dot--person' : `label-dot label-dot--${label.tone}`
}

/**
 * Откуда метка — словами, для подписи рядом с чипом.
 *
 * Подразделение и доска названы по имени, и своя доска тоже: подпись
 * одна на всех экранах, включая «Команду», где досок много, и «эта
 * доска» там значила бы разное. Имя отвечает на вопрос «почему её нет
 * на соседней доске».
 */
export function labelOrigin(label: Label): string {
  switch (label.scope) {
    case 'team':
      return t.model.originTeam(label.scopeName ?? '')
    case 'board':
      return t.model.originBoard(label.scopeName ?? '')
    default:
      return t.model.originOrg
  }
}

/**
 * Полная подсказка к чипу или точке: название, откуда, и что убрана.
 * Точка на карточке названия не несёт вовсе — всё, что о ней можно
 * узнать, не открывая панели, приходит отсюда.
 */
export function labelTitle(label: Label): string {
  const parts = [`${label.name} — ${labelOrigin(label)}`]
  if (label.archived) parts.push(t.model.archived)
  return parts.join(', ')
}

export type LabelGroup<L extends Label> = { key: string; title: string; labels: L[] }

/**
 * Метки группами по происхождению: сперва организация, потом
 * подразделения, потом доски — от широкого к узкому. Порядок групп —
 * по первому появлению: сервер отдаёт метки уже в этом порядке,
 * и пересортировать здесь значило бы завести второе правило.
 */
export function groupByOrigin<L extends Label>(labels: L[]): LabelGroup<L>[] {
  const groups: LabelGroup<L>[] = []
  const byKey = new Map<string, LabelGroup<L>>()
  for (const label of labels) {
    const key = `${label.scope}:${label.scopeId ?? ''}`
    let group = byKey.get(key)
    if (!group) {
      const origin = labelOrigin(label)
      group = { key, title: origin[0].toUpperCase() + origin.slice(1), labels: [] }
      byKey.set(key, group)
      groups.push(group)
    }
    group.labels.push(label)
  }
  return groups
}

/** Пункт выбора метки: повесить или снять существующую, вернуть убранную
 *  из архива, завести новую в названном месте. */
export type PickerItem =
  | { kind: 'toggle'; key: string; label: BoardLabel; checked: boolean }
  | { kind: 'restore'; key: string; label: BoardLabel }
  | { kind: 'create'; key: string; name: string; place: LabelPlace }

/** Название так, как его сравнивает сервер: без краёв и без регистра. */
export function sameName(a: string, b: string): boolean {
  return a.trim().toLowerCase() === b.trim().toLowerCase()
}

/**
 * Что предложить по набранному.
 *
 * Порядок и есть защита от свалки: сперва существующие, заведение —
 * последним. Совпадение по имени без учёта регистра заведения
 * не предлагает вовсе: человек, набравший «срочно», получает «Срочно»,
 * а не отказ «уже есть» и не вторую метку. Совпадение с убранной —
 * «вернуть из архива», а не «завести»: иначе в истории окажутся два
 * идентификатора с одним именем.
 *
 * Мест для заведения бывает несколько — эта доска, её подразделение
 * и старшие, организация, — и каждое предлагается отдельным пунктом,
 * от узкого к широкому: метка, заведённая на бегу, чаще нужна здесь,
 * а где она появится, должно быть видно до нажатия, а не после.
 *
 * `hung` — метки карточки; у массового действия их нет, и там пункты
 * не переключают, а только вешают.
 */
export function pickerItems(
  query: string,
  labels: BoardLabel[],
  hung: string[] | null,
  places: LabelPlace[],
): PickerItem[] {
  const needle = query.trim().toLowerCase()
  const shown = labels.filter(
    (l) => (l.offered || (hung?.includes(l.id) ?? false)) && l.name.toLowerCase().includes(needle),
  )
  const items: PickerItem[] = shown.map((label) => ({
    kind: 'toggle',
    key: `toggle:${label.id}`,
    label,
    checked: hung?.includes(label.id) ?? false,
  }))
  if (!needle) return items
  if (shown.some((l) => sameName(l.name, needle))) return items

  const archived = labels.filter((l) => l.archived && l.applies && sameName(l.name, needle))
  if (archived.length > 0) {
    return [
      ...items,
      ...archived.map((label): PickerItem => ({ kind: 'restore', key: `restore:${label.id}`, label })),
    ]
  }

  const name = query.trim()
  const narrowFirst = [
    ...places.filter((p) => p.scope === 'board'),
    ...places.filter((p) => p.scope === 'team').reverse(),
    ...places.filter((p) => p.scope === 'org'),
  ]
  return [
    ...items,
    ...narrowFirst.map(
      (place): PickerItem => ({
        kind: 'create',
        key: `create:${place.scope}:${place.id ?? ''}`,
        name,
        place,
      }),
    ),
  ]
}

/** Где появится заводимая метка — словами, для пункта «Завести». */
export function placeWords(place: LabelPlace): string {
  switch (place.scope) {
    case 'board':
      return t.model.placeBoard
    case 'team':
      return t.model.placeTeam(place.name)
    default:
      return t.model.placeOrg
  }
}
