# Field

Поле формы: подпись, само поле, пояснение и отказ. `Takt.Field` из `web/src/shared/ui/Field.tsx`; поле передаётся функцией-потомком и получает `id` и связи ARIA.

- Узел отказа есть всегда, даже пустой: раскладка не прыгает, `aria-describedby` не пересобирается.
- Отказ объясняет, что делать, а не только что не так.
- Граница поля — `rule-strong` (3:1), фокус — обводка `accent`.
- Правка поля стирает его отказ.

Свойства: `label`, `hint`, `error`, `hiddenLabel`, `onFix`, `children({id, aria-describedby, aria-invalid})`.
