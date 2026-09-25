import { useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { Iteration } from '../../shared/api/index.ts'
import { rangeWords } from '../../entities/card/model.ts'
import { Button } from '../../shared/ui/Button.tsx'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { ScreenError } from '../../shared/ui/Field'
import { Hint } from '../../shared/ui/Hint.tsx'
import { t } from '../../shared/i18n/index.ts'

// Полоса итераций — своим куском сборки (этап 32.6): первая загрузка
// доски стоит у порога размера (web/e2e/perf.spec.ts), а у досок без
// итераций полосы нет вовсе.

/**
 * Итерации доски.
 *
 * Закрытие необратимо, поэтому спрашивается подтверждением: это
 * утверждение «вот что было сделано», а не отметка о прочтении.
 */
export function Iterations({
  boardId,
  canEdit,
  iterations,
  onChanged,
  onReport,
}: {
  boardId: string
  /** Наблюдателю итерации видны, но заводить и закрывать их он не может:
   *  показанная кнопка означала бы обещание, которое сервер отвергнет. */
  canEdit: boolean
  iterations: Iteration[]
  onChanged: () => void
  /** Открыть отчёт по итерации. */
  onReport: (iteration: Iteration) => void
}) {
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Какую итерацию спрашивают закрыть. Нативный confirm тут был
  // единственным на всё приложение: он выглядит чужим и, в отличие
  // от своего диалога, останавливает страницу целиком.
  const [toClose, setToClose] = useState<Iteration | null>(null)
  // Закрытие необратимо, и одного подтверждения мало: «Закрыть итерацию»
  // и «Закрыть итерацию» в диалоге стоят в одном месте, и два щелчка
  // подряд закрывали спринт (замечено владельцем 25.09.2026). Как
  // у удаления доски — название набирается руками.
  const [typed, setTyped] = useState('')
  const open = iterations.filter((i) => i.closedAt === null)
  // Закрытые не пропадают с экрана. Итерация закрывается ради ответа
  // «что было в спринте на момент закрытия» — а до сих пор в этот момент
  // она и исчезала, унося ответ вместе с собой.
  const closed = iterations.filter((i) => i.closedAt !== null)

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(onChanged).catch((e) => setError(e instanceof Error ? e.message : t.common.notDone))
  }

  return (
    <div className="iterations">
      <ScreenError>{error}</ScreenError>

      <ConfirmDialog
        open={toClose !== null}
        title={t.screen.closeIterationTitle}
        // Не просто «Закрыть»: рядом на экране живёт «Закрыть» панели,
        // и одно и то же слово означало бы то «уйти отсюда», то
        // «заморозить состав навсегда».
        confirmLabel={t.screen.closeIteration}
        danger
        confirmDisabled={typed.trim() !== toClose?.name}
        onCancel={() => setToClose(null)}
        onConfirm={() => {
          const it = toClose
          setToClose(null)
          if (it) act(api.closeIteration(boardId, it.id))
        }}
      >
        <p>{t.screen.closeIterationBody(toClose?.name ?? '')}</p>
        <p className="muted small">{t.screen.closeIterationType}</p>
        {/* В подсказке — слово, а не само название: заминка задумана,
            и ответ прямо в поле её отменил бы. */}
        {/* Поле — только в открытом диалоге: закрытый <dialog> остаётся
            в разметке, и второе поле «название» спорило бы с формой
            новой итерации рядом. */}
        {toClose && (
          <input
            value={typed}
            aria-label={t.screen.closeIterationConfirmName}
            placeholder={t.screen.closeIterationPlaceholder}
            onChange={(e) => setTyped(e.target.value)}
          />
        )}
      </ConfirmDialog>
      <div className="row row--tight">
        {/* «Итераций нет» — только когда их нет вовсе. Рядом со списком
            закрытых эта надпись противоречила бы сама себе. */}
        {iterations.length === 0 && !adding && <span className="muted small">{t.screen.noIterations}</span>}
        {open.map((i) => (
          <span key={i.id} className="mark" title={i.goal}>
            <button className="link" onClick={() => onReport(i)}>
              {i.name} · {rangeWords(i.startsOn, i.endsOn)} · {i.cardCount}
            </button>
            {/* Имя называет итерацию: кнопок «закрыть» в строке столько
                же, сколько итераций, и с диктора они звучали одинаково. */}
            {canEdit && (
              <button
                className="link"
                aria-label={t.screen.closeIterationOf(i.name)}
                onClick={() => {
                  setTyped('')
                  setToClose(i)
                }}
              >
                {/* Полностью: одинокое «закрыть» у плашки читалось как
                    «убрать плашку», а действие необратимое — состав
                    итерации застывает. На том же экране «Закрыть» есть
                    у каждой панели (разбор 21.09.2026). */}
                {t.screen.closeIteration}
              </button>
            )}
          </span>
        ))}
        {closed.length > 0 && (
          <>
            <span className="muted small">{t.screen.closed}</span>
            {closed.map((i) => (
              <button key={i.id} className="link" title={i.goal} onClick={() => onReport(i)}>
                {i.name}
              </button>
            ))}
          </>
        )}
        {canEdit && !adding && (
          <button className="link" onClick={() => setAdding(true)}>
            {t.screen.addIteration}
          </button>
        )}
      </div>

      {adding && (
        <form
          className="row row--tight"
          onSubmit={(e) => {
            e.preventDefault()
            const form = e.currentTarget
            const data = new FormData(form)
            const name = String(data.get('name') ?? '').trim()
            if (!name) return
            act(
              api.createIteration(boardId, {
                name,
                goal: String(data.get('goal') ?? ''),
                startsOn: String(data.get('startsOn') ?? ''),
                endsOn: String(data.get('endsOn') ?? ''),
              }),
            )
            setAdding(false)
          }}
        >
          <input
            name="name"
            autoFocus
            aria-label={t.screen.iterationName}
            placeholder={t.screen.name}
            required
          />
          <input name="startsOn" type="date" required aria-label={t.screen.starts} />
          <input name="endsOn" type="date" required aria-label={t.screen.ends} />
          <input name="goal" aria-label={t.screen.goalLabel} placeholder={t.screen.goal} />
          <button type="submit" aria-label={t.screen.createIteration}>
            {t.screen.create}
          </button>
          <Button kind="quiet" type="button" onClick={() => setAdding(false)}>
            {t.common.cancel}
          </Button>
          <Hint topic="iteration" />
        </form>
      )}
    </div>
  )
}
