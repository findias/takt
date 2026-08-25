package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/findias/takt/internal/apiclient"
	"github.com/findias/takt/internal/auth"
)

// Сервисные клиенты. Список интеграций — это список того, что имеет
// доступ к данным, поэтому и видит его, и заводит только владелец.

func (s *Server) registerClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clients", s.owner(s.handleListClients))
	mux.HandleFunc("POST /api/clients", s.owner(s.handleCreateClient))
	mux.HandleFunc("DELETE /api/clients/{id}", s.owner(s.handleRevokeClient))
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	clients, err := s.client.List(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список ключей", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": clients})
}

// Токен возвращается один раз — в ответе на создание. В базе лежит только
// его хеш, и показать значение повторно невозможно: это то же правило,
// что и у приглашений.
func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresAt string   `json:"expiresAt"`
	}
	if !decode(w, r, &req) {
		return
	}

	var expires *time.Time
	if req.ExpiresAt != "" {
		parsed, err := time.Parse("2006-01-02", req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "срок пишется как ГГГГ-ММ-ДД")
			return
		}
		expires = &parsed
	}

	client, err := s.client.Create(r.Context(), p.OrgID, p.ID, req.Name, req.Scopes, expires)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, client)
	case errors.Is(err, apiclient.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) handleRevokeClient(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.client.Revoke(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, apiclient.ErrNotFound) {
		writeError(w, http.StatusNotFound, "ключ не найден")
		return
	}
	if err != nil {
		s.fail(w, "отзыв ключа", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
