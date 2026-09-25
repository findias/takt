# EmptyState

Пустое место и отказ загрузки. `Takt.EmptyState`, `Takt.ErrorState` и `Takt.Skeleton` из `web/src/shared/ui/states.tsx`.

- Пустое место либо предлагает действие, либо объясняет, почему пусто.
- Отказ говорит, что делать дальше, а не сообщает о состоянии базы.
- Заглушка загрузки (`Skeleton`) пульсирует (`motion-pending`): без движения серые полосы читаются как пустой список.

Свойства: `EmptyState` — `title`, `children`, `action`; `ErrorState` — `what` (глагол с объектом: «загрузить доску»), `error`, `onRetry`; `Skeleton` — `lines`.
