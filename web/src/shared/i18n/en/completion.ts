// Раздел каталога, который грузится вместе со своим экраном (см. index.ts).
import type { completion as ru } from '../ru/completion.ts'

export const completion: typeof ru = {
    title: 'Complete',
    value: (n: number) => `${n}%`,
    manual: 'set by hand',
    auto: 'Count automatically',
    hint:
      'Until someone moves it, the percentage counts itself: from the parts, with their percentages, and 100 for a card marked done. A percentage set by hand overrides the count; “Count automatically” brings it back. It neither moves the card nor marks it done.',
    is: (value: string) => `Complete: ${value}`,
}
