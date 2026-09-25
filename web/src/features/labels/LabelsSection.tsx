import { useCallback, useEffect, useState } from 'react'
import { TONE_NAMES, api } from '../../shared/api/index.ts'
import type { LabelPlace, LabelTone, ManagedLabel } from '../../shared/api/index.ts'
import { groupByOrigin, chipClass } from '../../entities/label/model.ts'
import { ScreenError } from '../../shared/ui/Field.tsx'
import { useToast } from '../../shared/ui/Toast.tsx'
import { t } from '../../shared/i18n/index.ts'

/**
 * Метки: все, какие видно, по месту, где они действуют.
 *
 * Метка принадлежит организации, подразделению (и действует на досках
 * всех вложенных) или одной доске. Группы здесь — ответ на вопрос
 * «откуда эта метка и где её ещё увидят»; одним списком на всех этот
 * вопрос задать было не к чему.
 *
 * Убранная в архив метка не снимается с карточек: карточка, помеченная
 * полгода назад, объясняет этим своё время в очереди, и стирать это
 * задним числом значит делать историю неверной. Поэтому у архивации
 * есть обратное, и предлагается оно сразу — тостом, а не вопросом.
 */
export function LabelsSection() {
  const [labels, setLabels] = useState<ManagedLabel[]>([])
  const [places, setPlaces] = useState<LabelPlace[]>([])
  const [error, setError] = useState<string | null>(null)
  const notify = useToast()

  const load = useCallback(() => {
    api
      .listLabels()
      .then((r) => {
        setLabels(r.labels)
        setPlaces(r.places)
      })
      .catch((e) => setError(e instanceof Error ? e.message : t.labelsAdmin.labelsFailed))
  }, [])

  useEffect(load, [load])

  const act = (p: Promise<unknown>, done?: () => void) => {
    setError(null)
    p.then(() => {
      load()
      done?.()
    }).catch((e) => setError(e instanceof Error ? e.message : t.common.notDone))
  }

  const archive = (label: ManagedLabel) =>
    act(api.archiveLabel(label.id), () =>
      notify({
        text: t.labelsAdmin.labelArchived(label.name),
        tone: 'info',
        action: { label: t.labelsAdmin.restore, onAct: () => act(api.restoreLabel(label.id)) },
      }),
    )

  const live = labels.filter((l) => !l.archived)
  const archived = labels.filter((l) => l.archived)

  return (
    <section className="stack">
      <h2 className="section-title">{t.labelsAdmin.labels}</h2>
      <ScreenError>{error}</ScreenError>

      {live.length === 0 ? (
        <p className="muted small">{t.labelsAdmin.labelsEmpty}</p>
      ) : (
        groupByOrigin(live).map((group) => (
          <div className="stack stack--tight" key={group.key}>
            <h3 className="label-group-title">{group.title}</h3>
            <ul className="member-list">
              {group.labels.map((label) => (
                <li key={label.id}>
                  <span className="row row--tight">
                    <span className={chipClass(label)}>{label.name}</span>
                    {/* Оттенок меняется здесь же, у метки: заведённой
                        с карточки он доставался сам, и поменять его было
                        негде. Меняется сразу на всех карточках. */}
                    {label.canManage && (
                      <select
                        value={label.tone}
                        aria-label={t.labelsAdmin.toneOf(label.name)}
                        onChange={(e) => act(api.recolorLabel(label.id, e.target.value as LabelTone))}
                      >
                        {(Object.keys(TONE_NAMES) as LabelTone[]).map((tone) => (
                          <option key={tone} value={tone}>
                            {TONE_NAMES[tone]}
                          </option>
                        ))}
                      </select>
                    )}
                  </span>
                  {label.canManage && (
                    <button
                      className="link link--remove"
                      aria-label={t.labelsAdmin.archiveLabel(label.name)}
                      onClick={() => archive(label)}
                    >
                      {t.labelsAdmin.toArchive}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ))
      )}

      {archived.length > 0 && (
        <div className="stack stack--tight">
          <h3 className="label-group-title">{t.labelsAdmin.archivedTitle}</h3>
          <p className="muted small">{t.labelsAdmin.archivedExplain}</p>
          <ul className="member-list">
            {archived.map((label) => (
              <li key={label.id}>
                <span className="row row--tight">
                  <span className={chipClass(label)}>{label.name}</span>
                  <span className="muted small">{groupByOrigin([label])[0].title}</span>
                </span>
                {label.canManage && (
                  <button
                    className="link"
                    aria-label={t.labelsAdmin.restoreLabel(label.name)}
                    onClick={() => act(api.restoreLabel(label.id))}
                  >
                    {t.labelsAdmin.restoreFromArchive}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {places.length > 0 && <NewLabel places={places} onCreate={act} />}
    </section>
  )
}

/**
 * Завести метку. Где она будет действовать, спрашивается только тогда,
 * когда выбирать есть из чего: у кого есть лишь организация, тому
 * лишний список ничего не скажет.
 */
function NewLabel({
  places,
  onCreate,
}: {
  places: LabelPlace[]
  onCreate: (p: Promise<unknown>, done?: () => void) => void
}) {
  const [name, setName] = useState('')
  const [tone, setTone] = useState<LabelTone>('green')
  const [placeKey, setPlaceKey] = useState('')
  const key = (p: LabelPlace) => `${p.scope}:${p.id ?? ''}`
  const place = places.find((p) => key(p) === placeKey) ?? places[0]

  return (
    <form
      className="row"
      onSubmit={(e) => {
        e.preventDefault()
        const sent = name.trim()
        if (!sent) return
        // Поле чистится, только если в нём всё ещё отправленное:
        // ответ приходит позже, и за это время успевают набрать
        // следующую метку — стереть её значило бы съесть набранное.
        onCreate(api.createLabel(sent, tone, place), () =>
          setName((now) => (now.trim() === sent ? '' : now)),
        )
      }}
    >
      <input
        value={name}
        aria-label={t.labelsAdmin.labelName}
        placeholder={t.labelsAdmin.labelName}
        onChange={(e) => setName(e.target.value)}
      />
      <select
        value={tone}
        aria-label={t.labelsAdmin.labelTone}
        onChange={(e) => setTone(e.target.value as LabelTone)}
      >
        {(Object.keys(TONE_NAMES) as LabelTone[]).map((tone) => (
          <option key={tone} value={tone}>
            {TONE_NAMES[tone]}
          </option>
        ))}
      </select>
      {places.length > 1 && (
        <select
          value={key(place)}
          aria-label={t.labelsAdmin.labelPlace}
          onChange={(e) => setPlaceKey(e.target.value)}
        >
          <PlaceOptions places={places} scope="org" title={t.labelsAdmin.placeOrg} keyOf={key} />
          <PlaceOptions places={places} scope="team" title={t.labelsAdmin.placeTeam} keyOf={key} />
          <PlaceOptions places={places} scope="board" title={t.labelsAdmin.placeBoard} keyOf={key} />
        </select>
      )}
      <button type="submit" disabled={!name.trim()}>
        {t.labelsAdmin.createLabel}
      </button>
    </form>
  )
}

function PlaceOptions({
  places,
  scope,
  title,
  keyOf,
}: {
  places: LabelPlace[]
  scope: LabelPlace['scope']
  title: string
  keyOf: (p: LabelPlace) => string
}) {
  const own = places.filter((p) => p.scope === scope)
  if (own.length === 0) return null
  if (scope === 'org') {
    return <option value={keyOf(own[0])}>{t.labelsAdmin.wholeOrg}</option>
  }
  return (
    <optgroup label={title}>
      {own.map((p) => (
        <option key={keyOf(p)} value={keyOf(p)}>
          {p.name}
        </option>
      ))}
    </optgroup>
  )
}
