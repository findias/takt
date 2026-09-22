package importer

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// MaxRows — сколько строк переносим за раз: столько отдаёт за раз
// monday, самая скупая из известных выгрузок. Перенос идёт одной
// транзакцией, пока человек ждёт ответа, — и укладывается в секунды,
// потому что вставляет пачкой (замер 22.09.2026: 5000 строк за 1,7 с,
// предпросмотр столько же). Фоновая работа ради этого не нужна;
// понадобится, если предел поднимется на порядок. Предел называется
// в отказе числом, чтобы было ясно, на сколько частей делить файл.
const MaxRows = 10000

// ErrEmpty — в файле нет ни одной строки после заголовков.
var ErrEmpty = errors.New("в файле нет строк: первая строка — заголовки, задачи — со второй")

// ReadCSV разбирает CSV так, как его на самом деле сохраняют.
//
// Excel в русской локали пишет точку с запятой и Windows-1251, Google
// Таблицы и Jira — запятую и UTF-8, иногда с BOM. Спрашивать человека
// о кодировке бесполезно: он её не знает. Поэтому: BOM снимается,
// невалидный UTF-8 читается как Windows-1251, разделитель — тот
// из трёх, которого больше в строке заголовков.
func ReadCSV(raw []byte) (Table, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(raw) {
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(raw)
		if err != nil {
			return Table{}, fmt.Errorf("файл не в UTF-8 и не в Windows-1251: %w", err)
		}
		raw = decoded
	}

	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = delimiter(raw)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var t Table
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return Table{}, fmt.Errorf("строка %d: файл не разбирается как CSV (%v)", pe.StartLine, pe.Err)
			}
			return Table{}, err
		}
		if t.Headers == nil {
			t.Headers = trimAll(rec)
			continue
		}
		if blank(rec) {
			continue
		}
		line, _ := r.FieldPos(0)
		t.Rows = append(t.Rows, rec)
		t.Lines = append(t.Lines, line)
		if len(t.Rows) > MaxRows {
			return Table{}, fmt.Errorf("в файле больше %d строк — перенесите его по частям", MaxRows)
		}
	}
	if len(t.Rows) == 0 {
		return Table{}, ErrEmpty
	}
	return t, nil
}

func delimiter(raw []byte) rune {
	line, _, _ := bytes.Cut(raw, []byte("\n"))
	best, count := ',', -1
	for _, c := range []rune{';', ',', '\t'} {
		if n := strings.Count(string(line), string(c)); n > count {
			best, count = c, n
		}
	}
	return best
}

func trimAll(rec []string) []string {
	out := make([]string, len(rec))
	for i, s := range rec {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

func blank(rec []string) bool {
	for _, s := range rec {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}
