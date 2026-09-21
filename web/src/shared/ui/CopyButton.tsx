import { useToast } from './Toast.tsx'
import { t } from '../i18n/index.ts'

/**
 * Копирование в буфер обмена.
 *
 * Отдельный компонент, а не три одинаковых обработчика, потому что
 * молчали все три одинаково: `navigator.clipboard?.writeText(...)`
 * ничего не показывает ни при успехе, ни при отказе, а буфер обмена —
 * единственное место в интерфейсе, куда нельзя заглянуть и проверить.
 * Хуже всего это там, где значение показывается один раз: ссылка-
 * приглашение, ключ доступа, ключ подписи — второй раз их не выдадут.
 *
 * Отказ бывает по-настоящему: браузер может не дать доступ к буферу
 * без явного разрешения, а на странице без защищённого соединения
 * `navigator.clipboard` просто отсутствует. Тогда человеку говорят,
 * что делать руками, — поле рядом и выделяется по щелчку.
 */
export function CopyButton({
  value,
  what,
}: {
  value: string
  /** Что копируем, в винительном падеже: «ссылку-приглашение»,
   *  «ключ доступа». Все четыре фразы — подпись кнопки и три
   *  сообщения — построены под один падеж, иначе на каждое новое
   *  место пришлось бы заводить вторую форму слова. */
  what: string
}) {
  const notify = useToast()

  return (
    <button
      aria-label={t.ui.copyWhat(what)}
      onClick={() => {
        const done = navigator.clipboard?.writeText(value)
        if (!done) {
          notify({
            text: t.ui.copyRefused(what),
            tone: 'warning',
          })
          return
        }
        void done.then(
          () => notify({ text: t.ui.copied(what), tone: 'info' }),
          () =>
            notify({
              text: t.ui.copyFailed(what),
              tone: 'warning',
            }),
        )
      }}
    >
      {t.ui.copy}
    </button>
  )
}
