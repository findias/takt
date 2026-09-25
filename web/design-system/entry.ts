// Точка входа живых компонентов дизайн-системы (build.mjs): то же, что
// в продукте, плюс подписи, загруженные заранее, — превью рисует сразу.
import { ALL_SECTIONS, loadLang, loadSections, lang } from '../src/shared/i18n/index.ts'
export { Button, IconButton } from '../src/shared/ui/Button.tsx'
export { Avatar, AvatarMore } from '../src/shared/ui/Avatar.tsx'
export { Field, FormError } from '../src/shared/ui/Field.tsx'
export { Tabs, TabPanel, useTabIds } from '../src/shared/ui/Tabs.tsx'
export { ToastHost, ToastHost as Toast, useToast } from '../src/shared/ui/Toast.tsx'
export { Menu } from '../src/shared/ui/Menu.tsx'
export { ConfirmDialog } from '../src/shared/ui/Dialog.tsx'
export { Hint } from '../src/shared/ui/Hint.tsx'
export { EditableText } from '../src/shared/ui/EditableText.tsx'
export { EstimateStepper } from '../src/shared/ui/EstimateStepper.tsx'
export { PickList } from '../src/shared/ui/PickList.tsx'
export { CopyButton } from '../src/shared/ui/CopyButton.tsx'
export { EmptyState, ErrorState, Skeleton } from '../src/shared/ui/states.tsx'
export * as icons from '../src/shared/ui/icons.tsx'

// Превью живёт во фрейме без своих cookie: запись в document.cookie там
// бросает исключение, и loadLang не доходил до подписей. Подменяем cookie
// пустышкой только в таком фрейме — в продукте ветка не срабатывает.
try {
  void document.cookie
} catch {
  Object.defineProperty(document, 'cookie', { configurable: true, get: () => '', set: () => {} })
}

async function use(next: 'ru' | 'en') {
  await loadLang(next)
  await loadSections(...ALL_SECTIONS)
}
/** Готовность подписей. Импорты встроены в файл, поэтому промис решается
 *  до следующего скрипта страницы; ждать его всё равно правильно. */
export const ready = use('ru')
export const setLang = use
export const currentLang = () => lang
