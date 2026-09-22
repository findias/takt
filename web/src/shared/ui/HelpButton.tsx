import { HelpIcon } from './icons.tsx'
import { currentHelpTopic, helpUrl } from '../lib/help.ts'
import { t } from '../i18n/index.ts'

/**
 * «Справка» в шапке: ссылка, а не кнопка, — её открывают и в новой
 * вкладке средней кнопкой мыши, и копируют адрес. Адрес считается
 * в момент нажатия или наведения: какой экран открыт, знает стопка
 * тем (`shared/lib/help.ts`), а не эта ссылка.
 */
export function HelpButton() {
  const refresh = (e: React.SyntheticEvent<HTMLAnchorElement>) => {
    e.currentTarget.href = helpUrl(currentHelpTopic())
  }
  return (
    <a
      className="btn btn--quiet help-link"
      href={helpUrl(currentHelpTopic())}
      target="_blank"
      rel="noopener"
      title={t.help.keys}
      onClick={refresh}
      onMouseEnter={refresh}
      onFocus={refresh}
      onAuxClick={refresh}
    >
      <HelpIcon />
      {t.help.button}
    </a>
  )
}
