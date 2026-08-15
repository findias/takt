package httpapi

import (
	"errors"
	"net/http"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/board"
)

// Доступ к доске: команда, видимость, поимённый состав.
//
// Право менять решает база, а не эти обработчики: писать в доску может
// тот, кому она доступна на запись, а состав закрытой доски раздаёт
// владелец организации. Здесь проверяется только то, что видно снаружи, —
// что человек вообще может изменять данные.

func (s *Server) registerAccessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/boards/{id}/access", s.authed(s.handleBoardAccess))
	mux.HandleFunc("PUT /api/boards/{id}/access", s.authed(s.handleSetBoardAccess))
	mux.HandleFunc("PUT /api/boards/{id}/members/{userId}", s.authed(s.handleAddBoardMember))
	mux.HandleFunc("DELETE /api/boards/{id}/members/{userId}", s.authed(s.handleRemoveBoardMember))
	mux.HandleFunc("GET /api/boards/archived", s.authed(s.handleArchivedBoards))
	mux.HandleFunc("DELETE /api/boards/{id}", s.authed(s.handleArchiveBoard))
	mux.HandleFunc("POST /api/boards/{id}/restore", s.authed(s.handleRestoreBoard))
	mux.HandleFunc("GET /api/boards/{id}/cards/{cardId}/comments", s.authed(s.handleComments))
	mux.HandleFunc("POST /api/boards/{id}/cards/{cardId}/comments", s.authed(s.handleAddComment))
	mux.HandleFunc("PATCH /api/comments/{commentId}", s.authed(s.handleEditComment))
	mux.HandleFunc("DELETE /api/comments/{commentId}", s.authed(s.handleDeleteComment))
	mux.HandleFunc("GET /api/comments/{commentId}/revisions", s.authed(s.handleRevisions))
	mux.HandleFunc("GET /api/fields", s.authed(s.handleListFields))
	mux.HandleFunc("POST /api/fields", s.authed(s.handleCreateField))
	mux.HandleFunc("DELETE /api/fields/{id}", s.authed(s.handleArchiveField))
	mux.HandleFunc("GET /api/labels", s.authed(s.handleListLabels))
	mux.HandleFunc("POST /api/labels", s.authed(s.handleCreateLabel))
	mux.HandleFunc("DELETE /api/labels/{id}", s.authed(s.handleArchiveLabel))
	mux.HandleFunc("PUT /api/boards/{id}/sle", s.authed(s.handleSetSLE))
	mux.HandleFunc("POST /api/boards/{id}/iterations", s.authed(s.handleCreateIteration))
	mux.HandleFunc("POST /api/boards/{id}/iterations/{iterationId}/close",
		s.authed(s.handleCloseIteration))
}

func (s *Server) handleBoardAccess(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	access, err := s.boards.Access(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failAccess(w, "доступ к доске", err) {
		return
	}
	writeJSON(w, http.StatusOK, access)
}

func (s *Server) handleSetBoardAccess(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Visibility string  `json:"visibility"`
		TeamID     *string `json:"teamId"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.boards.SetAccess(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), req.Visibility, req.TeamID)
	if s.failAccess(w, "смена видимости доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddBoardMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.AddMember(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("userId"))
	if s.failAccess(w, "добавление в доску", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveBoardMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.RemoveMember(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("userId"))
	if s.failAccess(w, "исключение из доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// failAccess отвечает на ошибку и сообщает, что ответ уже написан.
//
// Потеря собственного доступа — конфликт, а не сбой: пользователь просит
// невозможного, и объяснить это надо словами, потому что база отказывает
// голым нарушением политики.
func (s *Server) failAccess(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "доска не найдена")
	case errors.Is(err, board.ErrWouldLoseAccess):
		writeError(w, http.StatusConflict, board.ErrWouldLoseAccess.Error())
	case errors.Is(err, board.ErrReadOnlyBoard):
		writeError(w, http.StatusForbidden, board.ErrReadOnlyBoard.Error())
	case errors.Is(err, board.ErrNotAuthor):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, board.ErrTooDeep):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, board.ErrFieldExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, board.ErrIterationClosed),
		errors.Is(err, board.ErrCardInAnotherIteration):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, board.ErrTeamRequired):
		writeError(w, http.StatusBadRequest, board.ErrTeamRequired.Error())
	case errors.Is(err, board.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.fail(w, what, err)
	}
	return true
}

func (s *Server) handleArchivedBoards(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	boards, err := s.boards.Archived(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "архив досок", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
}

// Убранная доска не удаляется: карточки и журнал переходов остаются, по
// ним считается поток. Поэтому DELETE здесь означает «убрать с глаз»,
// а не «стереть», и у него есть обратное действие.
func (s *Server) handleArchiveBoard(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.Archive(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failAccess(w, "архивация доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreBoard(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.Restore(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failAccess(w, "возврат доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Обещание доски: «85% работы проходит доску за 8 дней». Пустое значение
// снимает обещание.
func (s *Server) handleSetSLE(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Days        *int `json:"days"`
		Probability *int `json:"probability"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Вероятность по умолчанию — та, о которой говорит руководство
	// в своём же примере: «85% of work items will be finished in eight
	// days or less».
	probability := 85
	if req.Probability != nil {
		probability = *req.Probability
	}
	err := s.boards.SetSLE(r.Context(), p.OrgID, p.ID, r.PathValue("id"), req.Days, probability)
	if s.failAccess(w, "обещание доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Итерация заводится и закрывается отдельными вызовами, а не операцией
// над доской: она не меняет ни порядок карточек, ни версию доски, и
// проводить её через тот же канал значило бы делать вид, что меняет.
func (s *Server) handleCreateIteration(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Name     string `json:"name"`
		Goal     string `json:"goal"`
		StartsOn string `json:"startsOn"`
		EndsOn   string `json:"endsOn"`
	}
	if !decode(w, r, &req) {
		return
	}
	it, err := s.boards.CreateIteration(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), req.Name, req.Goal, req.StartsOn, req.EndsOn)
	if s.failAccess(w, "создание итерации", err) {
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

// Обратного действия у закрытия нет намеренно: закрытие — утверждение
// «вот что было сделано», и переоткрытие превратило бы отчёты
// в движущуюся мишень.
func (s *Server) handleCloseIteration(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.CloseIteration(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("iterationId"))
	if s.failAccess(w, "закрытие итерации", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Свои поля заводятся на организацию, а не на доску: одинаково названное
// поле на двух досках — это одно поле, иначе сводный отчёт складывает
// разные сущности с общим названием.
// Метки заводятся на организацию, как и свои поля: одинаково названная
// метка на двух досках — это одна метка, иначе фильтр по организации
// собирать не из чего.
func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	labels, err := s.boards.Labels(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список меток", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labels, "tones": board.Tones})
}

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Name string `json:"name"`
		Tone string `json:"tone"`
	}
	if !decode(w, r, &req) {
		return
	}
	label, err := s.boards.CreateLabel(r.Context(), p.OrgID, p.ID, req.Name, req.Tone)
	switch {
	case errors.Is(err, board.ErrLabelExists):
		writeError(w, http.StatusConflict, board.ErrLabelExists.Error())
	case err != nil:
		if s.failAccess(w, "создание метки", err) {
			return
		}
	default:
		writeJSON(w, http.StatusCreated, label)
	}
}

// Метка убирается из обихода, но не снимается с карточек: карточка,
// помеченная полгода назад, объясняет этим своё время в очереди,
// и стирать это задним числом значит делать историю неверной.
func (s *Server) handleArchiveLabel(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	if s.failAccess(w, "архивация метки",
		s.boards.ArchiveLabel(r.Context(), p.OrgID, p.ID, r.PathValue("id"))) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListFields(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	fields, err := s.boards.Fields(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список полей", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields})
}

func (s *Server) handleCreateField(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Name    string   `json:"name"`
		Kind    string   `json:"kind"`
		Options []string `json:"options"`
	}
	if !decode(w, r, &req) {
		return
	}
	field, err := s.boards.CreateField(r.Context(), p.OrgID, p.ID, req.Name, req.Kind, req.Options)
	if s.failAccess(w, "создание поля", err) {
		return
	}
	writeJSON(w, http.StatusCreated, field)
}

// Убранное поле не удаляет значения: поле заводили затем, чтобы данные
// были, и стирать их вместе с определением нельзя.
func (s *Server) handleArchiveField(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.ArchiveField(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failAccess(w, "архивация поля", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleComments(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	comments, err := s.boards.Comments(r.Context(), p.OrgID, p.ID, r.PathValue("cardId"))
	if s.failAccess(w, "обсуждение карточки", err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Body     string   `json:"body"`
		ParentID *string  `json:"parentId"`
		Mentions []string `json:"mentions"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.boards.AddComment(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("cardId"), req.Body, req.ParentID, req.Mentions)
	if s.failAccess(w, "комментарий", err) {
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.boards.EditComment(r.Context(), p.OrgID, p.ID, r.PathValue("commentId"), req.Body)
	if s.failAccess(w, "правка комментария", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.boards.DeleteComment(r.Context(), p.OrgID, p.ID, r.PathValue("commentId"))
	if s.failAccess(w, "удаление комментария", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Прежние версии текста. «Изменено» без них бесполезно: спрашивают
// не «правил ли он», а «что там было написано до».
func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	was, err := s.boards.Revisions(r.Context(), p.OrgID, p.ID, r.PathValue("commentId"))
	if s.failAccess(w, "прежние версии комментария", err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": was})
}
