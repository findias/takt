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

import { AVATAR_SMALL, Avatar, AvatarMore } from '../../shared/ui/Avatar.tsx'
import { CommentIcon } from '../../shared/ui/icons.tsx'
import type { Related } from '../../entities/card/model.ts'

export function SubtaskRow({
  subtask,
  people,
  assignees,
  replies,
  onOpen,
  onMarkDone,
}: {
  subtask: Related
  /** userId → имя. */
  people: Record<string, string>
  /** Исполнители именно подзадачи, не родителя. */
  assignees: string[]
  replies: number
  onOpen: (id: string) => void
  /** Отметить часть сделанной. Отметка не двигает карточку по доске:
   *  пункты вида «согласовать с юристами» по колонкам не ездят. */
  onMarkDone: (id: string, done: boolean) => void
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
      {/* Квадрат нажимается у своих частей и только показывает
          состояние у чужих. Отметка не двигает карточку по доске —
          иначе пришлось бы решать за человека, в какую колонку она
          переезжает, а у соседей колонки и вовсе свои. Она говорит
          одно: эта часть уже сделана. Поток при этом считается
          по-прежнему точкой финиша.
          Роль checkbox, а не кнопка с галочкой: состояние обязано
          читаться вслух, а не выводиться из вида. Готовность видна
          и приглушённым зачёркнутым названием — одним цветом отличать
          нельзя. */}
      {openable ? (
        <button
          type="button"
          role="checkbox"
          aria-checked={subtask.done}
          className="subtask-check"
          title={subtask.done ? 'Снять отметку' : 'Отметить сделанной'}
          aria-label={`Сделана: ${subtask.title}`}
          onClick={() => onMarkDone(subtask.id, !subtask.done)}
        >
          <span className="subtask-box" aria-hidden="true" />
        </button>
      ) : (
        // Та же ширина, что у нажимаемого: иначе строки чужих частей
        // разъезжаются с остальными на пол-сантиметра, и список
        // читается как два списка.
        <span className="subtask-check" aria-hidden="true">
          <span className="subtask-box" />
        </span>
      )}

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
              <Avatar key={id} name={people[id] ?? 'Кто-то'} size={AVATAR_SMALL} />
            ))}
            {assignees.length > 2 && (
              <AvatarMore count={assignees.length - 2} size={AVATAR_SMALL} />
            )}
          </span>
        )}
      </span>
    </li>
  )
}
