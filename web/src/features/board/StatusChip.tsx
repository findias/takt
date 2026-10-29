import type { EpicStatusKind } from '../../shared/api/index.ts'
import { statusWord } from '../../entities/feed/model.ts'

/**
 * Слово статуса эпика цветом его тревожности (42.3, срез 10) — одно
 * и то же на карточке портфеля и в панели. Своим файлом, а не в
 * EpicStatus.tsx: карточка грузится с доской, форма статуса — с вкладкой
 * «Задачи», и тянуть форму в первый экран ради одного слова незачем.
 */
const TONES: Record<EpicStatusKind, string> = { on_track: 'green', at_risk: 'amber', off_track: 'rose' }

export function StatusChip({ status, title }: { status: EpicStatusKind; title?: string }) {
  return (
    <span className={`chip chip--${TONES[status]} epic-status-chip`} title={title}>
      {statusWord(status)}
    </span>
  )
}
