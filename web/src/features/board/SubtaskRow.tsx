/**
 * Строка подзадачи в раскрытом списке на доске.
 *
 * Отвечает на три вопроса, которых прежний вариант не закрывал:
 * готова ли она, **чья она** и есть ли там разговор.
 *
 * Про «чья» — главное. Подзадачи одной карточки почти всегда лежат
 * на разных людях: сверстать, сделать ручку, проверить. Пока в строке
 * только название, доска сообщает «работа разбита» и молчит о том,
 * кого спрашивать, — а спрашивают именно об этом. Аватар в строке
 * стоит восемнадцать пикселей и снимает открытие карточки.
 *
 * Про разговор — то же рассуждение: у подзадачи своё обсуждение,
 * и без счётчика реплик о нём узнают, только зайдя внутрь. Счётчик
 * показывается только когда реплики есть: ноль рядом с каждой строкой
 * — шум.
 *
 * Недоступная подзадача остаётся видимой: связь существует и мера
 * разбиения её учитывает, а спрятать её значит соврать про меру. Но
 * называть её нельзя — вместо названия пунктирный квадрат и «нет
 * доступа».
 */

import { Avatar } from '../../shared/ui/Avatar.tsx'
import { CommentIcon } from '../../shared/ui/icons.tsx'
import type { Related } from '../../entities/card/model.ts'

export function SubtaskRow({
  subtask,
  people,
  assignees,
  replies,
  onOpen,
}: {
  subtask: Related
  /** userId → имя. */
  people: Record<string, string>
  /** Исполнители именно подзадачи, не родителя. */
  assignees: string[]
  replies: number
  onOpen: (id: string) => void
}) {
  // Назвать можно всё, что видно: часть на соседней доске — обычное
  // дело, и её название с доской это ответ на «кто это делает».
  // Не назвать нельзя только то, чья доска нам не видна вовсе.
  // Открыть отсюда можно лишь свою: чужая живёт на своей доске.
  const named = subtask.reachable
  const openable = subtask.onThisBoard

  return (
    <li
      className={[
        'subtask',
        subtask.done ? 'subtask--done' : '',
        named ? '' : 'subtask--hidden',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {/* Квадрат — знак состояния, а не флажок: «готово» на доске
          означает колонку финиша, и отметить её нажатием здесь значило
          бы решить за человека, в какую именно колонку переехать,
          — а у подзадачи на чужой доске колонки и вовсе свои.
          Готовность видна приглушённым зачёркнутым названием: одним
          цветом отличать нельзя. */}
      <span className="subtask-box" aria-hidden="true" />

      {openable ? (
        <button className="link" onClick={() => onOpen(subtask.id)}>
          {subtask.title}
        </button>
      ) : named ? (
        // Часть у соседей: называем её и говорим, чья она, но открываем
        // не отсюда — она живёт на их доске.
        <span className="subtask-name">
          {subtask.title}
          <span className="muted small"> · {subtask.where}</span>
        </span>
      ) : (
        // Название чужой карточки не наше дело, но её существование —
        // наше: мера разбиения её считает, и спрятать значит соврать
        // про меру.
        <span className="muted">Нет доступа · {subtask.where}</span>
      )}

      <span className="subtask-tail">
        {replies > 0 && (
          <span className="subtask-replies" title={`Реплик в обсуждении: ${replies}`}>
            <CommentIcon size={12} />
            {replies}
          </span>
        )}
        {assignees.length > 0 && (
          <span className="avatars">
            {assignees.slice(0, 2).map((id) => (
              <Avatar key={id} name={people[id] ?? 'Кто-то'} />
            ))}
            {assignees.length > 2 && (
              <span className="avatar--more" title={`и ещё ${assignees.length - 2}`}>
                +{assignees.length - 2}
              </span>
            )}
          </span>
        )}
      </span>
    </li>
  )
}
