import { useCallback, useEffect, useState } from 'react'
import { ROLE_NAMES, api } from '../shared/api/index.ts'
import { FIELD_KIND_NAMES, SCOPE_NAMES, TONE_NAMES } from '../shared/api/index.ts'
import type {
  ApiClient,
  AuditEntry,
  CardField,
  FieldKind,
  Invite,
  Label,
  LabelTone,
  Member,
  Principal,
  Role,
} from '../shared/api/index.ts'
import { actorText, auditText, timeText } from '../entities/feed/model.ts'
import { Skeleton } from '../shared/ui/states.tsx'
import { ConfirmDialog } from '../shared/ui/Dialog.tsx'
import { Webhooks } from '../features/webhooks/Webhooks.tsx'

export function Team({ principal }: { principal: Principal }) {
  const [members, setMembers] = useState<Member[] | null>(null)
  const [invites, setInvites] = useState<Invite[]>([])
  const [error, setError] = useState<string | null>(null)
  const [freshLink, setFreshLink] = useState<string | null>(null)
  const isOwner = principal.role === 'owner'

  const load = useCallback(() => {
    api
      .team()
      .then((r) => {
        setMembers(r.members)
        setInvites(r.invites)
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось загрузить команду'))
  }, [])

  useEffect(load, [load])

  // Кого спрашивают обезличить.
  const [toErase, setToErase] = useState<Member | null>(null)

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <div className="stack">
      {error && <p className="error">{error}</p>}

      {/* Требование об удалении персональных данных. Диалог называет
          и человека, и то, что останется: обещать полное стирание
          нечестно — подписи под работой остаются. */}
      <ConfirmDialog
        open={toErase !== null}
        title="Удалить данные человека?"
        confirmLabel="Удалить данные"
        danger
        onCancel={() => setToErase(null)}
        onConfirm={() => {
          const who = toErase
          setToErase(null)
          if (who) act(api.eraseMember(who.userId))
        }}
      >
        {/* Имя стоит в именительном падеже и подлежащим: склонять чужие
            имена подстановкой нельзя, а «У «Иван Петров»» — это ошибка,
            которую видно сразу. */}
        <p>
          «{toErase?.name}» перестанет быть участником организации. Имя, почта и способ входа
          будут стёрты, сессии оборвутся.
        </p>
        <p className="muted small">
          Работа, которую он делал, останется: карточки, комментарии и записи журнала продолжат
          на неё ссылаться — просто без имени. Стереть их значило бы стереть историю чужой работы.
        </p>
      </ConfirmDialog>

      <section className="stack">
        <h2 className="section-title">В организации</h2>
        {members === null && <Skeleton lines={2} />}
        <ul className="member-list">
          {members?.map((m) => (
            <li key={m.userId}>
              <div className="member-who">
                <span>{m.name}</span>
                {/* У ключа вместо почты — то, чем он является: адрес
                    у него есть только потому, что личность требует
                    уникальной почты, и показывать «…@clients.invalid»
                    значит показывать устройство вместо смысла. */}
                <span className="muted small">
                  {m.kind === 'service' ? 'ключ интеграции' : m.email}
                </span>
              </div>
              {/* Ключу не предлагают ни роли, ни исключения, ни удаления
                  данных: роль у него от заведения, данных нет, а убирают
                  его отзывом самого ключа — ниже, в «Ключах для
                  интеграций». Раньше всё это предлагалось, и «Удалить
                  данные» отвечало отказом. */}
              {m.kind === 'service' ? (
                <span className="role-chip">ключ</span>
              ) : isOwner && m.userId !== principal.id ? (
                <div className="row">
                  <select
                    value={m.role}
                    aria-label={`Роль: ${m.name}`}
                    onChange={(e) => act(api.setRole(m.userId, e.target.value as Role))}
                  >
                    {(Object.keys(ROLE_NAMES) as Role[]).map((r) => (
                      <option key={r} value={r}>
                        {ROLE_NAMES[r]}
                      </option>
                    ))}
                  </select>
                  <button
                    className="link"
                    onClick={() => act(api.removeMember(m.userId))}
                    aria-label={`Исключить ${m.name}`}
                  >
                    Исключить
                  </button>
                  {/* Исключение обратимо приглашением, обезличивание
                      не обратимо ничем — и потому спрашивает. */}
                  <button
                    className="link link--danger"
                    onClick={() => setToErase(m)}
                    aria-label={`Удалить данные: ${m.name}`}
                  >
                    Удалить данные
                  </button>
                </div>
              ) : (
                <span className="role-chip">{ROLE_NAMES[m.role]}</span>
              )}
            </li>
          ))}
        </ul>
      </section>

      {isOwner && (
        <section className="stack">
          <h2 className="section-title">Пригласить</h2>
          <InviteForm
            onCreated={(invite) => {
              setFreshLink(invite.link ?? null)
              load()
            }}
            onError={setError}
          />

          {freshLink && (
            <div className="note">
              <p className="small">
                Ссылка создана. Она показывается один раз — в базе хранится только
                её отпечаток. Перешлите её приглашённому.
              </p>
              <div className="row">
                <input readOnly value={freshLink} onFocus={(e) => e.target.select()} />
                <button onClick={() => void navigator.clipboard?.writeText(freshLink)}>
                  Скопировать
                </button>
              </div>
            </div>
          )}

          {invites.length > 0 && (
            <>
              <h2 className="section-title">Ждут ответа</h2>
              <ul className="member-list">
                {invites.map((i) => (
                  <li key={i.id}>
                    <div className="member-who">
                      <span>{i.email}</span>
                      <span className="muted small">
                        {ROLE_NAMES[i.role]} · до{' '}
                        {new Date(i.expiresAt).toLocaleDateString('ru-RU')}
                      </span>
                    </div>
                    <button className="link" onClick={() => act(api.revokeInvite(i.id))}>
                      Отозвать
                    </button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </section>
      )}

      <CardFields canEdit={principal.role !== 'viewer'} />

      <Labels canEdit={principal.role !== 'viewer'} />

      {isOwner && <Clients />}

      {/* Подписки рядом с ключами: и то и другое про то, что уходит
          наружу, и смотрят на них по одному поводу. */}
      {isOwner && <Webhooks />}

      {isOwner && <Export />}

      <AuditFeed />
    </div>
  )
}

/**
 * Ключи для интеграций.
 *
 * Токен показывается один раз: в базе лежит только его хеш, как
 * у приглашения. В списке остаётся начало ключа — по нему свой ключ
 * узнают, не раскрывая его.
 */
function Clients() {
  const [clients, setClients] = useState<ApiClient[]>([])
  const [fresh, setFresh] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<string[]>(['boards:read'])
  const [expires, setExpires] = useState('')

  const load = useCallback(() => {
    api
      .listClients()
      .then((r) => setClients(r.clients))
      .catch(() => setClients([]))
  }, [])

  useEffect(load, [load])

  return (
    <section className="stack">
      <h2 className="section-title">Ключи для интеграций</h2>
      {error && <p className="error">{error}</p>}
      <p className="muted small">
        Ключ принадлежит организации, а не человеку: интеграция живёт дольше того,
        кто её завёл. Действия ключа видны в журнале наравне с людскими.
      </p>

      {clients.length > 0 && (
        <ul className="member-list">
          {clients.map((c) => (
            <li key={c.id}>
              <div className="member-who">
                <span>{c.name}</span>
                <span className="muted small">
                  {c.prefix}… · {c.scopes.map((s) => SCOPE_NAMES[s] ?? s).join(', ')}
                  {c.lastUsedAt
                    ? ` · работал ${new Date(c.lastUsedAt).toLocaleDateString('ru-RU')}`
                    : ' · ещё не работал'}
                </span>
              </div>
              <button
                className="link"
                onClick={() => {
                  setError(null)
                  api
                    .revokeClient(c.id)
                    .then(load)
                    .catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
                }}
              >
                Отозвать
              </button>
            </li>
          ))}
        </ul>
      )}

      {fresh && (
        <div className="note">
          <p className="small">
            Ключ создан. Он показывается один раз — в базе хранится только его
            отпечаток. Скопируйте и положите туда, откуда его возьмёт интеграция.
          </p>
          <div className="row">
            <input readOnly value={fresh} onFocus={(e) => e.target.select()} />
            <button onClick={() => void navigator.clipboard?.writeText(fresh)}>Скопировать</button>
          </div>
        </div>
      )}

      <form
        className="stack"
        onSubmit={(e) => {
          e.preventDefault()
          if (!name.trim() || scopes.length === 0) return
          setError(null)
          api
            .createClient(name.trim(), scopes, expires)
            .then((c) => {
              setFresh(c.token ?? null)
              setName('')
              setExpires('')
              load()
            })
            .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось создать ключ'))
        }}
      >
        <div className="row row--tight">
          <input
            value={name}
            placeholder="Для чего ключ"
            onChange={(e) => setName(e.target.value)}
          />
          <label className="small">
            Действует до
          </label>
          <input
            type="date"
            value={expires}
            aria-label="Действует до"
            onChange={(e) => setExpires(e.target.value)}
          />
          <button type="submit" disabled={!name.trim() || scopes.length === 0}>
            Создать
          </button>
        </div>
        <div className="row row--tight">
          {Object.keys(SCOPE_NAMES).map((scope) => (
            <label key={scope} className="row row--tight">
              <input
                type="checkbox"
                checked={scopes.includes(scope)}
                onChange={(e) =>
                  setScopes((current) =>
                    e.target.checked
                      ? [...current, scope]
                      : current.filter((s) => s !== scope),
                  )
                }
              />
              <span className="small">{SCOPE_NAMES[scope]}</span>
            </label>
          ))}
        </div>
      </form>
    </section>
  )
}

/**
 * Свои поля карточек.
 *
 * Определения принадлежат организации, а не доске: одинаково названное
 * поле на двух досках — это одно поле, иначе сводный отчёт сложит разные
 * сущности с общим названием. Поэтому и живут они здесь, а не на доске.
 */
/**
 * Метки организации.
 *
 * Заводятся здесь, а не на доске, по той же причине, что и поля:
 * одинаково названная метка на двух досках — это одна метка, иначе
 * фильтр по организации собирать не из чего.
 *
 * Убранная метка не снимается с карточек: карточка, помеченная полгода
 * назад, объясняет этим своё время в очереди, и стирать это задним
 * числом значит делать историю неверной.
 */
function Labels({ canEdit }: { canEdit: boolean }) {
  const [labels, setLabels] = useState<Label[]>([])
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [tone, setTone] = useState<LabelTone>('green')

  const load = useCallback(() => {
    api
      .listLabels()
      .then((r) => setLabels(r.labels))
      .catch(() => setLabels([]))
  }, [])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <section className="stack">
      <h2 className="section-title">Метки</h2>
      {error && <p className="error">{error}</p>}
      {labels.length === 0 ? (
        <p className="muted small">
          Меток нет. Метка отвечает на вопрос «да или нет» — срочно, снаружи,
          ждём ответа — и занимает на карточке столько же места, сколько слово.
          Заводится на всю организацию: одинаково названная метка на двух
          досках это одна метка.
        </p>
      ) : (
        <ul className="member-list">
          {labels.map((label) => (
            <li key={label.id}>
              <span className={`chip chip--${label.tone}`}>{label.name}</span>
              {canEdit && (
                <button
                  className="link"
                  title="Метка останется на карточках, где уже висит"
                  onClick={() => act(api.archiveLabel(label.id))}
                >
                  Убрать из списка
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canEdit && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim()) return
            act(api.createLabel(name.trim(), tone).then(() => setName('')))
          }}
        >
          <input
            value={name}
            placeholder="Название метки"
            onChange={(e) => setName(e.target.value)}
          />
          <select
            value={tone}
            aria-label="Оттенок метки"
            onChange={(e) => setTone(e.target.value as LabelTone)}
          >
            {(Object.keys(TONE_NAMES) as LabelTone[]).map((t) => (
              <option key={t} value={t}>
                {TONE_NAMES[t]}
              </option>
            ))}
          </select>
          <button type="submit" disabled={!name.trim()}>
            Завести метку
          </button>
        </form>
      )}
    </section>
  )
}

function CardFields({ canEdit }: { canEdit: boolean }) {
  const [fields, setFields] = useState<CardField[]>([])
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<FieldKind>('text')
  const [options, setOptions] = useState('')

  const load = useCallback(() => {
    api
      .listFields()
      .then((r) => setFields(r.fields))
      .catch(() => setFields([]))
  }, [])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <section className="stack">
      <h2 className="section-title">Поля карточек</h2>
      {error && <p className="error">{error}</p>}
      {fields.length === 0 ? (
        <p className="muted small">
          Полей нет. Поле заводится на всю организацию: одинаково названное поле
          на двух досках — это одно поле, иначе сводный отчёт складывает разное.
        </p>
      ) : (
        <ul className="member-list">
          {fields.map((f) => (
            <li key={f.id}>
              <div className="member-who">
                <span>{f.name}</span>
                <span className="muted small">
                  {FIELD_KIND_NAMES[f.kind]}
                  {f.options.length > 0 ? ` · ${f.options.join(', ')}` : ''}
                </span>
              </div>
              {canEdit && (
                <button
                  className="link"
                  title="Значения карточек останутся: поле заводили ради них"
                  onClick={() => act(api.archiveField(f.id))}
                >
                  Убрать
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canEdit && (
        <form
          className="row row--tight"
          onSubmit={(e) => {
            e.preventDefault()
            if (!name.trim()) return
            act(
              api.createField(
                name.trim(),
                kind,
                kind === 'select' ? options.split(',') : [],
              ),
            )
            setName('')
            setOptions('')
          }}
        >
          <input
            value={name}
            placeholder="Название поля"
            onChange={(e) => setName(e.target.value)}
          />
          <select
            value={kind}
            aria-label="Вид поля"
            onChange={(e) => setKind(e.target.value as FieldKind)}
          >
            {(Object.keys(FIELD_KIND_NAMES) as FieldKind[]).map((k) => (
              <option key={k} value={k}>
                {FIELD_KIND_NAMES[k]}
              </option>
            ))}
          </select>
          {kind === 'select' && (
            <input
              value={options}
              placeholder="Варианты через запятую"
              onChange={(e) => setOptions(e.target.value)}
            />
          )}
          <button type="submit" disabled={!name.trim()}>
            Завести поле
          </button>
        </form>
      )}
    </section>
  )
}

/**
 * Выгрузка данных организации.
 *
 * Обычная ссылка, а не кнопка с запросом: браузер умеет качать поток
 * сам, а собрать ответ в память ради «красивой» кнопки значит уронить
 * выгрузку ровно у той организации, ради которой она и нужна.
 *
 * Про журнал сказано отдельным флажком и словами: он больше всего
 * остального вместе взятого, и человек должен решать это заранее,
 * а не после получаса ожидания.
 */
function Export() {
  const [withAudit, setWithAudit] = useState(false)

  return (
    <section className="stack">
      <h2 className="section-title">Выгрузка</h2>
      <p className="muted small">
        Всё, что накопила организация: команды, доски, карточки, обсуждения,
        итерации и настройки. Секреты — подписи вебхуков и ключи доступа —
        в файл не попадают: восстановить по ним нечего, а потерять файл
        значило бы потерять доступ.
      </p>
      <label className="row row--tight">
        <input
          type="checkbox"
          checked={withAudit}
          onChange={(e) => setWithAudit(e.target.checked)}
        />
        <span className="small">Добавить журнал действий — он обычно больше всего остального</span>
      </label>
      <p>
        <a className="button" href={`/api/export${withAudit ? '?audit=1' : ''}`} download>
          Скачать файл
        </a>
      </p>
    </section>
  )
}

/**
 * Журнал административных действий.
 *
 * Читают его владелец организации и наблюдатель всей организации; всем
 * остальным приходит пустая лента, и раздел просто не появляется. Отказ
 * здесь был бы хуже: он подтверждал бы, что журнал есть и в нём что-то
 * лежит.
 */
function AuditFeed() {
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [next, setNext] = useState<number | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback((before?: number) => {
    api
      .audit(before)
      .then((page) => {
        setEntries((current) => (before ? [...current, ...page.entries] : page.entries))
        setNext(page.next)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }, [])

  useEffect(() => load(), [load])

  if (!loaded || entries.length === 0) return null

  return (
    <section className="stack">
      <h2 className="section-title">Что происходило</h2>
      <p className="muted small">
        Журнал ведёт база, а не приложение: изменение, сделанное в обход интерфейса,
        попадает сюда наравне с остальными. Записи только дописываются.
      </p>
      <ul className="feed">
        {entries.map((e) => (
          <li key={e.id}>
            <span>{auditText(e)}</span>
            <span className="muted small">
              {actorText(e.actor)} · {timeText(e.at)}
            </span>
          </li>
        ))}
      </ul>
      {next !== null && (
        <button className="link" onClick={() => load(next)}>
          Показать раньше
        </button>
      )}
    </section>
  )
}

function InviteForm({
  onCreated,
  onError,
}: {
  onCreated: (invite: Invite) => void
  onError: (message: string) => void
}) {
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('member')
  const [busy, setBusy] = useState(false)

  return (
    <form
      className="row"
      onSubmit={(e) => {
        e.preventDefault()
        if (!email.trim()) return
        setBusy(true)
        api
          .invite(email.trim(), role)
          .then((invite) => {
            setEmail('')
            onCreated(invite)
          })
          .catch((e) => onError(e instanceof Error ? e.message : 'Не удалось пригласить'))
          .finally(() => setBusy(false))
      }}
    >
      <input
        type="email"
        value={email}
        placeholder="Почта коллеги"
        onChange={(e) => setEmail(e.target.value)}
        required
      />
      <select value={role} onChange={(e) => setRole(e.target.value as Role)} aria-label="Роль">
        {(Object.keys(ROLE_NAMES) as Role[]).map((r) => (
          <option key={r} value={r}>
            {ROLE_NAMES[r]}
          </option>
        ))}
      </select>
      <button type="submit" disabled={busy || !email.trim()}>
        Пригласить
      </button>
    </form>
  )
}
