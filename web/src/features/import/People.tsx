import { useState } from 'react'
import type { ImportReport, PersonChoice } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'

/**
 * Люди источника (ROADMAP 23.6): для каждого — сопоставить с участником,
 * завести или не переносить. По умолчанию стоит то, что сервер нашёл сам
 * (та же почта) или что выбрали при прошлом переносе этой доски.
 *
 * Список выбора, а не три переключателя на строку: «сопоставить» — это
 * ещё и «с кем», и участник выбирается тем же движением. Выбор уходит
 * на сервер сразу, и предпросмотр пересчитывается: человек видит,
 * получилось ли (адрес занят, почты нет), до кнопки «Перенести».
 */
export function People({
  report,
  choices,
  onChange,
  disabled,
}: {
  report: ImportReport
  choices: Record<string, PersonChoice>
  onChange: (next: Record<string, PersonChoice>) => void
  disabled: boolean
}) {
  const s = t.imports
  const people = report.people ?? []
  const members = report.members ?? []
  const set = (key: string, choice: PersonChoice) => onChange({ ...choices, [key]: choice })

  return (
    <div className="stack stack--tight">
      <h3 className="section-title">{s.people}</h3>
      <p className="muted small">{report.canCreatePeople ? s.peopleHint : s.peopleHintMember}</p>
      <ul className="import-people">
        {people.map((p) => {
          const chosen = choices[p.key]
          // В списке — выбор человека, а не итог сервера: «завести»
          // без почты сервер исполнить не может и отвечает «не переносить»
          // с причиной, но выбор от этого не меняется — причина под строкой.
          const shown = chosen ?? p
          const value =
            shown.action === 'match' ? `match:${shown.userId ?? p.userId}` : shown.action
          return (
            <li key={p.key}>
              <div className="import-person">
                <span>{p.name}</span>
                <span className="muted small">
                  {/* Таблица даёт почту без имени — тогда имя и есть почта,
                      и повторять её второй строкой незачем. */}
                  {p.email !== p.name && <>{p.email || s.noEmailShort} · </>}
                  {s.cardsOf(p.cards)}
                  {p.origin === 'auto' && ` · ${s.foundByEmail}`}
                  {p.origin === 'saved' && ` · ${s.savedChoice}`}
                </span>
              </div>
              <select
                aria-label={s.personChoice(p.name)}
                value={value}
                disabled={disabled}
                onChange={(e) => {
                  const v = e.target.value
                  if (v.startsWith('match:')) set(p.key, { action: 'match', userId: v.slice(6) })
                  else if (v === 'create') set(p.key, { action: 'create', email: chosen?.email })
                  else set(p.key, { action: 'skip' })
                }}
              >
                <optgroup label={s.matchWith}>
                  {members.map((m) => (
                    <option key={m.id} value={`match:${m.id}`}>
                      {m.name} · {m.email}
                    </option>
                  ))}
                </optgroup>
                {/* Кого завели этим же предпросмотром, в участниках ещё нет:
                    значение должно найтись в списке, иначе выбор пропадёт. */}
                {(report.canCreatePeople || shown.action === 'create') && (
                  <option value="create">{s.create}</option>
                )}
                <option value="skip">{s.skip}</option>
              </select>
              {(chosen?.action === 'create' || p.action === 'create') && !p.email && (
                <EmailDraft
                  name={p.name}
                  value={chosen?.email ?? ''}
                  disabled={disabled}
                  onCommit={(email) => set(p.key, { action: 'create', email })}
                />
              )}
              {p.problem && <p className="import-person-problem small">{p.problem}</p>}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/**
 * Почта для заводимого, у которого источник её не дал. Уходит на сервер
 * по уходу из поля или Enter, а не с каждой буквой: иначе предпросмотр
 * пересчитывался бы на каждый символ недописанного адреса.
 */
function EmailDraft({
  name,
  value,
  disabled,
  onCommit,
}: {
  name: string
  value: string
  disabled: boolean
  onCommit: (email: string) => void
}) {
  const [draft, setDraft] = useState(value)
  const commit = () => {
    if (draft.trim() !== value) onCommit(draft.trim())
  }
  return (
    <input
      type="email"
      className="import-person-email"
      aria-label={t.imports.createEmail(name)}
      placeholder={t.imports.createEmailPlaceholder}
      value={draft}
      disabled={disabled}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          commit()
        }
      }}
    />
  )
}

/** Кого завёл перенос — и ссылки, по которым они войдут. Показываются
 *  один раз: в базе лежат только отпечатки. */
export function CreatedPeople({ report }: { report: ImportReport }) {
  const s = t.imports
  const created = report.createdPeople ?? []
  if (created.length === 0) return null
  return (
    <div className="note stack stack--tight">
      <h4>{s.createdPeople}</h4>
      <p className="small">{created.some((m) => m.link) ? s.createdLinksHint : s.createdOidcHint}</p>
      <ul className="import-list">
        {created.map((m) => (
          <li key={m.id}>
            {m.name} · {m.email}
            {m.link && (
              <input
                readOnly
                className="import-person-link"
                value={m.link}
                aria-label={s.createdLinkOf(m.name)}
                onFocus={(e) => e.target.select()}
              />
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
