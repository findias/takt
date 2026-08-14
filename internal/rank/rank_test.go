package rank

import (
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestBetweenKnownValues(t *testing.T) {
	cases := []struct{ prev, next, want string }{
		{"", "", "a0"},
		{"", "a0", "Zz"},
		{"", "Zz", "Zy"},
		{"a0", "", "a1"},
		{"a1", "", "a2"},
		{"a0", "a1", "a0V"},
		{"a1", "a2", "a1V"},
		{"a0V", "a1", "a0l"},
		{"Zz", "a0", "ZzV"},
		{"Zz", "a1", "a0"},
		{"", "Y00", "Xzzz"},
		{"bzz", "", "c000"},
		{"a0", "a0V", "a0G"},
		{"a0", "a0G", "a08"},
		{"b125", "b129", "b127"},
		{"a0", "a1V", "a1"},
		{"Zz", "a01", "a0"},
		{"", "a0V", "a0"},
		{"", "b999", "b99"},
	}
	for _, c := range cases {
		got, err := Between(c.prev, c.next)
		if err != nil {
			t.Errorf("Between(%q, %q) вернул ошибку: %v", c.prev, c.next, err)
			continue
		}
		if got != c.want {
			t.Errorf("Between(%q, %q) = %q, ожидалось %q", c.prev, c.next, got, c.want)
		}
	}
}

func TestBetweenRejectsBadInput(t *testing.T) {
	cases := []struct{ prev, next, why string }{
		{"a1", "a0", "соседи в обратном порядке"},
		{"a0", "a0", "одинаковые соседи"},
		{"a00", "", "хвостовой ноль в ключе"},
		{"a00", "a1", "хвостовой ноль в ключе"},
		{"0", "1", "недопустимый первый символ"},
		{"", "A" + strings.Repeat("0", 26), "зарезервированный ключ"},
		{"a", "", "обрезанный ключ"},
	}
	for _, c := range cases {
		if _, err := Between(c.prev, c.next); err == nil {
			t.Errorf("Between(%q, %q) прошёл без ошибки, ожидалось отклонение: %s", c.prev, c.next, c.why)
		}
	}
}

// Главное свойство: как бы ни вставляли, порядок ключей всегда совпадает
// с порядком элементов в списке.
func TestRandomInsertsKeepOrder(t *testing.T) {
	rnd := rand.New(rand.NewSource(20260814))
	keys := []string{}
	for i := 0; i < 3000; i++ {
		pos := rnd.Intn(len(keys) + 1)
		prev, next := "", ""
		if pos > 0 {
			prev = keys[pos-1]
		}
		if pos < len(keys) {
			next = keys[pos]
		}
		k, err := Between(prev, next)
		if err != nil {
			t.Fatalf("вставка №%d между %q и %q: %v", i, prev, next, err)
		}
		keys = append(keys, "")
		copy(keys[pos+1:], keys[pos:])
		keys[pos] = k
	}

	if !sort.StringsAreSorted(keys) {
		t.Fatal("ключи не отсортированы: порядок в списке разошёлся с порядком строк")
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("дубликат ключа %q", k)
		}
		seen[k] = true
	}
}

// Добавление в конец колонки — самое частое действие. Ключи не должны
// расти в длину: ради этого в схеме ключа есть целая часть.
func TestAppendKeepsKeysShort(t *testing.T) {
	last, longest := "", 0
	for i := 0; i < 5000; i++ {
		k, err := Between(last, "")
		if err != nil {
			t.Fatalf("добавление №%d: %v", i, err)
		}
		if k <= last {
			t.Fatalf("добавление №%d: %q не больше предыдущего %q", i, k, last)
		}
		if len(k) > longest {
			longest = len(k)
		}
		last = k
	}
	if longest > 4 {
		t.Errorf("после 5000 добавлений в конец самый длинный ключ — %d символов, ожидалось не больше 4", longest)
	}
}

// Худший случай для дробной части: каждая новая карточка кладётся вплотную
// за предыдущей. Ключи растут, но линейно и медленно.
func TestRepeatedInsertAfterFirst(t *testing.T) {
	first, err := Between("", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Between(first, "")
	if err != nil {
		t.Fatal(err)
	}
	prev, next := first, second
	for i := 0; i < 200; i++ {
		k, err := Between(prev, next)
		if err != nil {
			t.Fatalf("вставка №%d между %q и %q: %v", i, prev, next, err)
		}
		if !(prev < k && k < next) {
			t.Fatalf("вставка №%d: %q не лежит строго между %q и %q", i, k, prev, next)
		}
		next = k
	}
}

func TestNBetween(t *testing.T) {
	cases := []struct{ prev, next string }{
		{"", ""},
		{"a0", ""},
		{"", "a0"},
		{"a0", "a1"},
	}
	for _, c := range cases {
		keys, err := NBetween(c.prev, c.next, 25)
		if err != nil {
			t.Errorf("NBetween(%q, %q, 25): %v", c.prev, c.next, err)
			continue
		}
		if len(keys) != 25 {
			t.Errorf("NBetween(%q, %q, 25) вернул %d ключей", c.prev, c.next, len(keys))
			continue
		}
		if !sort.StringsAreSorted(keys) {
			t.Errorf("NBetween(%q, %q, 25) вернул неотсортированные ключи: %v", c.prev, c.next, keys)
		}
		if c.prev != "" && keys[0] <= c.prev {
			t.Errorf("NBetween(%q, %q): первый ключ %q не больше левой границы", c.prev, c.next, keys[0])
		}
		if c.next != "" && keys[len(keys)-1] >= c.next {
			t.Errorf("NBetween(%q, %q): последний ключ %q не меньше правой границы", c.prev, c.next, keys[len(keys)-1])
		}
	}

	if keys, err := NBetween("", "", 0); err != nil || len(keys) != 0 {
		t.Errorf("NBetween с n = 0 должен возвращать пустой срез, получено %v, %v", keys, err)
	}
}
