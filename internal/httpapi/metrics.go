package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/findias/takt/internal/apiclient"
	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/metrics"
)

// Метрики потока. Читаются тем же разрешением, что и доски: это та же
// доска, только посчитанная.

func (s *Server) registerMetricsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/boards/{id}/metrics",
		s.scoped(apiclient.ScopeBoardsRead, s.handleMetrics))
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	// Окно — часть вопроса, а не настройка: метрики потока без него
	// бессмысленны, команда полугодовой давности это другая команда.
	days := 90
	if raw := r.URL.Query().Get("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			days = parsed
		}
	}

	report, err := s.metrics.Report(r.Context(), p.OrgID, p.ID, r.PathValue("id"), days)
	if errors.Is(err, metrics.ErrNoData) {
		writeError(w, http.StatusNotFound, "доска не найдена")
		return
	}
	if err != nil {
		s.fail(w, "метрики доски", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
