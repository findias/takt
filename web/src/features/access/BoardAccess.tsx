import { useCallback, useEffect, useState } from 'react'
import { VISIBILITY_NAMES, api } from '../../shared/api/index.ts'
import type { BoardAccess as Access, Member, Team, Visibility } from '../../shared/api/index.ts'

/**
 * Кому видна доска.
 *
 * Одно правило приходится объяснять прямо здесь, потому что оно
 * неочевидно: закрыть доску можно только вокруг себя. Иначе доска,
 * закрытая вокруг чужих людей, стала бы неисправимой — редактировать
 * невидимую доску не может никто.
 *
 * Раньше правило и выражалось отказом: перевод в «только вписанным»
 * отклонялся, пока человек не впишет себя, и порядок действий он узнавал
 * из отказа. Теперь закрытие вписывает закрывающего само, а панель
 * говорит об этом до, а не после.
 */
export function BoardAccess({
  boardId,
  people,
  teams,
  canEdit,
  onClose,
  onChanged,
}: {
  boardId: string
  people: Member[]
  teams: Team[]
  canEdit: boolean
  /** Есть там, где блок раскрывают внутри списка. В панели закрывать
   *  нечего: у панели своя кнопка, и вторая рядом только путает. */
  onClose?: () => void
  /** Видимость поменялась — тому, кто показывает её рядом, стоит знать. */
  onChanged?: () => void
}) {
  const [access, setAccess] = useState<Access | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .boardAccess(boardId)
      .then(setAccess)
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось прочитать доступ'))
  }, [boardId])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(() => {
      load()
      onChanged?.()
    }).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  if (!access) return <div className="access">{error ?? 'Загружаем…'}</div>


  const outside = people.filter((p) => !access.members.some((m) => m.userId === p.userId))

  return (
    <div className="access stack">
      {error && <p className="error">{error}</p>}

      <div className="row">
        <label className="muted small" htmlFor={`vis-${boardId}`}>
          Видна
        </label>
        <select
          id={`vis-${boardId}`}
          value={access.visibility}
          disabled={!canEdit}
          onChange={(e) => {
            const next = e.target.value as Visibility
            // Командной доске нужна команда, и спросить её надо до запроса:
            // база откажет, но объяснять это ошибкой незачем.
            const team = next === 'team' ? (access.teamId ?? teams[0]?.id ?? null) : null
            if (next === 'team' && !team) {
              setError('Сначала заведите подразделение на вкладке «Структура»')
              return
            }
            act(api.setBoardAccess(boardId, next, team))
          }}
        >
          {(Object.keys(VISIBILITY_NAMES) as Visibility[]).map((v) => (
            <option key={v} value={v}>
              {VISIBILITY_NAMES[v]}
            </option>
          ))}
        </select>

        {access.visibility === 'team' && (
          <select
            value={access.teamId ?? ''}
            disabled={!canEdit}
            aria-label="Подразделение"
            onChange={(e) => act(api.setBoardAccess(boardId, 'team', e.target.value))}
          >
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        )}

        {onClose && (
          <button className="link" onClick={onClose}>
            Закрыть
          </button>
        )}
      </div>

      {access.visibility === 'private' ? (
        <p className="muted small">
          Закрытая доска открывается поимённо. Её не видит ни наблюдатель, ни владелец
          организации, если он не вписан — на то она и закрытая.
        </p>
      ) : (
        canEdit && (
          <p className="muted small">
            «Только вписанным» оставит доску тем, кто в её составе, и закрывающий
            попадает в состав сам: доску, закрытую вокруг чужих людей, не смог бы
            вернуть никто. Вписывает в состав владелец организации.
          </p>
        )
      )}

      {access.members.length > 0 && (
        <ul className="member-list">
          {access.members.map((m) => (
            <li key={m.userId}>
              <div className="member-who">
                <span>{m.name}</span>
                <span className="muted small">{m.email}</span>
              </div>
              {canEdit && (
                <button className="link" onClick={() => act(api.removeBoardMember(boardId, m.userId))}>
                  Убрать
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canEdit && outside.length > 0 && (
        <select
          value=""
          aria-label="Вписать в доску"
          onChange={(e) => {
            if (e.target.value) act(api.addBoardMember(boardId, e.target.value))
          }}
        >
          <option value="">Вписать человека…</option>
          {outside.map((p) => (
            <option key={p.userId} value={p.userId}>
              {p.name}
            </option>
          ))}
        </select>
      )}
    </div>
  )
}
