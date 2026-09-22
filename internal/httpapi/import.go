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
// строк, втрое больше предела на один перенос.
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
	// Название новой доски, если boardId пуст.
	NewBoardName string `json:"newBoardName"`
	// false — предпросмотр: всё то же, но ничего не записано.
	Apply bool `json:"apply"`
}

type importTableResponse struct {
	Headers []string         `json:"headers"`
	Mapping importer.Mapping `json:"mapping"`
	// Первые строки как есть — чтобы сопоставлять, глядя на данные,
	// а не только на заголовки.
	Sample [][]string `json:"sample"`
	// Почему перенести нельзя, пока не поправлено сопоставление.
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
	table, err := importer.ReadCSV(req.File)
	if err != nil {
		writeCoded(w, http.StatusBadRequest, "import_unreadable", err.Error())
		return
	}

	out := importTableResponse{Headers: table.Headers, Mapping: req.Mapping}
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

	target := board.ImportTarget{BoardID: req.BoardID, NewBoardName: req.NewBoardName}
	rep, err := s.boards.Import(r.Context(), p.OrgID, p.ID, target, plan, req.Apply)
	switch {
	case err == nil:
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "доска не найдена")
		return
	case errors.Is(err, board.ErrReadOnlyBoard):
		writeError(w, http.StatusForbidden, board.ErrReadOnlyBoard.Error())
		return
	case errors.Is(err, board.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		s.fail(w, "импорт таблицы", err)
		return
	}
	// Претензии к строкам говорятся на языке запроса, как и отказы:
	// они того же рода — объясняют, что делать.
	for i := range rep.Problems {
		rep.Problems[i].Message = i18n.Name(r.Context(), rep.Problems[i].Message)
	}
	out.Report = &rep
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
