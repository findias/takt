// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import { plural } from '../../lib/plural.ts'

export const flowReport = {
    cfdLabel: (days: number, queued: number, inProgress: number, done: number) =>
      `Накопительная диаграмма потока за ${days} ${plural(days, 'день', 'дня', 'дней')}. Сейчас: в очереди ${queued}, в работе ${inProgress}, сделано ${done}.`,
    cfdCaption:
      'Снизу вверх: сделано, в работе, в очереди. Полоса, растущая вверх без движения нижней границы, — это работа, которая копится, а не идёт.',
    cycleLabel: (points: number, min: string | number, max: string | number) =>
      `Время цикла по карточкам: ${points} ${plural(points, 'точка', 'точки', 'точек')}, от ${min} до ${max} ${plural(Number(max), 'дня', 'дней', 'дней')}.`,
    half: 'половина',
    p85: '85 из 100',
    cardDays: (title: string, days: string | number) => `${title}: ${days} дн.`,
    cycleCaption:
      'Каждая точка — доведённая карточка. Красные прошли дольше 85 из 100: по ним и стоит спрашивать, что случилось.',
    agingLabel: (n: number, oldest: string | number) =>
      `Возраст идущей работы: ${n} ${plural(n, 'карточка', 'карточки', 'карточек')}, самая старая ${oldest} ${plural(Number(oldest), 'день', 'дня', 'дней')}.`,
    promise: 'обещание',
    agingPoint: (title: string, days: string | number, column: string, blocked: boolean) =>
      `${title}: ${days} дн. в «${column}»${blocked ? ', заблокирована' : ''}`,
    agingCaption: 'Заблокированные красным: они стареют, ничего не делая.',
    reportOf: (name: string) => `Отчёт по итерации «${name}»`,
    reportFailed: 'Не удалось прочитать отчёт.',
    counting: 'Считаем…',
    closedOn: (when: string) => ` · закрыта ${when}, состав застыл`,
    running: ' · идёт, посчитано на сейчас',
    scope: 'Что было в составе',
    noCards: 'Ни одной карточки.',
    closedEmpty: 'Так она и закрылась.',
    stillEmpty: 'Пока пусто.',
    doneLabel: 'сделано',
    doneWeight: (unit: string) => `сделано ${unit}`,
    ofTotal: (done: string | number, total: string | number) => `${done} из ${total}`,
    carry: (count: number, name: string) => `Перенести незакрытые (${count}) в «${name}»`,
    carried: (count: number, name: string) =>
      `${count} ${plural(count, 'карточка перенесена', 'карточки перенесены', 'карточек перенесено')} в «${name}». Отчёт этой итерации не изменился.`,
    carryWhere: 'Куда перенести',
    lateAdded: 'пришло после начала',
    dropped: 'убрано по дороге',
    unestimated:
      'В составе есть неоценённые карточки — вес не считается: сумма без них показала бы меньше, чем было.',
    doneSr: 'Сделана. ',
    cardDropped: 'убрана из итерации',
    cardLate: 'пришла после начала',
    cardArchived: 'убрана с доски',
}
