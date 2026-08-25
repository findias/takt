package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/findias/takt/internal/auth"
)

// Выгрузка данных организации.
//
// Только владелец: файл содержит переписку в карточках и почтовые адреса
// всех участников, то есть больше, чем любой из них видит по отдельности.
//
// Отдаётся вложением, а не телом в браузере: выгрузка нужна как файл,
// и заставлять человека сохранять её из окна просмотра — лишний шаг,
// на котором теряется имя.

func (s *Server) registerExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/export", s.owner(s.handleExport))
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	withAudit := r.URL.Query().Get("audit") == "1"

	name := fmt.Sprintf("export-%s.json", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)

	// Пока не ушёл ни один байт, отказ ещё можно сказать словами — и это
	// разница между «сервер сломался» и «файл скачался испорченным».
	// После первого байта заголовок 200 уже отправлен и подменить его
	// нечем: остаётся оборвать соединение, чтобы недочитанный файл
	// не разобрался и выгрузку повторили.
	sent := &sentinelWriter{to: w}
	if err := s.export.Dump(r.Context(), sent, p.OrgID, p.ID, withAudit); err != nil {
		s.log.Error("выгрузка организации", "org", p.OrgID, "отдано байт", sent.written, "err", err)
		if sent.written == 0 {
			s.fail(w, "выгрузка организации", err)
			return
		}
		panic(http.ErrAbortHandler)
	}
}

type sentinelWriter struct {
	to      http.ResponseWriter
	written int
}

func (s *sentinelWriter) Write(p []byte) (int, error) {
	n, err := s.to.Write(p)
	s.written += n
	return n, err
}
