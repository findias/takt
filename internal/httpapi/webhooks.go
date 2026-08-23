package httpapi

import (
	"errors"
	"net/http"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/retention"
	"github.com/konkov/agile/internal/webhook"
)

// Подписки на события. Заводит и видит владелец: подписка выносит данные
// наружу, и список подписок — это список того, куда они уходят.

func (s *Server) registerWebhookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/webhooks", s.owner(s.handleListWebhooks))
	mux.HandleFunc("POST /api/webhooks", s.owner(s.handleCreateWebhook))
	mux.HandleFunc("DELETE /api/webhooks/{id}", s.owner(s.handleDeleteWebhook))
	mux.HandleFunc("PATCH /api/webhooks/{id}", s.owner(s.handlePauseWebhook))
	mux.HandleFunc("GET /api/webhooks/{id}/deliveries", s.owner(s.handleDeliveries))
	mux.HandleFunc("POST /api/deliveries/{id}/retry", s.owner(s.handleRetryDelivery))
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	hooks, err := s.hooks.List(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список подписок", err)
		return
	}
	// Список событий едет вместе с подписками, а не отдельным запросом:
	// спрашивают его ровно там же и ровно тогда же. И едет он с сервера
	// потому, что у интерфейса был свой — вчетверо короче, без «работа
	// сделана», ради которой подписку чаще всего и заводят.
	//
	// Тем же рейсом едут границы автономии: сколько раз доставка
	// повторит, когда сдастся и сколько времени журнал вообще хранится.
	// Всё это система делает сама, а человеку было сказано только
	// «повторяем, удваивая паузу» — из чего нельзя узнать ни числа
	// попыток, ни того, что после последней подписка отключается совсем.
	// Числа считаются там, где действуют: второй их набор в клиенте
	// разошёлся бы с первым.
	policy := webhook.CurrentPolicy()
	writeJSON(w, http.StatusOK, map[string]any{
		"webhooks": hooks,
		"events":   s.hooks.Known(),
		"policy": map[string]any{
			"attempts":           policy.Attempts,
			"firstDelaySeconds":  policy.FirstDelaySeconds,
			"maxDelaySeconds":    policy.MaxDelaySeconds,
			"timeoutSeconds":     policy.TimeoutSeconds,
			"giveUpAfterMinutes": policy.GiveUpAfterMinutes,
			// Срок хранения берётся у уборщика, который и стирает.
			"keepDeliveredDays": int(retention.DeliveredTTL.Hours() / 24),
		},
	})
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

// Пауза — обратимое вмешательство в идущую работу. До неё вмешаться
// можно было только удалением, то есть необратимо, — при том, что
// правило продукта прямо обратное.
func (s *Server) handlePauseWebhook(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Paused *bool `json:"paused"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Paused == nil {
		writeError(w, http.StatusBadRequest, "нужно сказать, приостановить или возобновить")
		return
	}
	err := s.hooks.SetPaused(r.Context(), p.OrgID, p.ID, r.PathValue("id"), *req.Paused)
	if errors.Is(err, webhook.ErrNotFound) {
		writeError(w, http.StatusNotFound, "подписка не найдена")
		return
	}
	if err != nil {
		s.fail(w, "пауза подписки", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
