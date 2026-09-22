package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/importer/yougile"
)

// Перенос из YouGile по API (ROADMAP 23.3). Четыре шага, и каждый —
// отдельный запрос без состояния на сервере: компании по логину
// и паролю, ключ компании, доски, перенос. Пароль нужен только первым
// двум и дальше не едет; ключ хранит браузер, пока открыт экран.
//
// Только от человека и только тому, кто пишет: ключ интеграции здесь
// ни к чему — переезд делают глядя в предпросмотр.
func (s *Server) registerYougileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/import/yougile/companies", s.authed(s.handleYougileCompanies))
	mux.HandleFunc("POST /api/import/yougile/key", s.authed(s.handleYougileKey))
	mux.HandleFunc("POST /api/import/yougile/boards", s.authed(s.handleYougileBoards))
	mux.HandleFunc("POST /api/import/yougile", s.authed(s.handleYougileImport))
}

type yougileRequest struct {
	// Выбор по людям источника: ключ человека → что с ним делать.
	People    map[string]board.PersonChoice `json:"people"`
	Login     string                        `json:"login"`
	Password  string                        `json:"password"`
	CompanyID string                        `json:"companyId"`
	Key       string                        `json:"key"`
	// Доска YouGile, откуда переносим.
	Board string `json:"board"`
	// Куда: как у переноса из таблицы.
	BoardID      string            `json:"boardId"`
	NewBoardName string            `json:"newBoardName"`
	Columns      map[string]string `json:"columns"`
	Apply        bool              `json:"apply"`
}

func (s *Server) yougileRequest(w http.ResponseWriter, r *http.Request, p auth.Principal) (yougileRequest, bool) {
	var req yougileRequest
	if s.cfg.YougileURL == config.YougileOff {
		writeCoded(w, http.StatusForbidden, "yougile_off",
			"перенос из YouGile на этой установке выключен — выгрузите таблицу в YouGile и перенесите её файлом")
		return req, false
	}
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return req, false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "тело запроса не разобрано")
		return req, false
	}
	req.Login, req.Key = strings.TrimSpace(req.Login), strings.TrimSpace(req.Key)
	return req, true
}

// yougileFailed отвечает на отказ YouGile кодом, по которому экран
// понимает, что делать: неверный вход — поправить поля, сети нет —
// перенести файлом.
func (s *Server) yougileFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, yougile.ErrDenied):
		writeCoded(w, http.StatusBadRequest, "yougile_denied", err.Error())
	case errors.Is(err, yougile.ErrUnreachable):
		writeCoded(w, http.StatusBadGateway, "yougile_unreachable", err.Error())
	case errors.Is(err, yougile.ErrBusy):
		writeCoded(w, http.StatusServiceUnavailable, "yougile_busy", err.Error())
	case errors.Is(err, yougile.ErrNotFound):
		writeCoded(w, http.StatusNotFound, "yougile_not_found", err.Error())
	default:
		s.fail(w, "обращение к YouGile", err)
	}
}

func (s *Server) handleYougileCompanies(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	req, ok := s.yougileRequest(w, r, p)
	if !ok {
		return
	}
	if req.Login == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "укажите почту и пароль, с которыми входите в YouGile")
		return
	}
	companies, err := yougile.Companies(r.Context(), s.cfg.YougileURL, req.Login, req.Password)
	if err != nil {
		s.yougileFailed(w, err)
		return
	}
	if companies == nil {
		companies = []yougile.Company{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"companies": companies})
}

func (s *Server) handleYougileKey(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	req, ok := s.yougileRequest(w, r, p)
	if !ok {
		return
	}
	if req.Login == "" || req.Password == "" || req.CompanyID == "" {
		writeError(w, http.StatusBadRequest, "укажите почту, пароль и компанию YouGile")
		return
	}
	key, created, err := yougile.Key(r.Context(), s.cfg.YougileURL, req.Login, req.Password, req.CompanyID)
	if err != nil {
		s.yougileFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "created": created})
}

func (s *Server) handleYougileBoards(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	req, ok := s.yougileRequest(w, r, p)
	if !ok {
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "нужен ключ API YouGile")
		return
	}
	boards, err := yougile.New(s.cfg.YougileURL, req.Key).Boards(r.Context())
	if err != nil {
		s.yougileFailed(w, err)
		return
	}
	if boards == nil {
		boards = []yougile.Board{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
}

func (s *Server) handleYougileImport(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	req, ok := s.yougileRequest(w, r, p)
	if !ok {
		return
	}
	if req.Key == "" || req.Board == "" {
		writeError(w, http.StatusBadRequest, "нужен ключ API YouGile и доска, откуда переносить")
		return
	}
	plan, err := yougile.New(s.cfg.YougileURL, req.Key).Plan(r.Context(), req.Board)
	if errors.Is(err, yougile.ErrTooBig) {
		writeCoded(w, http.StatusBadRequest, "import_unreadable", err.Error())
		return
	}
	if err != nil {
		s.yougileFailed(w, err)
		return
	}
	rep, ok := s.importPlan(w, r, p, plan, board.ImportTarget{
		BoardID: req.BoardID, NewBoardName: req.NewBoardName, ColumnMap: columnMap(req.Columns),
		People: req.People,
	}, req.Apply)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": rep})
}
