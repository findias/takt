import { useCallback, useEffect, useState } from 'react'
import { WEBHOOK_EVENT_NAMES, api } from '../../shared/api/index.ts'
import type { Delivery, Webhook } from '../../shared/api/index.ts'
import { Skeleton } from '../../shared/ui/states.tsx'

/**
 * Подписки на события.
 *
 * Сервер умеет их с пятого этапа — подпись, повторы с удвоением, журнал
 * доставок с ручным повтором, — а интерфейса не было вовсе: подписку
 * заводили запросом к API, и то, что она перестала доставлять,
 * узнавали от соседней системы.
 *
 * Здесь показывается ровно то, из-за чего к подпискам возвращаются:
 * куда уходит, что уходит, доходит ли — и кнопка досдать. Ключ подписи
 * показывается один раз, как токен ключа и ссылка приглашения: в базе
 * он есть, но второй раз его показывать незачем — хранит его получатель.
 */
export function Webhooks() {
  const [hooks, setHooks] = useState<Webhook[] | null>(null)
  // Какие события бывают, знает сервер: свой список здесь уже был
  // и разошёлся с доставляемым вчетверо.
  const [known, setKnown] = useState<string[]>([])
  const [fresh, setFresh] = useState<Webhook | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [events, setEvents] = useState<string[]>(['card.created'])

  const load = useCallback(() => {
    api
      .listWebhooks()
      .then((r) => {
        setHooks(r.webhooks)
        setKnown(r.events)
      })
      .catch(() => setHooks([]))
  }, [])

  useEffect(load, [load])

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(load).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <section className="stack">
      <h2 className="section-title">Подписки на события</h2>
      {error && <p className="error">{error}</p>}
      <p className="muted small">
        Подписка уносит события наружу: мы отправляем их на ваш адрес и подписываем ключом,
        чтобы получатель мог убедиться, что письмо от нас. Доставка идёт не менее одного раза
        — при отказе получателя мы повторяем, удваивая паузу.
      </p>

      {hooks === null ? (
        <Skeleton lines={2} />
      ) : (
        hooks.length > 0 && (
          <ul className="hook-list">
            {hooks.map((h) => (
              <HookRow key={h.id} hook={h} onAct={act} />
            ))}
          </ul>
        )
      )}

      {fresh && (
        <div className="note">
          <p className="small">
            Подписка создана. Ключ подписи показывается один раз — положите его туда, где
            получатель проверяет заголовок подписи.
          </p>
          <div className="row">
            <input
              readOnly
              value={fresh.secret ?? ''}
              aria-label="Ключ подписи"
              onFocus={(e) => e.target.select()}
            />
            <button onClick={() => void navigator.clipboard?.writeText(fresh.secret ?? '')}>
              Скопировать
            </button>
          </div>
          {/* Ключ без правила проверки бесполезен, а правило до сих пор
              можно было узнать только чтением нашего кода. */}
          <p className="small muted">
            Проверяется так: <code>X-Signature</code> — это{' '}
            <code>sha256=</code> и HMAC-SHA256 от строки «
            <code>X-Timestamp</code>.тело запроса», ключом служит эта строка.
            Полностью — в{' '}
            <a href="/api/v1/openapi.json" target="_blank" rel="noreferrer">
              описании контракта
            </a>
            .
          </p>
        </div>
      )}

      <form
        className="stack"
        onSubmit={(e) => {
          e.preventDefault()
          if (!name.trim() || !url.trim() || events.length === 0) return
          setError(null)
          api
            .createWebhook(name.trim(), url.trim(), events)
            .then((h) => {
              setFresh(h)
              setName('')
              setUrl('')
              load()
            })
            .catch((e) =>
              setError(e instanceof Error ? e.message : 'Не удалось завести подписку'),
            )
        }}
      >
        <div className="row row--tight">
          <input
            value={name}
            placeholder="Для чего подписка"
            aria-label="Название подписки"
            onChange={(e) => setName(e.target.value)}
          />
          <input
            value={url}
            placeholder="https://…"
            aria-label="Адрес получателя"
            onChange={(e) => setUrl(e.target.value)}
          />
          <button type="submit" disabled={!name.trim() || !url.trim() || events.length === 0}>
            Завести
          </button>
        </div>
        {/* Подписка без событий ничего не доставляет — сервер такую
            не заводит, и предлагать её незачем. */}
        <div className="checkbox-grid">
          {known.map((event) => (
            <label key={event} className="row row--tight">
              <input
                type="checkbox"
                checked={events.includes(event)}
                onChange={(e) =>
                  setEvents((current) =>
                    e.target.checked ? [...current, event] : current.filter((x) => x !== event),
                  )
                }
              />
              {/* Незнакомое имя показываем как есть: событие доставляется,
                  и промолчать о нём хуже, чем назвать непонятно. */}
              <span className="small">{WEBHOOK_EVENT_NAMES[event] ?? event}</span>
            </label>
          ))}
        </div>
      </form>
    </section>
  )
}

function HookRow({ hook, onAct }: { hook: Webhook; onAct: (p: Promise<unknown>) => void }) {
  // Журнал доставок раскрывается по требованию: он длинный, а смотрят
  // в него тогда, когда что-то не доехало.
  const [open, setOpen] = useState(false)

  return (
    <li>
      <div className="row row--between">
        <div className="member-who">
          <span>{hook.name}</span>
          <span className="muted small">
            {hook.url} · {hook.events.map((e) => WEBHOOK_EVENT_NAMES[e] ?? e).join(', ')}
          </span>
          {/* Отключённая подписка молчит, и молчать об этом нельзя:
              соседняя система в этот момент считает, что у нас ничего
              не происходит. */}
          {hook.disabled && (
            <span className="mark mark--fail">
              Отключена: получатель не отвечал. {hook.lastError ?? ''}
            </span>
          )}
        </div>
        <div className="row row--tight">
          <button className="link" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
            {open ? 'Скрыть доставки' : 'Доставки'}
          </button>
          <button
            className="link link--danger"
            onClick={() => onAct(api.deleteWebhook(hook.id))}
            aria-label={`Удалить подписку «${hook.name}»`}
          >
            Удалить
          </button>
        </div>
      </div>

      {open && <Deliveries hookId={hook.id} />}
    </li>
  )
}

/**
 * Журнал доставок одной подписки.
 *
 * Своё состояние, а не общее с подписками: повтор меняет одну доставку,
 * и перечитывать из-за него весь список подписок незачем.
 */
function Deliveries({ hookId }: { hookId: string }) {
  const [list, setList] = useState<Delivery[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .deliveries(hookId)
      .then((r) => setList(r.deliveries))
      .catch(() => setList([]))
  }, [hookId])

  useEffect(load, [load])

  if (list === null) return <Skeleton lines={2} />
  if (list.length === 0) {
    return <p className="muted small">Доставок ещё не было: событий, на которые она подписана, не случалось.</p>
  }

  return (
    <>
      {error && <p className="error">{error}</p>}
      <ul className="feed">
        {list.map((d) => (
          <li key={d.id}>
            <div className="row row--between">
              <div className="member-who delivery-line">
                <span className="small">
                  {WEBHOOK_EVENT_NAMES[d.event] ?? d.event} · {state(d)}
                </span>
                <span className="muted small">
                  {new Date(d.createdAt).toLocaleString('ru-RU')}
                  {d.attempts > 0 && ` · попыток: ${d.attempts}`}
                  {d.lastStatus !== null && ` · ответ ${d.lastStatus}`}
                  {d.lastError && ` · ${d.lastError}`}
                </span>
              </div>
              {/* Досдать можно то, что ещё не доставлено: повторять
                  доставленное значило бы прислать получателю событие
                  дважды по своей воле. */}
              {!d.delivered && (
                <button
                  className="link"
                  onClick={() => {
                    setError(null)
                    api
                      .retryDelivery(d.id)
                      .then(load)
                      .catch((e) =>
                        setError(e instanceof Error ? e.message : 'Не удалось повторить'),
                      )
                  }}
                >
                  Повторить
                </button>
              )}
            </div>
          </li>
        ))}
      </ul>
    </>
  )
}

/** Три состояния доставки, и они разные по смыслу: доставлено, сдались,
 *  ещё пробуем. Последнее называет время следующей попытки — иначе
 *  «не доставлено» читается как «не доставится». */
function state(d: Delivery): string {
  if (d.delivered) return 'доставлено'
  if (d.failed) return 'не доставлено, попытки исчерпаны'
  if (d.nextTry) return `следующая попытка ${new Date(d.nextTry).toLocaleTimeString('ru-RU')}`
  return 'в очереди'
}
