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
	mux.HandleFunc("GET /help/{lang}/search", s.handleHelpSearch)
	mux.HandleFunc("GET /help/{lang}/screenshots/{file}", s.handleHelpScreenshot)
}

// Корень справки — обзор на языке того, кто пришёл.
func (s *Server) handleHelpRoot(w http.ResponseWriter, r *http.Request) {
	// #nosec G710 -- язык не берётся из запроса как есть: FromRequest
	// отвечает одним из двух своих значений, ru или en, и переход ведёт
	// только внутрь справки этого же сервера.
	http.Redirect(w, r, "/help/"+string(i18n.FromRequest(r))+"/overview", http.StatusFound)
}

func (s *Server) handleHelpPage(w http.ResponseWriter, r *http.Request) {
	страница, ok, err := help.Собрать(r.PathValue("lang"), r.PathValue("page"), i18n.Say(i18n.Lang(r.PathValue("lang")), version.Строка()))
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
	// #nosec G705 -- страница собрана из вшитой документации; язык
	// и адрес раздела сверены со списком, прежде чем попасть в разметку.
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
	// #nosec G705 -- вшитый PNG из docs/, отдаётся как image/png при nosniff;
	// имя файла без косых черт, а вшитая файловая система не знает «..».
	_, _ = w.Write(raw)
}

func (s *Server) handleHelpSearch(w http.ResponseWriter, r *http.Request) {
	страница, ok, err := help.СобратьПоиск(r.PathValue("lang"), r.URL.Query().Get("q"), i18n.Say(i18n.Lang(r.PathValue("lang")), version.Строка()))
	if err != nil {
		s.fail(w, "поиск по справке", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "такой страницы справки нет — начните с /help/")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// #nosec G705 -- запрос попадает в разметку только через
	// html.EscapeString; враждебный запрос проверяет
	// TestSearchPageSaysWhatToDoWhenNothingIsFound.
	_, _ = w.Write([]byte(страница))
}
