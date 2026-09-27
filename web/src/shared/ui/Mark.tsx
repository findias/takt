import { useId } from 'react'

/**
 * Знак Takt — метроном, сложенный из карточек: корпус — доска, ряды
 * и колонки — карточки, маятник задаёт такт, орбита уносит готовую
 * карточку дальше по потоку.
 *
 * Щели между карточками, вырез под маятником и под орбитой — не белая
 * заливка, а маска: сквозь них виден фон, на котором знак стоит, поэтому
 * знак верен на обеих темах без второй раскраски. Цвета — свои, а не
 * `--accent`: знак — имя продукта, а не состояние интерфейса.
 *
 * Три ступени по размеру, потому что щель уже пикселя залипает и знак
 * читается пятном. От 64 — с чёрточками заголовков на карточках (README,
 * документация). От 32 — без чёрточек. До 32 — метроном из трёх рядов
 * с маятником: без колонок, орбиты и летящей карточки, у 16 пикселей они
 * сливаются в шум.
 *
 * `swing` — маятник качается ±33° с периодом 1.4 s (первая загрузка);
 * тем, кто просил меньше движения, знак проявляется, а не качается.
 */
export function Mark({ size, swing = false }: { size: number; swing?: boolean }) {
  const id = useId()
  const small = size < 32
  const titles = size >= 64
  const body = `url(#${id}b)`
  return (
    <svg
      className={swing ? 'takt-mark takt-mark--swing' : 'takt-mark'}
      viewBox="0 0 48 48"
      width={size}
      height={size}
      aria-hidden="true"
      focusable="false"
    >
      <defs>
        <linearGradient id={`${id}b`} x1="9" y1="0" x2="39" y2="0" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#5ade8e" />
          <stop offset=".48" stopColor="#16ad82" />
          <stop offset=".52" stopColor="#1a93cf" />
          <stop offset="1" stopColor="#1f68cc" />
        </linearGradient>
        <mask id={`${id}m`}>
          <rect width="48" height="48" fill="#fff" />
          {small ? (
            <>
              <path d="M0 17.3 H48 M0 30.7 H48" stroke="#000" strokeWidth="2.6" />
              <g className="takt-mark-pendulum" style={{ transformOrigin: '22px 40px' }}>
                <path d={SMALL_ROD} stroke="#000" strokeWidth="9" strokeLinecap="round" />
              </g>
            </>
          ) : (
            <>
              <path
                d="M0 14 H48 M0 24 H48 M0 34 H48 M22.6 0 L18.9 48 M25.4 0 L29.1 48"
                stroke="#000"
                strokeWidth="1.3"
              />
              {titles &&
                TITLES.map(([x, y, w]) => (
                  <rect key={`${x} ${y}`} x={x} y={y} width={w} height="0.9" rx="0.3" fill="#000" />
                ))}
              <path d={ORBIT} fill="none" stroke="#000" strokeWidth="4.4" strokeLinecap="round" />
              <g className="takt-mark-pendulum" style={{ transformOrigin: '23px 34px' }}>
                <path d={ROD} stroke="#000" strokeWidth="4.8" strokeLinecap="round" />
              </g>
            </>
          )}
        </mask>
        {!small && (
          <mask id={`${id}c`}>
            <rect x="-4" y="-5" width="8" height="10" fill="#fff" />
            {titles && <rect x="-1.5" y="-2.4" width="3" height="0.9" rx="0.3" fill="#000" />}
          </mask>
        )}
      </defs>
      <path className="takt-mark-body" mask={`url(#${id}m)`} d="M18.8 4 H29.2 L38.5 44 H9.5 Z" fill={body} />
      {small ? (
        <g className="takt-mark-pendulum" style={{ transformOrigin: '22px 40px' }}>
          <path d={SMALL_ROD} stroke={FLOW} strokeWidth="4" strokeLinecap="round" />
        </g>
      ) : (
        <>
          <path d={ORBIT_BACK} fill="none" stroke={FLOW} strokeWidth="1.5" strokeLinecap="round" />
          <path d={ORBIT} fill="none" stroke={FLOW} strokeWidth="1.8" strokeLinecap="round" />
          <g className="takt-mark-pendulum" style={{ transformOrigin: '23px 34px' }}>
            <path d={ROD} stroke={FLOW} strokeWidth="2" strokeLinecap="round" />
          </g>
          <rect
            transform="translate(42.3 20.2) rotate(14)"
            x="-2.5"
            y="-3.5"
            width="5"
            height="7"
            rx="0.9"
            fill={FLOW}
            mask={`url(#${id}c)`}
          />
        </>
      )}
    </svg>
  )
}

// Цвет потока: маятник, орбита и карточка, которую она уносит.
const FLOW = '#1bb3d6'
const ROD = 'M23 34 L39.3 8.8'
const SMALL_ROD = 'M22 40 L38 12.3'
// Передняя часть орбиты — перед корпусом, снизу; задняя — короткий
// хвост за карточкой, остальное скрыто корпусом.
const ORBIT = 'M7.39 19.71 A19 10 -7 1 0 41.9 22.38'
const ORBIT_BACK = 'M38.35 16.78 A19 10 -7 0 1 40.9 19.19'
// Чёрточки заголовков: [x, y, ширина] — посередине каждой карточки;
// у верхних боковых, где колонка сходится в щепу, чёрточка короче.
const TITLES: [number, number, number][] = [
  [19.3, 6.3, 1.6], [22.6, 6.3, 2.8], [27.1, 6.3, 1.6],
  [17.2, 16.3, 2.8], [22.6, 16.3, 2.8], [28, 16.3, 2.8],
  [15.6, 26.3, 2.8], [22.6, 26.3, 2.8], [29.6, 26.3, 2.8],
  [14.1, 36.3, 2.8], [22.6, 36.3, 2.8], [31.1, 36.3, 2.8],
]
