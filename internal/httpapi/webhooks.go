package httpapi

import (
	"errors"
	"net/http"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/webhook"
)

// Подписки на события. Заводит и видит владелец: подписка выносит данные
// наружу, и список подписок — это список того, куда они уходят.

func (s *Server) registerWebhookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/webhooks", s.owner(s.handleListWebhooks))
	mux.HandleFunc("POST /api/webhooks", s.owner(s.handleCreateWebhook))
	mux.HandleFunc("DELETE /api/webhooks/{id}", s.owner(s.handleDeleteWebhook))
	mux.HandleFunc("GET /api/webhooks/{id}/deliveries", s.owner(s.handleDeliveries))
	mux.HandleFunc("POST /api/deliveries/{id}/retry", s.owner(s.handleRetryDelivery))
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	hooks, err := s.hooks.List(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список подписок", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": hooks})
}

// Ключ подписи возвращается один раз — в ответе на создание. Подписывать
// им будем мы, а получатель обязан сохранить его у себя.
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if !decode(w, r, &req) {
		return
	}
	hook, err := s.hooks.Create(r.Context(), p.OrgID, p.ID, req.Name, req.URL, req.Events)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, hook)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.hooks.Delete(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, webhook.ErrNotFound) {
		writeError(w, http.StatusNotFound, "подписка не найдена")
		return
	}
	if err != nil {
		s.fail(w, "удаление подписки", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	list, err := s.hooks.Deliveries(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, "журнал доставок", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": list})
}

// Ручной повтор нужен затем же, зачем и журнал: получателя починили,
// и хочется досдать то, что не доехало, не выдумывая события заново.
func (s *Server) handleRetryDelivery(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.hooks.Retry(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, webhook.ErrNotFound) {
		writeError(w, http.StatusNotFound, "доставка не найдена или уже доставлена")
		return
	}
	if err != nil {
		s.fail(w, "повтор доставки", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
