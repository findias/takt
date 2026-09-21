// Чистая часть меток: откуда метка и как их раскладывать по
// происхождению. Отдельно от экранов по той же причине, что и модель
// доски: проверяется без браузера и без сети.

import type { Label } from '../../shared/api/index.ts'

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
      return `подразделение «${label.scopeName ?? ''}»`
    case 'board':
      return `доска «${label.scopeName ?? ''}»`
    default:
      return 'вся организация'
  }
}

/**
 * Полная подсказка к чипу или точке: название, откуда, и что убрана.
 * Точка на карточке названия не несёт вовсе — всё, что о ней можно
 * узнать, не открывая панели, приходит отсюда.
 */
export function labelTitle(label: Label): string {
  const parts = [`${label.name} — ${labelOrigin(label)}`]
  if (label.archived) parts.push('в архиве')
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
