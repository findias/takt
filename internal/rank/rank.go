// Package rank реализует дробную индексацию: строковые ключи сортировки,
// между любыми двумя из которых всегда можно вставить третий.
//
// Зачем это нужно. Если хранить порядок карточек целыми числами, перемещение
// одной карточки требует перенумеровать половину колонки, а два человека,
// перетащившие карточки одновременно, получат одинаковые номера и
// перепутанный порядок. Со строковым ключом перемещение — это ровно один
// UPDATE одной строки, и коллизий не возникает.
//
// Алгоритм — каноническая дробная индексация (тот же подход, что в Figma;
// в Jira его вариант называется LexoRank). Ключ состоит из целой части,
// первый символ которой кодирует её длину, и дробной части. Целая часть
// делает добавление в конец списка дешёвым и не даёт ключам расти в длину
// при последовательных добавлениях; дробная обеспечивает вставку между
// любыми двумя соседями.
//
// Пустая строка в аргументах Between означает «границы нет»: Between("", x) —
// вставить перед первым, Between(x, "") — добавить в конец, Between("", "") —
// первый ключ в пустой колонке.
package rank

import (
	"errors"
	"fmt"
	"strings"
)

// digits — алфавит ключей. Порядок символов совпадает с порядком их байтов,
// поэтому обычное лексикографическое сравнение строк даёт нужную сортировку,
// а в SQL достаточно ORDER BY position.
const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ErrExhausted возвращается, когда ключи упёрлись в границу диапазона.
// На практике недостижимо: чтобы это произошло, нужно порядка 62^26 вставок
// подряд в один конец.
var ErrExhausted = errors.New("rank: диапазон ключей исчерпан")

// Between возвращает ключ, строго больший prev и строго меньший next.
// Пустая строка означает отсутствие соответствующей границы.
func Between(prev, next string) (string, error) {
	if prev != "" {
		if err := validate(prev); err != nil {
			return "", err
		}
	}
	if next != "" {
		if err := validate(next); err != nil {
			return "", err
		}
	}
	if prev != "" && next != "" && prev >= next {
		return "", fmt.Errorf("rank: %q >= %q, порядок соседей нарушен", prev, next)
	}

	if prev == "" {
		if next == "" {
			return "a" + digits[:1], nil
		}
		ib, err := integerPart(next)
		if err != nil {
			return "", err
		}
		fb := next[len(ib):]
		if ib == "A"+strings.Repeat(digits[:1], 26) {
			mid, err := midpoint("", fb)
			if err != nil {
				return "", err
			}
			return ib + mid, nil
		}
		if ib < next {
			return ib, nil
		}
		return decrementInteger(ib)
	}

	ia, err := integerPart(prev)
	if err != nil {
		return "", err
	}
	fa := prev[len(ia):]

	if next == "" {
		inc, err := incrementInteger(ia)
		if errors.Is(err, ErrExhausted) {
			mid, mErr := midpoint(fa, "")
			if mErr != nil {
				return "", mErr
			}
			return ia + mid, nil
		}
		if err != nil {
			return "", err
		}
		return inc, nil
	}

	ib, err := integerPart(next)
	if err != nil {
		return "", err
	}
	fb := next[len(ib):]

	if ia == ib {
		mid, err := midpoint(fa, fb)
		if err != nil {
			return "", err
		}
		return ia + mid, nil
	}
	inc, err := incrementInteger(ia)
	if err != nil {
		return "", err
	}
	if inc < next {
		return inc, nil
	}
	mid, err := midpoint(fa, "")
	if err != nil {
		return "", err
	}
	return ia + mid, nil
}

// NBetween возвращает n возрастающих ключей между prev и next.
// Используется для первичного заполнения колонки и для импорта.
func NBetween(prev, next string, n int) ([]string, error) {
	switch {
	case n < 0:
		return nil, fmt.Errorf("rank: n = %d, ожидалось неотрицательное", n)
	case n == 0:
		return []string{}, nil
	case n == 1:
		k, err := Between(prev, next)
		if err != nil {
			return nil, err
		}
		return []string{k}, nil
	}

	if next == "" {
		out := make([]string, 0, n)
		cur := prev
		for i := 0; i < n; i++ {
			k, err := Between(cur, "")
			if err != nil {
				return nil, err
			}
			out = append(out, k)
			cur = k
		}
		return out, nil
	}
	if prev == "" {
		out := make([]string, n)
		cur := next
		for i := n - 1; i >= 0; i-- {
			k, err := Between("", cur)
			if err != nil {
				return nil, err
			}
			out[i] = k
			cur = k
		}
		return out, nil
	}

	// обе границы заданы: делим пополам и рекурсивно заполняем половины
	mid := n / 2
	m, err := Between(prev, next)
	if err != nil {
		return nil, err
	}
	left, err := NBetween(prev, m, mid)
	if err != nil {
		return nil, err
	}
	right, err := NBetween(m, next, n-mid-1)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, n)
	out = append(out, left...)
	out = append(out, m)
	out = append(out, right...)
	return out, nil
}

// --- внутреннее ---

func digitIndex(c byte) int {
	return strings.IndexByte(digits, c)
}

// integerLength читает длину целой части из её первого символа.
// 'a'..'z' — положительная величина длиной 2..27, 'A'..'Z' — отрицательная.
func integerLength(head byte) (int, error) {
	switch {
	case head >= 'a' && head <= 'z':
		return int(head-'a') + 2, nil
	case head >= 'A' && head <= 'Z':
		return int('Z'-head) + 2, nil
	default:
		return 0, fmt.Errorf("rank: недопустимый первый символ ключа: %q", string(head))
	}
}

func integerPart(key string) (string, error) {
	if key == "" {
		return "", errors.New("rank: пустой ключ")
	}
	n, err := integerLength(key[0])
	if err != nil {
		return "", err
	}
	if n > len(key) {
		return "", fmt.Errorf("rank: обрезанный ключ: %q", key)
	}
	return key[:n], nil
}

func validate(key string) error {
	if key == "A"+strings.Repeat(digits[:1], 26) {
		return fmt.Errorf("rank: недопустимый ключ: %q", key)
	}
	ip, err := integerPart(key)
	if err != nil {
		return err
	}
	// дробная часть не должна оканчиваться на первый символ алфавита:
	// такой ключ имеет два представления, и инвариант «ключи сравнимы
	// как строки» перестал бы выполняться
	if frac := key[len(ip):]; frac != "" && frac[len(frac)-1] == digits[0] {
		return fmt.Errorf("rank: недопустимый ключ (хвостовой ноль): %q", key)
	}
	return nil
}

func validateInteger(x string) error {
	n, err := integerLength(x[0])
	if err != nil {
		return err
	}
	if len(x) != n {
		return fmt.Errorf("rank: повреждённая целая часть ключа: %q", x)
	}
	return nil
}

func incrementInteger(x string) (string, error) {
	if err := validateInteger(x); err != nil {
		return "", err
	}
	head, digs := x[0], []byte(x[1:])
	carry := true
	for i := len(digs) - 1; carry && i >= 0; i-- {
		d := digitIndex(digs[i]) + 1
		if d == len(digits) {
			digs[i] = digits[0]
		} else {
			digs[i] = digits[d]
			carry = false
		}
	}
	if carry {
		switch head {
		case 'Z':
			return "a" + digits[:1], nil
		case 'z':
			return "", ErrExhausted
		}
		h := head + 1
		if h > 'a' {
			digs = append(digs, digits[0])
		} else {
			digs = digs[:len(digs)-1]
		}
		return string(h) + string(digs), nil
	}
	return string(head) + string(digs), nil
}

func decrementInteger(x string) (string, error) {
	if err := validateInteger(x); err != nil {
		return "", err
	}
	head, digs := x[0], []byte(x[1:])
	borrow := true
	for i := len(digs) - 1; borrow && i >= 0; i-- {
		d := digitIndex(digs[i]) - 1
		if d == -1 {
			digs[i] = digits[len(digits)-1]
		} else {
			digs[i] = digits[d]
			borrow = false
		}
	}
	if borrow {
		switch head {
		case 'a':
			return "Z" + digits[len(digits)-1:], nil
		case 'A':
			return "", ErrExhausted
		}
		h := head - 1
		if h < 'Z' {
			digs = append(digs, digits[len(digits)-1])
		} else {
			digs = digs[:len(digs)-1]
		}
		return string(h) + string(digs), nil
	}
	return string(head) + string(digs), nil
}

// midpoint возвращает строку строго между a и b, где пустая b означает
// отсутствие верхней границы. Обе строки — дробные части без целой.
func midpoint(a, b string) (string, error) {
	if b != "" && a >= b {
		return "", fmt.Errorf("rank: %q >= %q", a, b)
	}
	if (a != "" && a[len(a)-1] == digits[0]) || (b != "" && b[len(b)-1] == digits[0]) {
		return "", errors.New("rank: хвостовой ноль в дробной части")
	}
	if b != "" {
		// снимаем общий префикс
		n := 0
		for n < len(b) {
			ca := byte(digits[0])
			if n < len(a) {
				ca = a[n]
			}
			if ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			if n > len(a) {
				a = ""
			} else {
				a = a[n:]
			}
			rest, err := midpoint(a, b[n:])
			if err != nil {
				return "", err
			}
			return b[:n] + rest, nil
		}
	}

	// первые символы различаются
	da := 0
	if a != "" {
		da = digitIndex(a[0])
	}
	db := len(digits)
	if b != "" {
		db = digitIndex(b[0])
	}
	if db-da > 1 {
		mid := (da + db + 1) / 2
		return digits[mid : mid+1], nil
	}
	// символы соседние — уходим глубже
	if b != "" && len(b) > 1 {
		return b[:1], nil
	}
	var restA string
	if a != "" {
		restA = a[1:]
	}
	rest, err := midpoint(restA, "")
	if err != nil {
		return "", err
	}
	return digits[da:da+1] + rest, nil
}
