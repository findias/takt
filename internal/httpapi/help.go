package httpapi

import (
	"net/http"

	"github.com/findias/takt/internal/help"
	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/version"
)

// Справка внутри приложения (ROADMAP 30.3). Без входа: это та же
// документация, что лежит в открытом репозитории, и открывают её
// в том числе с экрана входа.
func (s *Server) registerHelpRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /help", s.handleHelpRoot)
	mux.HandleFunc("GET /help/", s.handleHelpRoot)
	mux.HandleFunc("GET /help/{lang}/{page}", s.handleHelpPage)
	mux.HandleFunc("GET /help/{lang}/screenshots/{file}", s.handleHelpScreenshot)
}

// Корень справки — обзор на языке того, кто пришёл.
func (s *Server) handleHelpRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/help/"+string(i18n.FromRequest(r))+"/overview", http.StatusFound)
}

func (s *Server) handleHelpPage(w http.ResponseWriter, r *http.Request) {
	страница, ok, err := help.Собрать(r.PathValue("lang"), r.PathValue("page"), version.Строка())
	if err != nil {
		s.fail(w, "сборка справки", err)
		return
	}
	if !ok {
		// Не «не найдено» голым словом, а куда идти: адрес справки мог
		// устареть вместе с названием раздела.
		writeError(w, http.StatusNotFound, "такой страницы справки нет — начните с /help/")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(страница))
}

func (s *Server) handleHelpScreenshot(w http.ResponseWriter, r *http.Request) {
	raw, ok := help.Снимок(r.PathValue("lang"), r.PathValue("file"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Снимки вшиты и меняются только с версией — кэшировать их можно
	// до следующей выкладки, но не навсегда.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(raw)
}
