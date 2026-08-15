package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/konkov/agile/internal/apiclient"
	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/scim"
)

// SCIM 2.0: заведение и отключение людей и групп провайдером.
//
// Путь без нашей версии — /scim/v2/…, так требует спецификация, и спорить
// с ней нельзя: адреса вбиты в настройки провайдера, а не согласуются
// с нами. По той же причине здесь чужие имена полей (userName, displayName)
// и чужой формат ошибок.
//
// Организацию определяет ключ: он выдан в конкретной организации, значит,
// и заводить людей будет в ней. Отдельная настройка «в какую организацию»
// была бы вторым источником правды, расходящимся с первым.

const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimPatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimType        = "application/scim+json"
)

func (s *Server) registerSCIMRoutes(mux *http.ServeMux) {
	// Описание возможностей провайдеры читают первым делом и по нему
	// решают, что нам можно присылать. Без ключа: это не данные,
	// а свойства сервера.
	mux.HandleFunc("GET /scim/v2/ServiceProviderConfig", s.handleSCIMConfig)

	mux.HandleFunc("GET /scim/v2/Users", s.scim(s.handleSCIMListUsers))
	mux.HandleFunc("POST /scim/v2/Users", s.scim(s.handleSCIMCreateUser))
	mux.HandleFunc("GET /scim/v2/Users/{id}", s.scim(s.handleSCIMGetUser))
	mux.HandleFunc("PUT /scim/v2/Users/{id}", s.scim(s.handleSCIMReplaceUser))
	mux.HandleFunc("PATCH /scim/v2/Users/{id}", s.scim(s.handleSCIMPatchUser))
	mux.HandleFunc("DELETE /scim/v2/Users/{id}", s.scim(s.handleSCIMDeleteUser))

	mux.HandleFunc("GET /scim/v2/Groups", s.scim(s.handleSCIMListGroups))
	mux.HandleFunc("POST /scim/v2/Groups", s.scim(s.handleSCIMCreateGroup))
	mux.HandleFunc("GET /scim/v2/Groups/{id}", s.scim(s.handleSCIMGetGroup))
	mux.HandleFunc("PATCH /scim/v2/Groups/{id}", s.scim(s.handleSCIMPatchGroup))
	mux.HandleFunc("PUT /scim/v2/Groups/{id}", s.scim(s.handleSCIMReplaceGroup))
	mux.HandleFunc("DELETE /scim/v2/Groups/{id}", s.scim(s.handleSCIMDeleteGroup))
}

// scim пускает только по ключу с разрешением на заведение людей.
// Сессия здесь не годится намеренно: провайдер ходит без браузера,
// а cookie у него взяться неоткуда.
func (s *Server) scim(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			scimError(w, http.StatusUnauthorized, "нужен ключ")
			return
		}
		principal, scopes, err := s.client.Authenticate(r.Context(), token)
		if err != nil {
			scimError(w, http.StatusUnauthorized, "ключ недействителен")
			return
		}
		if !contains(scopes, apiclient.ScopeSCIM) {
			scimError(w, http.StatusForbidden, "ключу не выдано разрешение "+apiclient.ScopeSCIM)
			return
		}
		next(w, r, principal)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- представления ---

type scimName struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type scimMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created,omitempty"`
	Location     string `json:"location,omitempty"`
}

type scimUser struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	DisplayName string      `json:"displayName,omitempty"`
	Name        *scimName   `json:"name,omitempty"`
	Emails      []scimEmail `json:"emails,omitempty"`
	Active      bool        `json:"active"`
	Meta        scimMeta    `json:"meta"`
}

type scimGroupMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type scimGroup struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id"`
	ExternalID  string            `json:"externalId,omitempty"`
	DisplayName string            `json:"displayName"`
	Members     []scimGroupMember `json:"members"`
	Meta        scimMeta          `json:"meta"`
}

func viewUser(u scim.User) scimUser {
	out := scimUser{
		Schemas:     []string{scimUserSchema},
		ID:          u.ID,
		ExternalID:  u.ExternalID,
		UserName:    u.Email,
		DisplayName: u.Name,
		Name:        &scimName{Formatted: u.Name},
		Active:      u.Active,
		Meta:        scimMeta{ResourceType: "User", Location: "/scim/v2/Users/" + u.ID},
	}
	if u.Email != "" {
		out.Emails = []scimEmail{{Value: u.Email, Primary: true}}
	}
	if !u.CreatedAt.IsZero() {
		out.Meta.Created = u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}

func viewGroup(g scim.Group) scimGroup {
	members := make([]scimGroupMember, 0, len(g.Members))
	for _, m := range g.Members {
		members = append(members, scimGroupMember{Value: m.ID, Display: m.Name})
	}
	out := scimGroup{
		Schemas:     []string{scimGroupSchema},
		ID:          g.ID,
		ExternalID:  g.ExternalID,
		DisplayName: g.Name,
		Members:     members,
		Meta:        scimMeta{ResourceType: "Group", Location: "/scim/v2/Groups/" + g.ID},
	}
	if !g.CreatedAt.IsZero() {
		out.Meta.Created = g.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}

func scimJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("content-type", scimType)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// scimError отвечает в чужом формате: провайдер разбирает именно его,
// а наш обычный вид ошибки для него — просто нечитаемый текст.
func scimError(w http.ResponseWriter, code int, detail string) {
	scimJSON(w, code, map[string]any{
		"schemas": []string{scimErrorSchema},
		"status":  itoa(code),
		"detail":  detail,
	})
}

func itoa(n int) string {
	if n < 100 || n > 599 {
		return "500"
	}
	return string(rune('0'+n/100)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10))
}

func (s *Server) scimFail(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, scim.ErrNotFound):
		scimError(w, http.StatusNotFound, "не найдено")
	case errors.Is(err, scim.ErrConflict):
		scimError(w, http.StatusConflict, "уже есть")
	default:
		s.log.Error("ошибка заведения из каталога", "этап", what, "err", err)
		scimError(w, http.StatusInternalServerError, "внутренняя ошибка")
	}
}

// --- фильтр ---
//
// Провайдеры присылают ровно один вид фильтра — точное равенство
// по единственному полю: userName eq "…" или displayName eq "…".
// Разбирать весь язык фильтров SCIM ради этого значит написать
// интерпретатор, который никто не вызовет.

var eqFilter = regexp.MustCompile(`(?i)^\s*(userName|displayName|externalId)\s+eq\s+"([^"]*)"\s*$`)

func filterValue(r *http.Request) string {
	raw := r.URL.Query().Get("filter")
	if raw == "" {
		return ""
	}
	m := eqFilter.FindStringSubmatch(raw)
	if m == nil {
		// Незнакомый фильтр — не повод отдать всех: провайдер получил бы
		// список, из которого сделал бы неверные выводы. Пустой список
		// честнее.
		return "\x00неразобранный фильтр"
	}
	return m[2]
}

// --- люди ---

func (s *Server) handleSCIMListUsers(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	users, err := s.scimSvc.ListUsers(r.Context(), p.OrgID, p.ID, filterValue(r))
	if err != nil {
		s.scimFail(w, "список людей", err)
		return
	}
	resources := make([]scimUser, 0, len(users))
	for _, u := range users {
		resources = append(resources, viewUser(u))
	}
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{scimListSchema},
		"totalResults": len(resources),
		"itemsPerPage": len(resources),
		"startIndex":   1,
		"Resources":    resources,
	})
}

func (s *Server) handleSCIMGetUser(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	u, err := s.scimSvc.GetUser(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if err != nil {
		s.scimFail(w, "человек", err)
		return
	}
	scimJSON(w, http.StatusOK, viewUser(u))
}

func (s *Server) handleSCIMCreateUser(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in scimUser
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		scimError(w, http.StatusBadRequest, "тело запроса не разбирается")
		return
	}
	u, err := s.scimSvc.CreateUser(r.Context(), p.OrgID, p.ID, scim.User{
		ExternalID: in.ExternalID,
		Email:      userNameOf(in),
		Name:       displayNameOf(in),
	})
	if err != nil {
		s.scimFail(w, "заведение человека", err)
		return
	}
	scimJSON(w, http.StatusCreated, viewUser(u))
}

// userNameOf: почта приезжает то в userName, то в emails — зависит
// от провайдера и от того, как его настроили.
func userNameOf(in scimUser) string {
	if in.UserName != "" {
		return in.UserName
	}
	for _, e := range in.Emails {
		if e.Value != "" {
			return e.Value
		}
	}
	return ""
}

func displayNameOf(in scimUser) string {
	if in.DisplayName != "" {
		return in.DisplayName
	}
	if in.Name != nil {
		if in.Name.Formatted != "" {
			return in.Name.Formatted
		}
		full := strings.TrimSpace(in.Name.GivenName + " " + in.Name.FamilyName)
		if full != "" {
			return full
		}
	}
	return ""
}

func (s *Server) handleSCIMReplaceUser(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in scimUser
	// Значение по умолчанию — «работает»: PUT без поля active не должен
	// толковаться как отключение.
	in.Active = true
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		scimError(w, http.StatusBadRequest, "тело запроса не разбирается")
		return
	}
	u, err := s.scimSvc.UpdateUser(r.Context(), p.OrgID, p.ID, r.PathValue("id"), scim.User{
		Email:  userNameOf(in),
		Name:   displayNameOf(in),
		Active: in.Active,
	})
	if err != nil {
		s.scimFail(w, "изменение человека", err)
		return
	}
	scimJSON(w, http.StatusOK, viewUser(u))
}

type scimPatch struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	} `json:"Operations"`
}

func (s *Server) handleSCIMPatchUser(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var patch scimPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&patch); err != nil {
		scimError(w, http.StatusBadRequest, "тело запроса не разбирается")
		return
	}
	id := r.PathValue("id")

	// Единственное, ради чего провайдер шлёт PATCH человеку, — отключение.
	// Остальные поля он присылает целым PUT.
	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		if path != "active" && path != "" {
			continue
		}
		active, ok := patchedActive(path, op.Value)
		if !ok {
			continue
		}
		if !active {
			if err := s.scimSvc.DeactivateUser(r.Context(), p.OrgID, p.ID, id); err != nil {
				s.scimFail(w, "отключение человека", err)
				return
			}
			scimJSON(w, http.StatusOK, viewUser(scim.User{ID: id, Active: false}))
			return
		}
	}

	u, err := s.scimSvc.GetUser(r.Context(), p.OrgID, p.ID, id)
	if err != nil {
		s.scimFail(w, "человек", err)
		return
	}
	scimJSON(w, http.StatusOK, viewUser(u))
}

// patchedActive достаёт признак из обоих видов, которыми его присылают:
// {"path":"active","value":false} и {"value":{"active":false}}.
func patchedActive(path string, raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	if path == "active" {
		var direct bool
		if err := json.Unmarshal(raw, &direct); err == nil {
			return direct, true
		}
		// Некоторые шлют строкой: "False".
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return !strings.EqualFold(strings.TrimSpace(text), "false"), true
		}
		return false, false
	}
	var object struct {
		Active *bool `json:"active"`
	}
	if err := json.Unmarshal(raw, &object); err == nil && object.Active != nil {
		return *object.Active, true
	}
	return false, false
}

func (s *Server) handleSCIMDeleteUser(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if err := s.scimSvc.DeactivateUser(r.Context(), p.OrgID, p.ID, r.PathValue("id")); err != nil {
		s.scimFail(w, "отключение человека", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- группы ---

func (s *Server) handleSCIMListGroups(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	groups, err := s.scimSvc.ListGroups(r.Context(), p.OrgID, p.ID, filterValue(r))
	if err != nil {
		s.scimFail(w, "список групп", err)
		return
	}
	resources := make([]scimGroup, 0, len(groups))
	for _, g := range groups {
		resources = append(resources, viewGroup(g))
	}
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{scimListSchema},
		"totalResults": len(resources),
		"itemsPerPage": len(resources),
		"startIndex":   1,
		"Resources":    resources,
	})
}

func (s *Server) handleSCIMGetGroup(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	g, err := s.scimSvc.GetGroup(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if err != nil {
		s.scimFail(w, "группа", err)
		return
	}
	scimJSON(w, http.StatusOK, viewGroup(g))
}

func (s *Server) handleSCIMCreateGroup(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in scimGroup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		scimError(w, http.StatusBadRequest, "тело запроса не разбирается")
		return
	}
	g, err := s.scimSvc.CreateGroup(r.Context(), p.OrgID, p.ID, scim.Group{
		ExternalID: in.ExternalID,
		Name:       in.DisplayName,
		Members:    membersOf(in.Members),
	})
	if err != nil {
		s.scimFail(w, "заведение группы", err)
		return
	}
	scimJSON(w, http.StatusCreated, viewGroup(g))
}

func membersOf(list []scimGroupMember) []scim.Member {
	out := make([]scim.Member, 0, len(list))
	for _, m := range list {
		out = append(out, scim.Member{ID: m.Value, Name: m.Display})
	}
	return out
}

func (s *Server) handleSCIMReplaceGroup(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in scimGroup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		scimError(w, http.StatusBadRequest, "тело запроса не разбирается")
		return
	}
	id := r.PathValue("id")
	if in.DisplayName != "" {
		if err := s.scimSvc.RenameGroup(r.Context(), p.OrgID, p.ID, id, in.DisplayName); err != nil {
			s.scimFail(w, "переименование группы", err)
			return
		}
	}
	if err := s.scimSvc.SetMembers(r.Context(), p.OrgID, p.ID, id, membersOf(in.Members)); err != nil {
		s.scimFail(w, "состав группы", err)
		return
	}
	s.handleSCIMGetGroup(w, r, p)
}

func (s *Server) handleSCIMPatchGroup(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var patch scimPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&patch); err != nil {
		scimError(w, http.StatusBadRequest, "тело запроса не разбирается")
		return
	}
	id := r.PathValue("id")

	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		action := strings.ToLower(strings.TrimSpace(op.Op))

		if path == "displayname" {
			var name string
			if err := json.Unmarshal(op.Value, &name); err == nil && name != "" {
				if err := s.scimSvc.RenameGroup(r.Context(), p.OrgID, p.ID, id, name); err != nil {
					s.scimFail(w, "переименование группы", err)
					return
				}
			}
			continue
		}
		if path != "members" && path != "" {
			continue
		}

		members, ok := patchedMembers(path, op.Value)
		if !ok {
			continue
		}
		var err error
		switch action {
		case "add":
			err = s.scimSvc.AddMembers(r.Context(), p.OrgID, p.ID, id, members)
		case "remove":
			// Remove без значения означает «очистить состав»: так шлёт Okta,
			// когда группу опустошают целиком.
			if len(members) == 0 {
				err = s.scimSvc.SetMembers(r.Context(), p.OrgID, p.ID, id, nil)
			} else {
				err = s.scimSvc.RemoveMembers(r.Context(), p.OrgID, p.ID, id, members)
			}
		case "replace":
			err = s.scimSvc.SetMembers(r.Context(), p.OrgID, p.ID, id, members)
		}
		if err != nil {
			s.scimFail(w, "состав группы", err)
			return
		}
	}

	s.handleSCIMGetGroup(w, r, p)
}

func patchedMembers(path string, raw json.RawMessage) ([]scim.Member, bool) {
	if len(raw) == 0 {
		return nil, path == "members"
	}
	var list []scimGroupMember
	if err := json.Unmarshal(raw, &list); err == nil {
		return membersOf(list), true
	}
	var object struct {
		Members []scimGroupMember `json:"members"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return membersOf(object.Members), true
	}
	return nil, false
}

func (s *Server) handleSCIMDeleteGroup(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if err := s.scimSvc.DeleteGroup(r.Context(), p.OrgID, p.ID, r.PathValue("id")); err != nil {
		s.scimFail(w, "удаление группы", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSCIMConfig описывает, что мы умеем. Провайдер читает это первым
// и по ответу решает, слать ли PATCH, фильтры и массовые запросы.
// Отвечать «умеем всё» — верный способ получить запрос, который мы
// не обработаем.
func (s *Server) handleSCIMConfig(w http.ResponseWriter, _ *http.Request) {
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":   map[string]any{"supported": true},
		// Массовые запросы не поддержаны: провайдеры к ним прибегают
		// на тысячах сотрудников, а у нас установка на сотню.
		"bulk":           map[string]any{"supported": false},
		"filter":         map[string]any{"supported": true, "maxResults": 200},
		"changePassword": map[string]any{"supported": false},
		"sort":           map[string]any{"supported": false},
		"etag":           map[string]any{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type":        "oauthbearertoken",
			"name":        "Ключ доступа",
			"description": "Ключ организации с разрешением " + apiclient.ScopeSCIM,
		}},
	})
}
