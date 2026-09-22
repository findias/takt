package httpapi

import (
	"net/http"

	"github.com/findias/takt/internal/demo"
	"github.com/findias/takt/internal/stand"
	"github.com/findias/takt/internal/version"
)

// Тестовый стенд ветки (ROADMAP 30.7). Ручка есть только в режиме
// стенда: на установке заказчика её нет вовсе, и 404 здесь — ответ
// маршрутизатора, а не наш отказ.
func (s *Server) registerStandRoutes(mux *http.ServeMux) {
	if !s.cfg.Stand {
		return
	}
	mux.HandleFunc("GET /api/stand", s.handleStand)
}

// Что на стенде: ветка, версия, коммиты поверх master (у master —
// с последнего выпуска) и как войти.
//
// Без входа: заметку читают и на экране входа — там же, где нужен
// пароль. Скрывать нечего: коммиты лежат в открытом репозитории,
// а пароль — от демонстрационных людей стенда, а не от чьих-то.
func (s *Server) handleStand(w http.ResponseWriter, r *http.Request) {
	note := stand.Read()
	writeJSON(w, http.StatusOK, map[string]any{
		"branch":   note.Branch,
		"since":    note.Since,
		"version":  version.Строка(),
		"commits":  note.Commits,
		"email":    demo.People[0].Email,
		"password": demo.Password,
	})
}
