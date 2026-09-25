# Tabs

Полоса вкладок по образцу ARIA tablist. `Takt.Tabs`, `Takt.TabPanel` и `Takt.useTabIds` из `web/src/shared/ui/Tabs.tsx`.

- Tab останавливается на полосе один раз, внутри неё ходят стрелками.
- Текущая вкладка — `tab--active` и `aria-selected`.
- Содержимое — `TabPanel`: идентификаторы связи собираются в одном месте.

Свойства: `label`, `tabs[{id,label}]`, `active`, `onSelect`.
