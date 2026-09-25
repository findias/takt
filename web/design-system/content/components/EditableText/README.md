# EditableText

Текст, который правят на месте: `Takt.EditableText` из `web/src/shared/ui/EditableText.tsx`.

- Enter или уход фокуса сохраняет, Escape отменяет.
- Пустое значение не сохраняется.
- Используется для названий колонок и карточек; на карточке правку открывает меню «…» или клавиша E, а не двойной клик.

Свойства: `value`, `onSave`, `onCancel`, `label`, `placeholder`, `autoFocus`, `className`.
