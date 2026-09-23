// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
// Вкладка «Задачи»: задачи человека со всех досок (решение владельца
// 22.09.2026).
import { plural } from '../../lib/plural.ts'

export const tasks = {
    whose: 'Чьи задачи',
    mine: 'Мои',
    withDone: 'Показать законченные',
    status: 'Статус',
    statusAny: 'Любой',
    statuses: { queue: 'Ещё не начаты', in_progress: 'В работе', done: 'Сделаны', blocked: 'Заблокированы' },
    due: 'Срок',
    dueAny: 'Любой',
    dues: { overdue: 'Прошёл', soon: 'В ближайшие 3 дня', none: 'Без срока' },
    label: 'Метка',
    labelAny: 'Любая',
    reset: 'Сбросить отбор',
    shownOf: (shown: number, total: number) => `${shown} из ${total}`,
    emptyFiltered: 'Под отбор ни одна задача не подходит.',
    count: (n: number) => `${n} ${plural(n, 'задача', 'задачи', 'задач')}`,
    truncated: (n: number) => `Показаны первые ${n}: откройте доски, чтобы увидеть остальные.`,
    caption: (who: string) => `Задачи: ${who}`,
    colNumber: 'Номер',
    colTask: 'Задача',
    colBoard: 'Доска',
    colColumn: 'Колонка',
    colDue: 'Срок',
    colPriority: 'Важность',
    colLabels: 'Метки',
    colAge: 'В работе',
    days: (n: number) => `${n} дн.`,
    blocked: 'заблокирована',
    done: 'закончена',
    empty: 'Задач нет на досках, которые вам видны.',
    loadFailed: 'Не удалось загрузить задачи',
    hint: 'Карточки, где человек исполнитель, со всех досок, которые видны вам. Закрытых досок, куда у вас нет доступа, здесь нет.',
}
