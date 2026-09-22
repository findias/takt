import { useEffect, useRef, useState } from 'react'
import { api } from '../../shared/api/index.ts'
import type { StandHalf, StandNote } from '../../shared/api/index.ts'
import { Button, IconButton } from '../../shared/ui/Button.tsx'
import { CloseIcon } from '../../shared/ui/icons.tsx'
import { lang, loadSections, locale, t } from '../../shared/i18n/index.ts'

const REPO = 'https://github.com/findias/takt'

/**
 * Текст коммита — для экрана. В сообщении строки перенесены руками
 * на 72-м знаке, и показанные как есть они дают рваный край, на узком
 * экране — через строку. Внутри абзаца строки склеиваются; пункты
 * списка («1. », «- ») остаются каждый на своей строке — там перенос
 * значим.
 */
export function reflow(text: string): string {
  return text
    .split(/\n\s*\n/)
    .map((para) => {
      const lines = para.split('\n').filter((l) => l.trim() !== '')
      // Абзац, набранный целиком с отступом, — команды и примеры:
      // склеенные в строку, они перестают читаться как команды.
      if (lines.length > 0 && lines.every((l) => /^(\s{2,}|\t)\S/.test(l))) {
        return lines.map((l) => l.trim()).join('\n')
      }
      return lines
        .map((line) => line.trim())
        .reduce((out, line) => {
          if (!out) return line
          return /^(\d+[.)]|[-*•])\s/.test(line) ? `${out}\n${line}` : `${out} ${line}`
        }, '')
    })
    .join('\n\n')
}

/**
 * Полоса тестового стенда ветки (ROADMAP 30.7): на каждом экране,
 * включая вход, — чтобы снимок со стенда не спутали с продуктом,
 * а пароль был под рукой там, где он нужен.
 *
 * Не стенд — ручки на сервере нет, ответ 404, и полосы нет вовсе:
 * на установке заказчика этот компонент ничего не рисует.
 */
export function StandBar() {
  const [note, setNote] = useState<StandNote | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    // Тексты стенда — отдельным разделом: установке заказчика, где
    // стенда нет, они в первой загрузке ни к чему (порог — perf.spec.ts).
    api
      .stand()
      .then((n) => loadSections('stand').then(() => setNote(n)))
      // Отказ — обычный ответ установки, которая не стенд: молчим.
      .catch(() => setNote(null))
  }, [])

  if (!note) return null
  return (
    <div className="stand-bar">
      <span>{t.stand.bar(note.branch || '—', note.version)}</span>
      <button
        type="button"
        className="link"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        {t.stand.open}
      </button>
      {open && <StandDialog note={note} onClose={() => setOpen(false)} />}
    </div>
  )
}

function StandDialog({ note, onClose }: { note: StandNote; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const dialog = ref.current
    if (dialog && !dialog.open) dialog.showModal()
  }, [])
  // Закрывается через `close()`, чтобы фокус вернул браузер; о закрытии
  // любым способом, в том числе Escape, узнаём по событию.
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    dialog.addEventListener('close', onClose)
    return () => dialog.removeEventListener('close', onClose)
  }, [onClose])
  const close = () => ref.current?.close()

  return (
    <dialog className="dialog stand-dialog" ref={ref} aria-labelledby="stand-title">
      <div className="account-head">
        <div>
          <h2 className="dialog-title" id="stand-title">
            {t.stand.title}
          </h2>
          <p className="muted small account-who">{t.stand.bar(note.branch || '—', note.version)}</p>
        </div>
        <IconButton label={t.common.close} onClick={close}>
          <CloseIcon />
        </IconButton>
      </div>
      <p className="small">{t.stand.signIn(note.email, note.password)}</p>
      {/* От чего считали — сказано: у ветки это «поверх master», а у
          выложенного master — «после выпуска», иначе заметка master
          была бы пустой при десятках сделанного. */}
      {note.commits.length === 0 ? (
        <p className="muted small">
          {note.since && note.since !== 'master' ? t.stand.emptySince(note.since) : t.stand.empty}
        </p>
      ) : (
        <>
          {note.since && <p className="muted small">{t.stand.since(note.since, note.commits.length)}</p>}
          <ol className="stand-commits">
            {note.commits.map((c) => (
              <StandCommit key={c.hash} commit={c} />
            ))}
          </ol>
        </>
      )}
      <div className="row dialog-actions">
        <Button kind="quiet" onClick={close}>
          {t.common.close}
        </Button>
      </div>
    </dialog>
  )
}

function StandCommit({ commit }: { commit: StandNote['commits'][number] }) {
  // Половина на языке интерфейса; русской нет — английская с пометкой,
  // а не пустое место: коммит без перевода всё равно на стенде.
  const fallback = lang === 'ru' && !commit.ru
  const half: StandHalf = lang === 'ru' && commit.ru ? commit.ru : commit.en
  const short = commit.hash.slice(0, 7)
  const when = new Date(commit.date)
  return (
    <li className="stand-commit">
      <h3 className="stand-commit-title">{half.title}</h3>
      <p className="muted small">
        <a
          className="stand-link"
          href={`${REPO}/commit/${commit.hash}`}
          aria-label={t.stand.commit(short)}
        >
          <code>{short}</code>
        </a>
        {!Number.isNaN(when.getTime()) &&
          ` · ${when.toLocaleString(locale(), { dateStyle: 'medium', timeStyle: 'short' })}`}
      </p>
      {fallback && <p className="muted small">{t.stand.noTranslation}</p>}
      {lang === 'en' && commit.onlyRu && <p className="muted small">{t.stand.onlyRussian}</p>}
      {half.body && <p className="stand-text">{reflow(half.body)}</p>}
      {half.check ? (
        <>
          <h4 className="stand-check-title">{t.stand.check}</h4>
          <p className="stand-text">{reflow(half.check)}</p>
        </>
      ) : (
        <p className="stand-missing small">{t.stand.noCheck}</p>
      )}
    </li>
  )
}
