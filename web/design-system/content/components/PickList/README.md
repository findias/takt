# PickList

Выбор нескольких значений из списка: `Takt.PickList` из `web/src/shared/ui/PickList.tsx`.

- Выбранное — метками со снятием крестиком; добавление — родным `<select>`, он доступен и на телефоне.
- Пусто — значит «любой» (`anyText`), и это сказано словом.
- Крестик у каждой метки назван для диктора (`removeLabel`).

Свойства: `label`, `anyText`, `addText`, `options[{id, name}]`, `chosen`, `removeLabel(name)`, `onChange`.
