import { useCallback, useEffect, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { Attachment, Attachments } from '../../shared/api/index.ts'
import { timeText } from '../../entities/feed/model.ts'
import { ConfirmDialog } from '../../shared/ui/Dialog.tsx'
import { FilePick } from '../../shared/ui/FilePick.tsx'
import { ScreenError } from '../../shared/ui/Field'
import { t } from '../../shared/i18n/index.ts'

/**
 * Файлы карточки (ROADMAP 36.1): снимок экрана, документ, таблица.
 *
 * Небольшие — предел на файл и на карточку называет сервер, и экран
 * говорит о нём до выбора файла, а не отказом после. Файл уходит сразу
 * после выбора: отдельная кнопка «Загрузить» — второй шаг ради того же
 * решения. Скачивается вложением: ничто из приложенного не открывается
 * страницей на нашем адресе.
 */
export function Files({ boardId, cardId, canEdit }: { boardId: string; cardId: string; canEdit: boolean }) {
  const [data, setData] = useState<Attachments | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [toDelete, setToDelete] = useState<Attachment | null>(null)

  const load = useCallback(() => {
    api
      .attachments(boardId, cardId)
      .then(setData)
      .catch((e) => setError(e instanceof Error ? e.message : t.files.readFailed))
  }, [boardId, cardId])
  useEffect(load, [load])

  if (!data) return <p className="muted small">{t.files.loading}</p>

  const upload = (file: File | undefined) => {
    if (!file) return
    setError(null)
    // Предел проверяется и здесь: отказ до отправки мегабайтов — это
    // секунды, которых человек не ждёт зря. Сервер проверит ещё раз.
    if (file.size > data.maxBytes) {
      setError(t.files.tooBig(humanBytes(data.maxBytes)))
      return
    }
    setBusy(true)
    api
      .attach(boardId, cardId, file)
      .then(load)
      .catch((e) => setError(e instanceof Error ? e.message : t.common.notDone))
      .finally(() => setBusy(false))
  }

  return (
    <section className="stack">
      <h3 className="section-title">{t.files.title}</h3>
      {/* Удалённый файл не вернуть — значит, спрашивает. */}
      <ConfirmDialog
        open={toDelete !== null}
        title={t.files.deleteTitle}
        confirmLabel={t.files.delete}
        danger
        onCancel={() => setToDelete(null)}
        onConfirm={() => {
          const a = toDelete
          setToDelete(null)
          if (!a) return
          setError(null)
          api
            .deleteAttachment(a.id)
            .then(load)
            .catch((e) => setError(e instanceof Error ? e.message : t.common.notDone))
        }}
      >
        <p>{t.files.deleteBody(toDelete?.name ?? '')}</p>
      </ConfirmDialog>
      <ScreenError>{error}</ScreenError>

      {data.attachments.length === 0 && <p className="muted small">{t.files.none}</p>}
      {data.attachments.length > 0 && (
        <ul className="attachments">
          {data.attachments.map((a) => (
            <li key={a.id} className="attachment">
              {/* Ссылка с download: браузер сохраняет файл под его именем,
                  а не открывает; сервер и так отдаёт вложением. */}
              <a className="attachment-name" href={`/api/attachments/${a.id}`} download={a.name}>
                {a.name}
              </a>
              <span className="muted small attachment-meta">
                {humanBytes(a.size)} · {a.author ?? t.talk.noName} · {timeText(a.createdAt)}
              </span>
              {a.deletable && (
                <button
                  type="button"
                  className="link link--remove"
                  aria-label={t.files.deleteOf(a.name)}
                  onClick={() => setToDelete(a)}
                >
                  {t.files.delete}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canEdit && (
        <>
          <FilePick label={t.files.attach} accept={data.extensions.join(',')} disabled={busy} onChoose={upload} />
          <p className="muted small">{t.files.hint(humanBytes(data.maxBytes), humanBytes(data.cardBytes))}</p>
        </>
      )}
    </section>
  )
}

/** Размер словами: «5 МБ», «512 КБ». Как на сервере — чтобы отказ и
 *  подсказка называли одно и то же число одинаково. */
export function humanBytes(n: number): string {
  if (n >= 1 << 20) {
    const mb = n / (1 << 20)
    return t.files.mb(Number.isInteger(mb) ? String(mb) : mb.toFixed(1))
  }
  if (n >= 1 << 10) return t.files.kb(String(Math.round(n / 1024)))
  return t.files.b(String(n))
}
