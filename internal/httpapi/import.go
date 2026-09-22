package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer"
)

// Файл едет в теле запроса base64: 5 МБ таблицы — это десятки тысяч
// строк, больше предела на один перенос.
const (
	maxImportFile = 5 << 20
	maxImportBody = maxImportFile*4/3 + 1<<16
)

// Импорт — только от человека, не ключом: переезд делают один раз
// и глядя в предпросмотр, а у ключа предпросмотра нет.
func (s *Server) registerImportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/import/table", s.authed(s.handleImportTable))
	mux.HandleFunc("GET /api/import/table/sample", s.handleImportSample)
}

type importTableRequest struct {
	File []byte `json:"file"`
	// Пусто — сопоставление предлагает сервер по заголовкам.
	Mapping importer.Mapping `json:"mapping"`
	BoardID string           `json:"boardId"`
	// Куда ложатся значения колонки файла на существующей доске:
	// значение → колонка доски. Пусто — по названию, иначе новая.
	Columns map[string]string `json:"columns"`
	// Лист книги Excel; пусто — первый. У CSV листов нет.
	Sheet string `json:"sheet"`
	// Название новой доски, если boardId пуст.
	NewBoardName string `json:"newBoardName"`
	// false — предпросмотр: всё то же, но ничего не записано.
	Apply bool `json:"apply"`
}

type importTableResponse struct {
	// Листы книги в порядке Excel; у CSV — пусто.
	Sheets []string `json:"sheets"`
	// Лист, который прочитан: выбранный или первый с таблицей.
	Sheet   string           `json:"sheet,omitempty"`
	Headers []string         `json:"headers"`
	Mapping importer.Mapping `json:"mapping"`
	// Первые строки как есть — чтобы сопоставлять, глядя на данные,
	// а не только на заголовки.
	Sample [][]string `json:"sample"`
	// Почему перенести нельзя: сопоставление без заголовка или лист
	// без таблицы.
	MappingError string              `json:"mappingError,omitempty"`
	Report       *board.ImportReport `json:"report"`
}

func (s *Server) handleImportTable(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req importTableRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBody)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "файл больше 5 МБ — перенесите его по частям")
			return
		}
		writeError(w, http.StatusBadRequest, "тело запроса не разобрано")
		return
	}
	if len(req.File) > maxImportFile {
		writeError(w, http.StatusRequestEntityTooLarge, "файл больше 5 МБ — перенесите его по частям")
		return
	}
	table, sheets, sheet, err := importer.Read(req.File, req.Sheet)
	if err != nil && (len(sheets) == 0 || req.Apply) {
		writeCoded(w, http.StatusBadRequest, "import_unreadable", err.Error())
		return
	}
	out := importTableResponse{Sheets: sheets, Sheet: sheet, Headers: table.Headers, Mapping: req.Mapping}
	if out.Sheets == nil {
		out.Sheets = []string{}
	}
	if err != nil {
		// Книга читается, лист — нет: отвечаем списком листов и тем,
		// что не так с этим, — иначе выбрать другой лист негде.
		out.Headers, out.Mapping, out.Sample = []string{}, importer.Mapping{}, [][]string{}
		out.MappingError = i18n.Name(r.Context(), err.Error())
		writeJSON(w, http.StatusOK, out)
		return
	}
	if out.Mapping == nil {
		out.Mapping = importer.Suggest(table.Headers)
	}
	out.Sample = table.Rows[:min(3, len(table.Rows))]
	plan, err := importer.Build(table, out.Mapping)
	if err != nil {
		if req.Apply {
			writeCoded(w, http.StatusBadRequest, "import_mapping", err.Error())
			return
		}
		out.MappingError = i18n.Name(r.Context(), err.Error())
		writeJSON(w, http.StatusOK, out)
		return
	}

	out.Sample = readableDates(out.Sample, table.Headers, plan.Dates)

	rep, ok := s.importPlan(w, r, p, plan, board.ImportTarget{
		BoardID: req.BoardID, NewBoardName: req.NewBoardName, ColumnMap: columnMap(req.Columns),
	}, req.Apply)
	if !ok {
		return
	}
	out.Report = rep
	writeJSON(w, http.StatusOK, out)
}

// Образец таблицы — с экрана импорта. Импорт из произвольной таблицы —
// угадывание, и образец его ограничивает: вот так назовите колонки,
// так пишите даты. Заголовки — на языке человека, и оба варианта
// сервер понимает.
func (s *Server) handleImportSample(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="takt-import.csv"`)
	sample := importer.SampleRU
	if i18n.FromRequest(r) == i18n.EN {
		sample = importer.SampleEN
	}
	// BOM — чтобы Excel открыл кириллицу как кириллицу, а не кракозябрами.
	_, _ = w.Write([]byte("\xef\xbb\xbf" + strings.TrimLeft(sample, "\n")))
}

// readableDates показывает дату ячейки Excel датой, а не числом: в книге
// срок хранится днями от 1899 года, и «46295» в примере человеку ничего
// не говорит. Меняется только пример на экране — переносится то же.
func readableDates(sample [][]string, headers []string, dates []importer.DateFormat) [][]string {
	cols := map[int]bool{}
	for _, d := range dates {
		if d.Format != "excel" {
			continue
		}
		for i, h := range headers {
			if h == d.Header {
				cols[i] = true
			}
		}
	}
	if len(cols) == 0 {
		return sample
	}
	out := make([][]string, len(sample))
	for r, row := range sample {
		out[r] = append([]string(nil), row...)
		for i := range cols {
			if i < len(row) {
				if d, ok := importer.ExcelDate(row[i]); ok {
					out[r][i] = d.Format("2006-01-02")
				}
			}
		}
	}
	return out
}

// columnMap — выбор человека «значение → колонка» в том виде, в каком
// его сравнивает перенос: без учёта регистра и пробелов по краям.
func columnMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for v, id := range in {
		out[strings.ToLower(strings.TrimSpace(v))] = id
	}
	return out
}

// importPlan переносит разобранное — из таблицы или из YouGile —
// и отвечает отказом сам. false — отказ уже записан.
func (s *Server) importPlan(
	w http.ResponseWriter, r *http.Request, p auth.Principal,
	plan importer.Plan, target board.ImportTarget, apply bool,
) (*board.ImportReport, bool) {
	rep, err := s.boards.Import(r.Context(), p.OrgID, p.ID, target, plan, apply)
	switch {
	case err == nil:
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "доска не найдена")
		return nil, false
	case errors.Is(err, board.ErrReadOnlyBoard):
		writeError(w, http.StatusForbidden, board.ErrReadOnlyBoard.Error())
		return nil, false
	case errors.Is(err, board.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	default:
		s.fail(w, "импорт", err)
		return nil, false
	}
	// Претензии к строкам и потери говорятся на языке запроса, как
	// и отказы: они того же рода — объясняют, что делать.
	for i := range rep.Problems {
		rep.Problems[i].Message = i18n.Name(r.Context(), rep.Problems[i].Message)
	}
	for i := range rep.Lost {
		rep.Lost[i] = i18n.Name(r.Context(), rep.Lost[i])
	}
	return &rep, true
}
