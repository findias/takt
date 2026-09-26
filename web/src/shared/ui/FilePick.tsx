import { useState } from 'react'
import { t } from '../i18n/index.ts'

/**
 * Выбор файла, подписанный языком интерфейса.
 *
 * Родное поле выбора файла подписывает кнопку и «файл не выбран»
 * языком браузера, а не нашим: на английском экране у русского браузера
 * стояло «Выберите файл», и наоборот, — единственная чужая строка
 * на экране переноса. Переписать эту подпись стилем нельзя.
 *
 * Поле остаётся настоящим, только спрятанным для глаза: фокус с
 * клавиатуры, диктор и перетаскивание в тестах идут через него, а
 * видимые кнопка и имя файла — его отражение. Подпись компонент ставит
 * сам, обёрткой: так поле названо всегда, где бы его ни поставили,
 * а щелчок по подписи и по «кнопке» открывает выбор без обработчика.
 */
export function FilePick({
  label,
  accept,
  disabled,
  onChoose,
}: {
  label: string
  accept: string
  disabled?: boolean
  onChoose: (file: File | undefined) => void
}) {
  const [name, setName] = useState<string | null>(null)
  return (
    <label className="import-file">
      <span>{label}</span>
      <span className="file-pick">
        <input
          type="file"
          className="sr-only"
          accept={accept}
          disabled={disabled}
          onChange={(e) => {
            const file = e.target.files?.[0]
            setName(file?.name ?? null)
            onChoose(file)
          }}
        />
        {/* Диктору то и другое скажет само поле, поэтому здесь — только
            для глаза. */}
        <span className="file-pick-button" aria-hidden="true">
          {t.ui.chooseFile}
        </span>
        <span className="file-pick-name" aria-hidden="true">
          {name ?? t.ui.noFileChosen}
        </span>
      </span>
    </label>
  )
}
