import { locale, t } from '../../shared/i18n/index.ts'

/**
 * Пометка песочницы публичного демо в шапке.
 *
 * Не полоса во всю ширину: доска занимает ровно высоту окна, и лишняя
 * строка над ней выдавила бы низ колонок за край. Пометка стоит в шапке
 * рядом с названием — там, куда смотрят первым делом, — и говорит
 * главное: это демо и когда оно исчезнет. Посетитель, потерявший работу
 * без предупреждения, решил бы, что продукт теряет данные.
 */
export function SandboxNote({ expiresAt }: { expiresAt?: string }) {
  if (!expiresAt) return null
  const when = sandboxEnds(expiresAt)
  return (
    <span className="sandbox-note" title={t.demo.noteTitle}>
      {t.demo.note(when)}
    </span>
  )
}

/** Когда исчезнет — днём и временем по часам смотрящего: «22 сентября,
 *  21:40». Секунды и год человеку ни о чём не говорят. */
export function sandboxEnds(iso: string): string {
  return new Date(iso).toLocaleString(locale(), {
    day: 'numeric',
    month: 'long',
    hour: '2-digit',
    minute: '2-digit',
  })
}
