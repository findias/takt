package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
)

// Задачи человека со всех досок, которые видит спрашивающий
// (board.Tasks). Без ?user= — свои.
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	person := r.URL.Query().Get("user")
	if person == "" {
		person = p.ID
	}
	list, err := s.boards.Tasks(r.Context(), p.OrgID, p.ID, person, r.URL.Query().Get("done") == "1")
	switch {
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "такого участника в организации нет")
	case err != nil:
		s.fail(w, "задачи человека", err)
	default:
		writeJSON(w, http.StatusOK, list)
	}
}

// Поиск карточек по всем видимым доскам (board.SearchCards) — чтобы
// связать задачу команды с эпиком портфеля и наоборот: выбор связи
// прежде предлагал только карточки своей доски.
func (s *Server) handleSearchCards(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	found, err := s.boards.SearchCards(r.Context(), p.OrgID, p.ID, r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, "поиск карточек", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": found})
}

// Путь карточки до корня дерева — для строки «Эпик › Фича» в панели.
// Отдельным запросом, а не в снимке доски: предки бывают на других
// досках, и считать путь каждой из пятисот карточек ради той, что
// открыта, незачем.
func (s *Server) handleCardPath(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
		return
	}
	path, err := s.boards.CardPath(r.Context(), p.OrgID, p.ID, id)
	switch {
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
	case err != nil:
		s.fail(w, "путь карточки", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"path": path})
	}
}

// Ветка карточки вниз через доски — для вида «Дерево» (этап 33.5).
// Снимок доски знает части только своей доски и прямых соседей; эпик
// портфеля раскладывается на три уровня по чужим доскам, и собрать его
// может только сервер.
func (s *Server) handleCardTree(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
		return
	}
	nodes, err := s.boards.CardTree(r.Context(), p.OrgID, p.ID, id)
	switch {
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
	case err != nil:
		s.fail(w, "ветка карточки", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}
