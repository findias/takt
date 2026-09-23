import type { Label, Task } from '../shared/api/index.ts'
import { dueLabel } from '../entities/card/model.ts'

/**
 * Отбор на вкладке «Задачи»: по статусу, сроку и метке.
 *
 * Статус — вид колонки, а не её имя: у задач разных досок колонки свои,
 * и «В работе» одной доски — «Делаем» другой. Вопрос, ради которого
 * фильтруют, общий для всех досок: взялись или нет, закончено ли, стоит ли.
 *
 * Отбирается уже загруженный список, а не запрос к серверу: задач
 * у человека сотни, не тысячи, и отбор должен отвечать сразу, без
 * скелетона на каждый щелчок.
 */
export type TaskStatus = '' | 'queue' | 'in_progress' | 'done' | 'blocked'
export type TaskDue = '' | 'overdue' | 'soon' | 'none'

export type TaskFilter = { status: TaskStatus; due: TaskDue; label: string }

export const STATUSES: Exclude<TaskStatus, ''>[] = ['queue', 'in_progress', 'done', 'blocked']
export const DUES: Exclude<TaskDue, ''>[] = ['overdue', 'soon', 'none']

export function parseTaskFilter(q: URLSearchParams): TaskFilter {
  const status = q.get('status') ?? ''
  const due = q.get('due') ?? ''
  return {
    status: (STATUSES as string[]).includes(status) ? (status as TaskStatus) : '',
    due: (DUES as string[]).includes(due) ? (due as TaskDue) : '',
    label: q.get('label') ?? '',
  }
}

export function isFiltered(f: TaskFilter): boolean {
  return f.status !== '' || f.due !== '' || f.label !== ''
}

export function filterTasks(tasks: Task[], f: TaskFilter, now: Date = new Date()): Task[] {
  return tasks.filter((task) => {
    if (f.status === 'blocked' ? !task.blocked : f.status !== '' && task.columnKind !== f.status) return false
    if (f.due === 'none' && task.dueOn !== null) return false
    if (f.due === 'overdue' || f.due === 'soon') {
      if (task.dueOn === null) return false
      const days = dueLabel(task.dueOn, now).days
      // «Скоро» — сегодня и три дня вперёд, тот же порог, что у отбора
      // на доске; прошедший срок сюда не входит: у него свой пункт.
      if (f.due === 'overdue' ? days >= 0 : days < 0 || days > 3) return false
    }
    if (f.label !== '' && !task.labels.some((l) => l.id === f.label)) return false
    return true
  })
}

/** Метки, которые есть у задач списка, — выбирать из других незачем:
 *  отбор по ним дал бы пустоту. По имени, без повторов. */
export function labelsOf(tasks: Task[]): Label[] {
  const byId = new Map<string, Label>()
  for (const task of tasks) for (const l of task.labels) byId.set(l.id, l)
  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name))
}
