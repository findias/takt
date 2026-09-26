import { useId } from 'react'

/**
 * Знак Takt — метроном, сплошной (система знака — холст «Takt — логотип»
 * в Claude Design, вариант 05.2).
 *
 * Корпус держит форму, вырезанный маятник задаёт ритм. Вырез — не белая
 * заливка, а маска: сквозь маятник виден фон, на котором знак стоит, и
 * знак переназначает роли вместе с темой, как токены. Цвет корпуса —
 * `--accent`, поэтому в тёмной теме знак светлеет сам.
 *
 * Две версии по размеру. Полная — от 32 пикселей: с основанием, маятник
 * 3.5. Малая — до 32: без основания, маятник 4.5 и груз крупнее, иначе
 * вырез залипает в пиксельной сетке и знак читается пятном.
 *
 * `swing` — маятник качается ±15° с периодом 1.4 s (первая загрузка);
 * тем, кто просил меньше движения, знак проявляется, а не качается.
 */
export function Mark({ size, swing = false }: { size: number; swing?: boolean }) {
  const mask = useId()
  const small = size < 32
  return (
    <svg
      className={swing ? 'takt-mark takt-mark--swing' : 'takt-mark'}
      viewBox="0 0 48 48"
      width={size}
      height={size}
      aria-hidden="true"
      focusable="false"
    >
      <mask id={mask}>
        <rect width="48" height="48" fill="#fff" />
        {!small && (
          <path d="M14.5 36.5 H33.5" stroke="#000" strokeWidth="2.5" strokeLinecap="round" />
        )}
        <g className="takt-mark-pendulum">
          {small ? (
            <>
              <path d="M24 38 L30 14" stroke="#000" strokeWidth="4.5" strokeLinecap="round" />
              <circle cx="28" cy="22" r="5.5" fill="#000" />
            </>
          ) : (
            <>
              <path d="M24 36 L30 14" stroke="#000" strokeWidth="3.5" strokeLinecap="round" />
              <circle cx="27.8" cy="22" r="4.5" fill="#000" />
            </>
          )}
        </g>
      </mask>
      <path
        className="takt-mark-body"
        mask={`url(#${mask})`}
        d="M17 6 H31 L39 42 H9 Z"
        strokeWidth="4"
        strokeLinejoin="round"
      />
    </svg>
  )
}
