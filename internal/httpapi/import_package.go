package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/importer/pack"
)

// Пакет переноса (docs/import-package.md) — третий источник переноса,
// тот, что приходит в закрытый контур из-за периметра. Через экран —
// до 50 МБ; больше переносит администратор с командной строки.
const (
	maxPackageFile = 50 << 20
	maxPackageBody = maxPackageFile*4/3 + 1<<16
)

func (s *Server) registerPackageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/import/package", s.authed(s.handleImportPackage))
}

type packageRequest struct {
	File []byte `json:"file"`
	// Какая доска пакета, с единицы; пусто — первая.
	Board        int               `json:"board"`
	BoardID      string            `json:"boardId"`
	NewBoardName string            `json:"newBoardName"`
	Columns      map[string]string `json:"columns"`
	Apply        bool              `json:"apply"`
}

// packageSummary — что за пакет, словами для экрана: откуда, кем
// собран, какие в нём доски. Суммы и имена частей человеку ни к чему.
type packageSummary struct {
	Source      string    `json:"source"`
	URL         string    `json:"url,omitempty"`
	Account     string    `json:"account,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	CreatedBy   string    `json:"createdBy"`
	CollectedBy string    `json:"collectedBy,omitempty"`
	Boards      []struct {
		Title string `json:"title"`
		Cards int    `json:"cards"`
	} `json:"boards"`
}

func (s *Server) handleImportPackage(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req packageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPackageBody)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge,
				"пакет больше 50 МБ — такой переносит администратор командой takt import на сервере")
			return
		}
		writeError(w, http.StatusBadRequest, "тело запроса не разобрано")
		return
	}
	if len(req.File) > maxPackageFile {
		writeError(w, http.StatusRequestEntityTooLarge,
			"пакет больше 50 МБ — такой переносит администратор командой takt import на сервере")
		return
	}
	pkg, err := pack.Read(req.File)
	if err != nil {
		writeCoded(w, http.StatusBadRequest, "import_package", err.Error())
		return
	}
	if req.Board == 0 {
		req.Board = 1
	}
	plan, err := pkg.Plan(req.Board)
	if err != nil {
		writeCoded(w, http.StatusBadRequest, "import_package", err.Error())
		return
	}

	m := pkg.Manifest
	summary := packageSummary{Source: pack.Systems[m.Source.System], URL: m.Source.URL, Account: m.Source.Account,
		CreatedAt: m.CreatedAt, CreatedBy: m.CreatedBy, CollectedBy: m.CollectedBy}
	for _, b := range pkg.Boards {
		summary.Boards = append(summary.Boards, struct {
			Title string `json:"title"`
			Cards int    `json:"cards"`
		}{b.Title, len(b.Cards)})
	}

	rep, ok := s.importPlan(w, r, p, plan, board.ImportTarget{
		BoardID: req.BoardID, NewBoardName: req.NewBoardName, ColumnMap: columnMap(req.Columns),
	}, req.Apply)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"package": summary, "board": req.Board, "report": rep})
}
