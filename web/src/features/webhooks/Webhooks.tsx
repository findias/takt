import { useCallback, useEffect, useState } from 'react'
import { WEBHOOK_EVENT_NAMES, api } from '../../shared/api/index.ts'
import type { Delivery, Webhook, WebhookPolicy } from '../../shared/api/index.ts'
import { Skeleton } from '../../shared/ui/states.tsx'
import { CopyButton } from '../../shared/ui/CopyButton.tsx'
import { Field, useFormErrors } from '../../shared/ui/Field.tsx'
import { plural } from '../../shared/lib/plural.ts'
import { ScreenError } from '../../shared/ui/Field'

/**
 * Срок словами и с той точностью, какая тут есть.
 *
 * «Примерно 123 мин» — машинная точность там, где нужен порядок
 * величины: число складывается из восьми удвоений и меняется от любой
 * правки пауз, а человек читает его, чтобы решить, ждать ему или чинить
 * получателя. На этот же вопрос отвечает «около двух часов», и оно
 * не врёт точностью, которой нет.
 */
function примерно(минут: number): string {
  if (минут < 90) return `около ${Math.round(минут / 10) * 10} минут`
  const часов = Math.round(минут / 60)
  return `около ${часов} ${plural(часов, 'часа', 'часов', 'часов')}`
}

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
  // Границы автономии приходят с сервера — там, где эти числа
  // и действуют.
  const [policy, setPolicy] = useState<WebhookPolicy | null>(null)
  const [fresh, setFresh] = useState<Webhook | null>(null)
  const [error, setError] = useState<string | null>(null)
  const form = useFormErrors()
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [events, setEvents] = useState<string[]>(['card.created'])

  const load = useCallback(() => {
    api
      .listWebhooks()
      .then((r) => {
        setHooks(r.webhooks)
        setKnown(r.events)
        setPolicy(r.policy)
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
      <ScreenError>{error}</ScreenError>
      <p className="muted small">
        Подписка уносит события наружу: мы отправляем их на ваш адрес и подписываем ключом,
        чтобы получатель мог убедиться, что письмо от нас. Доставка идёт не менее одного раза.
      </p>
      {/* Что доставка делает сама — целиком и числами. Прежде здесь
          стояло «повторяем, удваивая паузу», и из этого нельзя было
          узнать ни сколько раз мы повторим, ни того, что после
          последней неудачи подписка отключается совсем: человек узнавал
          об этом от подписки, которая перестала слать. Автономная
          работа обязана говорить, где она остановится. */}
      {policy && (
        <p className="muted small">
          Что мы делаем сами: ждём ответа {policy.timeoutSeconds} с; если получатель
          не ответил — повторяем до {policy.attempts} раз, удваивая паузу
          от {policy.firstDelaySeconds} с до {Math.round(policy.maxDelaySeconds / 60)} мин.
          Всего это {примерно(policy.giveUpAfterMinutes)}. Дальше не пробуем: подписка
          отключается, и события по ней перестают копиться вовсе. Включит её обратно
          любой повтор доставки вручную. Журнал доставленного храним{' '}
          {policy.keepDeliveredDays} дней и потом убираем сами.
        </p>
      )}

      {hooks === null ? (
        <Skeleton lines={2} />
      ) : (
        hooks.length > 0 && (
          <ul className="hook-list">
            {hooks.map((h) => (
              <HookRow key={h.id} hook={h} onAct={act} onRefresh={load} />
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
            <CopyButton value={fresh.secret ?? ''} what="ключ подписи" />
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
        ref={form.ref}
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          const found = form.check(e.currentTarget)
          if (Object.keys(found).length > 0) {
            form.report(found)
            return
          }
          if (events.length === 0) return
          setError(null)
          api
            .createWebhook(name.trim(), url.trim(), events)
            .then((h) => {
              setFresh(h)
              setName('')
              setUrl('')
              load()
            })
            .catch((e) => {
              // Отказ здесь почти всегда про адрес: остальное проверено
              // до отправки. Он и встаёт под адресом — общая плашка
              // под рядом из трёх полей не говорит, какое переписывать.
              form.report({
                url: e instanceof Error ? e.message : 'Не удалось завести подписку',
              })
            })
        }}
      >
        <div className="form-row">
          <Field label="Название подписки" hiddenLabel {...form.field('name')}>
            {(bind) => (
              <input
                {...bind}
                name="name"
                value={name}
                placeholder="Для чего подписка"
                required
                onChange={(e) => setName(e.target.value)}
              />
            )}
          </Field>
          <Field label="Адрес получателя" hiddenLabel {...form.field('url')}>
            {(bind) => (
              <input
                {...bind}
                name="url"
                type="url"
                value={url}
                placeholder="https://…"
                required
                onChange={(e) => setUrl(e.target.value)}
              />
            )}
          </Field>
          {/* Кнопка гаснет только там, где нажатие бессмысленно:
              подписка без событий не доставляет ничего. Про пустые поля
              скажет отказ у поля — он объясняет, а погашенная кнопка
              молчит. */}
          <button type="submit" aria-label="Завести подписку" disabled={events.length === 0}>
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

function HookRow({
  hook,
  onAct,
  onRefresh,
}: {
  hook: Webhook
  onAct: (p: Promise<unknown>) => void
  onRefresh: () => void
}) {
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
            <>
              <span className="mark mark--fail">
                Отключена: получатель не отвечал. {hook.lastError ?? ''}
              </span>
              {/* Что теперь делать — отдельной строкой, а не в той же
                  отметке: причина отказа приходит от получателя, длины
                  и вида непредсказуемых, и совет, приписанный к ней
                  встык, читается её продолжением. */}
              <span className="muted small">
                Повтор любой доставки включит подписку снова — как и «Возобновить».
              </span>
            </>
          )}
          {/* Пауза — не отказ, и говорится о ней иначе: «мы сдались»
              и «остановлено вами» человек должен различать, не вчитываясь. */}
          {hook.paused && !hook.disabled && (
            <>
              <span className="mark">Приостановлена</span>
              <span className="muted small">
                Пока стоит пауза, события по этой подписке не копятся: возобновление
                не обернётся лавиной за всё время простоя.
              </span>
            </>
          )}
          {/* Работа, а не ожидание: сколько сейчас в очереди и чем
              кончилась последняя попытка. За журналом идут, когда уже
              не доходит, — а вопрос «доходит ли» задают каждый раз, и
              ответ на него не должен требовать ещё одного нажатия. */}
          {!hook.disabled && !hook.paused && (hook.pending > 0 || hook.lastTryAt) && (
            <span className="muted small">
              {hook.pending > 0 && `В очереди: ${hook.pending}. `}
              {hook.lastTryAt &&
                `Последняя попытка ${new Date(hook.lastTryAt).toLocaleString('ru-RU')}` +
                  (hook.lastStatus !== null ? `, ответ ${hook.lastStatus}.` : '.')}
            </span>
          )}
        </div>
        <div className="row row--tight">
          <button className="link" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
            {open ? 'Скрыть доставки' : 'Доставки'}
          </button>
          {/* Обратимое стоит раньше необратимого и не спрашивает,
              необратимое — спрашивает. До паузы вмешаться в идущую
              доставку можно было только удалением, а оно уносит ключ
              подписи (он показывается один раз) и весь журнал. */}
          <button
            className="link"
            onClick={() => onAct(api.pauseWebhook(hook.id, !hook.paused))}
            aria-label={
              hook.paused
                ? `Возобновить подписку «${hook.name}»`
                : `Приостановить подписку «${hook.name}»`
            }
          >
            {hook.paused ? 'Возобновить' : 'Приостановить'}
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

      {open && <Deliveries hookId={hook.id} onRetried={onRefresh} />}
    </li>
  )
}

/**
 * Журнал доставок одной подписки.
 *
 * Своё состояние, а не общее с подписками: повтор меняет одну доставку,
 * и перечитывать из-за него весь список подписок незачем.
 */
function Deliveries({ hookId, onRetried }: { hookId: string; onRetried: () => void }) {
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
      <ScreenError>{error}</ScreenError>
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
                      // Досдача включает отключённую подписку обратно,
                      // значит и список подписок больше не тот: иначе
                      // отметка «Отключена» осталась бы висеть враньём.
                      .then(() => {
                        load()
                        onRetried()
                      })
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
