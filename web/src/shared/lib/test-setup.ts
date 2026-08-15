// Общая подготовка тестов с DOM.
//
// Уборка после каждого теста нужна потому, что React-компоненты
// монтируются в общий документ: без неё второй тест видит разметку
// первого и падает на «нашлось несколько элементов» — то есть на своей же
// уборке, а не на проверяемом поведении.
//
// Обычно это делает сам @testing-library, но только когда включены
// глобальные describe/it. Они выключены намеренно: явный импорт видно,
// а магию — нет.

import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(cleanup)

// jsdom не знает про matchMedia: окна нет, размеров нет, отвечать
// нечем. Подменяем заглушкой, которая всегда говорит «не совпало» —
// то есть широкий экран. Компоненты, которым важна узкая раскладка,
// проверяются в настоящем браузере, где ширину можно задать;
// здесь важно лишь, чтобы вызов не падал.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      media: query,
      matches: false,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
}
