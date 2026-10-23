import { useRef, useState } from 'react'
import { Button } from '../../shared/ui/Button.tsx'
import { t } from '../../shared/i18n/index.ts'
import { useAutoGrow } from '../../shared/lib/useAutoGrow.ts'

/**
 * Заведение карточки.
 *
 * Форма не закрывается после Enter, а очищается и остаётся с фокусом:
 * работа приходит списком, и карточки заводят подряд. Закрытая после
 * каждой форма означала поиск кнопки на каждую вторую карточку — это
 * и было первым, обо что спотыкался проход по интерфейсу.
 *
 * Исполнитель выбирается здесь же (ROADMAP 37.4): заводящий обычно
 * знает, кто будет делать, и второй заход в карточку ради этого был
 * лишним шагом на каждую задачу. Выбор переживает Enter — карточки
 * заводят пачкой, и чаще всего на одного человека.
 */
export function NewCardForm({
  column,
  people,
  assignable,
  onCreate,
  onCancel,
}: {
  /** Имя колонки — кнопке: «Завести» отдельно от окружения ничего
   *  не говорит, а диктор читает кнопку именно отдельно. */
  column: string
  people: Record<string, string>
  /** Кого можно назначить: видит доску (как в панели карточки). */
  assignable: Record<string, string>
  onCreate: (title: string, assigneeIds: string[]) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState('')
  const [who, setWho] = useState<string[]>([])
  const field = useRef<HTMLTextAreaElement>(null)
  useAutoGrow(field, value)
  const free = Object.entries(assignable).filter(([id]) => !who.includes(id))

  const submit = () => {
    const title = value.trim()
    if (!title) return
    onCreate(title, who)
    setValue('')
    // Фокус возвращается руками: доска перерисовывается новой карточкой,
    // и без этого он уезжает на `body` — то есть обход с клавиатуры
    // начинается заново со всей страницы.
    field.current?.focus()
  }

  return (
    <form
      className="new-card"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <textarea
        ref={field}
        autoFocus
        value={value}
        aria-label={t.column.whatToDo}
        placeholder={t.column.whatToDo}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onCancel()
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            submit()
          }
        }}
      />
      {(who.length > 0 || free.length > 0) && (
        <div className="row row--tight new-card-who" role="group" aria-label={t.newCard.assignees}>
          {who.map((id) => {
            const name = people[id] ?? assignable[id] ?? t.common.someone
            return (
              <button
                key={id}
                type="button"
                className="chip chip--slate chip--removable"
                aria-label={t.newCard.unassign(name)}
                onClick={() => setWho(who.filter((x) => x !== id))}
              >
                {name} ×
              </button>
            )
          })}
          {free.length > 0 && (
            <select
              value=""
              aria-label={t.newCard.addAssignee}
              onChange={(e) => {
                if (e.target.value) setWho([...who, e.target.value])
                field.current?.focus()
              }}
            >
              <option value="">{t.newCard.addAssigneePick}</option>
              {free.map(([id, name]) => (
                <option key={id} value={id}>
                  {name}
                </option>
              ))}
            </select>
          )}
        </div>
      )}
      <div className="row">
        <button
          type="submit"
          aria-label={t.column.addCardTo(column)}
          disabled={!value.trim()}
        >
          {t.column.create}
        </button>
        {/* Отмена — тихая: цвет ссылки рядом с главным действием
            перетягивает взгляд на себя, и первым читается «Отмена». */}
        <Button kind="quiet" type="button" onClick={onCancel}>
          {t.common.cancel}
        </Button>
      </div>
    </form>
  )
}
