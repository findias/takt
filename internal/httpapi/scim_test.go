package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SCIM проверяется целиком по проводу: смысл протокола в том, что
// на другом конце чужая система, и наши внутренние вызовы о нём ничего
// не говорят.

type scimClient struct {
	api   *api
	token string
}

// withSCIMKey заводит ключ с разрешением на заведение людей.
func (a *api) withSCIMKey(t *testing.T, owner *session) *scimClient {
	t.Helper()
	created := owner.mustDo("POST", "/api/clients", map[string]any{
		"name": "Каталог " + uuid.NewString()[:8], "scopes": []string{"scim:write"},
	}, http.StatusCreated)
	token, _ := field(t, created, "token").(string)
	if token == "" {
		t.Fatalf("ключ не выдан, тело: %s", created)
	}
	return &scimClient{api: a, token: token}
}

func (c *scimClient) do(method, path string, body any) (int, []byte) {
	c.api.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.api.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.api.server.URL+path, reader)
	if err != nil {
		c.api.t.Fatal(err)
	}
	req.Header.Set("content-type", "application/scim+json")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	resp, err := c.api.client.Do(req)
	if err != nil {
		c.api.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (c *scimClient) must(method, path string, body any, want int) []byte {
	c.api.t.Helper()
	code, raw := c.do(method, path, body)
	if code != want {
		c.api.t.Fatalf("%s %s: код %d, ожидался %d; тело: %s", method, path, code, want, raw)
	}
	return raw
}

func newUserPayload(email string) map[string]any {
	return map[string]any{
		"schemas":    []string{scimUserSchema},
		"userName":   email,
		"externalId": "внешний-" + uuid.NewString()[:8],
		"name":       map[string]any{"givenName": "Иван", "familyName": "Петров"},
		"emails":     []map[string]any{{"value": email, "primary": true}},
		"active":     true,
	}
}

func TestDirectoryCreatesAndFindsPerson(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С каталогом")
	dir := a.withSCIMKey(t, owner)

	email := "scim-" + uuid.NewString()[:8] + "@example.test"
	created := dir.must("POST", "/scim/v2/Users", newUserPayload(email), http.StatusCreated)
	id, _ := field(t, created, "id").(string)
	if id == "" {
		t.Fatalf("заведённый человек без идентификатора: %s", created)
	}
	if field(t, created, "userName") != email {
		t.Errorf("userName в ответе: %v", field(t, created, "userName"))
	}
	if field(t, created, "active") != true {
		t.Error("заведённый человек не отмечен работающим")
	}

	// Повторное заведение того же — конфликт, а не второй человек:
	// провайдеры повторяют выгрузку, и каждая не должна плодить копии.
	if code, _ := dir.do("POST", "/scim/v2/Users", newUserPayload(email)); code != http.StatusConflict {
		t.Errorf("повторное заведение: код %d, ожидался 409", code)
	}

	// Провайдер ищет человека единственным способом — точным равенством.
	// Фильтр уезжает закодированным, как его шлёт провайдер.
	found := dir.must("GET",
		"/scim/v2/Users?filter="+url.QueryEscape(`userName eq "`+email+`"`), nil, http.StatusOK)
	if field(t, found, "totalResults") != float64(1) {
		t.Errorf("поиск по userName: %s", found)
	}

	// А по несуществующему адресу — пусто, а не «все».
	empty := dir.must("GET",
		"/scim/v2/Users?filter="+url.QueryEscape(`userName eq "нет@example.test"`), nil, http.StatusOK)
	if empty := field(t, empty, "totalResults"); empty != float64(0) {
		t.Errorf("поиск несуществующего вернул %v", empty)
	}

	dir.must("GET", "/scim/v2/Users/"+id, nil, http.StatusOK)
}

// Главное, ради чего ставят SCIM: увольнение снимает доступ. Но человека
// не стирает — иначе исчезли бы подписи под его карточками.
func TestDeactivationRemovesAccessButKeepsThePerson(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С увольнением")
	dir := a.withSCIMKey(t, owner)

	email := "fired-" + uuid.NewString()[:8] + "@example.test"
	created := dir.must("POST", "/scim/v2/Users", newUserPayload(email), http.StatusCreated)
	id := field(t, created, "id").(string)

	dir.must("PATCH", "/scim/v2/Users/"+id, map[string]any{
		"schemas": []string{scimPatchSchema},
		"Operations": []map[string]any{
			{"op": "replace", "path": "active", "value": false},
		},
	}, http.StatusOK)

	// Для каталога он больше не существует.
	if code, _ := dir.do("GET", "/scim/v2/Users/"+id, nil); code != http.StatusNotFound {
		t.Errorf("отключённый всё ещё виден: код %d", code)
	}
	// Для организации — тоже: участия нет.
	team := owner.mustDo("GET", "/api/team", nil, http.StatusOK)
	if bytes.Contains(team, []byte(email)) {
		t.Error("отключённый остался в составе организации")
	}

	// Но запись о человеке цела: под ней подписаны его действия.
	var alive bool
	if err := a.impl.db.Pool.QueryRow(context.Background(),
		`select exists (select 1 from users where id = $1)`, id).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Error("человек стёрт вместе с доступом")
	}
}

// Провайдеры расходятся: одни шлют отключение, другие удаление. Разными
// действиями это считать нельзя — иначе поведение зависит от настроек
// чужой системы.
func TestDeleteMeansTheSameAsDeactivate(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С удалением")
	dir := a.withSCIMKey(t, owner)

	email := "gone-" + uuid.NewString()[:8] + "@example.test"
	created := dir.must("POST", "/scim/v2/Users", newUserPayload(email), http.StatusCreated)
	id := field(t, created, "id").(string)

	dir.must("DELETE", "/scim/v2/Users/"+id, nil, http.StatusNoContent)

	if code, _ := dir.do("GET", "/scim/v2/Users/"+id, nil); code != http.StatusNotFound {
		t.Errorf("после удаления: код %d, ожидался 404", code)
	}
	var alive bool
	if err := a.impl.db.Pool.QueryRow(context.Background(),
		`select exists (select 1 from users where id = $1)`, id).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Error("удаление из каталога стёрло человека")
	}
}

func TestDirectoryGroupsBecomeTeams(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С группами")
	dir := a.withSCIMKey(t, owner)

	email := "member-" + uuid.NewString()[:8] + "@example.test"
	person := dir.must("POST", "/scim/v2/Users", newUserPayload(email), http.StatusCreated)
	personID := field(t, person, "id").(string)

	name := "Отдел " + uuid.NewString()[:8]
	group := dir.must("POST", "/scim/v2/Groups", map[string]any{
		"schemas": []string{scimGroupSchema}, "displayName": name,
	}, http.StatusCreated)
	groupID := field(t, group, "id").(string)

	// Группа появилась как команда — то есть видна там, где смотрят
	// структуру, а не только через SCIM.
	teams := owner.mustDo("GET", "/api/teams", nil, http.StatusOK)
	if !bytes.Contains(teams, []byte(name)) {
		t.Errorf("группа не стала командой: %s", teams)
	}

	dir.must("PATCH", "/scim/v2/Groups/"+groupID, map[string]any{
		"schemas": []string{scimPatchSchema},
		"Operations": []map[string]any{
			{"op": "add", "path": "members", "value": []map[string]any{{"value": personID}}},
		},
	}, http.StatusOK)

	after := dir.must("GET", "/scim/v2/Groups/"+groupID, nil, http.StatusOK)
	members, _ := field(t, after, "members").([]any)
	if len(members) != 1 {
		t.Fatalf("после добавления участников %d: %s", len(members), after)
	}

	dir.must("PATCH", "/scim/v2/Groups/"+groupID, map[string]any{
		"schemas": []string{scimPatchSchema},
		"Operations": []map[string]any{
			{"op": "remove", "path": "members", "value": []map[string]any{{"value": personID}}},
		},
	}, http.StatusOK)
	after = dir.must("GET", "/scim/v2/Groups/"+groupID, nil, http.StatusOK)
	if members, _ := field(t, after, "members").([]any); len(members) != 0 {
		t.Errorf("участник не убран: %s", after)
	}

	// Удаление группы — архивирование: доски, отданные команде, иначе
	// потеряли бы владельца.
	dir.must("DELETE", "/scim/v2/Groups/"+groupID, nil, http.StatusNoContent)
	if code, _ := dir.do("GET", "/scim/v2/Groups/"+groupID, nil); code != http.StatusNotFound {
		t.Errorf("удалённая группа: код %d, ожидался 404", code)
	}
}

// Отключение уносит человека и из команд: команда, в которой числится
// уволенный, вводит в заблуждение тех, кто по ней распределяет работу.
func TestDeactivationClearsTeamMembership(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С командой и увольнением")
	dir := a.withSCIMKey(t, owner)

	email := "team-" + uuid.NewString()[:8] + "@example.test"
	person := dir.must("POST", "/scim/v2/Users", newUserPayload(email), http.StatusCreated)
	personID := field(t, person, "id").(string)

	group := dir.must("POST", "/scim/v2/Groups", map[string]any{
		"schemas": []string{scimGroupSchema}, "displayName": "Отдел " + uuid.NewString()[:8],
		"members": []map[string]any{{"value": personID}},
	}, http.StatusCreated)
	groupID := field(t, group, "id").(string)

	dir.must("DELETE", "/scim/v2/Users/"+personID, nil, http.StatusNoContent)

	after := dir.must("GET", "/scim/v2/Groups/"+groupID, nil, http.StatusOK)
	if members, _ := field(t, after, "members").([]any); len(members) != 0 {
		t.Errorf("уволенный остался в команде: %s", after)
	}
}

func TestSCIMNeedsItsOwnPermission(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Без разрешения")

	created := owner.mustDo("POST", "/api/clients", map[string]any{
		"name": "Читатель " + uuid.NewString()[:8], "scopes": []string{"boards:read"},
	}, http.StatusCreated)
	weak := &scimClient{api: a, token: field(t, created, "token").(string)}

	if code, _ := weak.do("GET", "/scim/v2/Users", nil); code != http.StatusForbidden {
		t.Errorf("ключ без разрешения: код %d, ожидался 403", code)
	}
	anon := &scimClient{api: a}
	if code, _ := anon.do("GET", "/scim/v2/Users", nil); code != http.StatusUnauthorized {
		t.Errorf("без ключа: код %d, ожидался 401", code)
	}

	// И наоборот: ключ каталога не открывает доски, хотя его служебная
	// личность и владелец.
	dir := a.withSCIMKey(t, owner)
	if code, _ := dir.do("GET", "/api/v1/boards", nil); code != http.StatusForbidden {
		t.Errorf("ключ каталога на досках: код %d, ожидался 403", code)
	}
}

func TestServiceProviderConfigIsHonestAboutWhatWeSupport(t *testing.T) {
	a := newAPI(t)
	anon := &scimClient{api: a}
	raw := anon.must("GET", "/scim/v2/ServiceProviderConfig", nil, http.StatusOK)
	if field(t, raw, "bulk", "supported") != false {
		t.Error("обещаны массовые запросы, которых нет")
	}
	if field(t, raw, "patch", "supported") != true {
		t.Error("PATCH поддержан, но об этом не сказано")
	}
}

// Ключ каталога — не владелец организации.
//
// Роль владельца он носил ради двух таблиц: политика manage на teams
// и team_members пускала владельца и администратора поддерева, а каталог
// не был ни тем, ни другим. Довод «роль нужна, чтобы заводить людей» был
// неверен: users и memberships под RLS не попадают вовсе.
//
// Плата за роль была несоразмерна: в полосе /scim/v2 ключ держала
// проверка в коде, а не политика, — одного маршрута, обёрнутого s.authed
// вместо s.scoped, хватило бы, чтобы ключ из чужой системы стал
// владельцем арендатора. Поэтому проверяется не только «работа делается»,
// но и «сорвавшись с маршрута, ключ ничего не получает»: право каталога
// кончается на двух таблицах, и держит его база.
func TestDirectoryKeyIsNotAnOwner(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Каталог без роли владельца")
	dir := a.withSCIMKey(t, owner)
	ctx := context.Background()

	// Работа каталога делается без роли: группа заводится, человек
	// зачисляется. Обе таблицы — под политикой directory.
	email := "bez-roli-" + uuid.NewString()[:8] + "@example.test"
	person := dir.must("POST", "/scim/v2/Users", newUserPayload(email), http.StatusCreated)
	personID := field(t, person, "id").(string)
	group := dir.must("POST", "/scim/v2/Groups", map[string]any{
		"schemas": []string{scimGroupSchema}, "displayName": "Отдел " + uuid.NewString()[:8],
	}, http.StatusCreated)
	groupID := field(t, group, "id").(string)
	dir.must("PATCH", "/scim/v2/Groups/"+groupID, map[string]any{
		"schemas": []string{scimPatchSchema},
		"Operations": []map[string]any{
			{"op": "add", "path": "members", "value": []map[string]any{{"value": personID}}},
		},
	}, http.StatusOK)

	// А роли у него нет. Смотрится в базе: наружу роль ключа не выходит
	// вовсе — и это часть обещания, а не недосмотр.
	var orgID, botID, role string
	// memberships под RLS не попадает — оттуда организация и роль читаются
	// прямо. А вот api_clients под политикой, и без арендатора запрос
	// вернул бы ноль строк молча: строка ключа читается от лица владельца,
	// которому она и видна.
	if err := a.impl.db.Pool.QueryRow(ctx,
		`select org_id from memberships where user_id = $1`, owner.userID).Scan(&orgID); err != nil {
		t.Fatalf("организация владельца: %v", err)
	}
	if err := a.impl.db.InTenant(ctx, orgID, owner.userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`select user_id from api_clients where 'scim:write' = any (scopes)`).Scan(&botID)
	}); err != nil {
		t.Fatalf("чтение ключа каталога: %v", err)
	}
	if err := a.impl.db.Pool.QueryRow(ctx,
		`select role from memberships where org_id = $1 and user_id = $2`,
		orgID, botID).Scan(&role); err != nil {
		t.Fatalf("чтение роли ключа: %v", err)
	}
	if role == "owner" {
		t.Fatalf("у ключа каталога роль владельца организации — ровно то, " +
			"ради чего заведена политика directory (миграция 0048). " +
			"Заводит ключи apiclient.Create; роль выводится из разрешений")
	}

	// И даже дойдя до базы от своего имени — так было бы, окажись
	// маршрут обёрнут s.authed, — владельцем он не оказывается.
	err := a.impl.db.InTenant(ctx, orgID, botID, func(tx pgx.Tx) error {
		var isOwner, isDirectory bool
		if err := tx.QueryRow(ctx, `select app_is_owner(), app_is_directory()`).
			Scan(&isOwner, &isDirectory); err != nil {
			return err
		}
		if isOwner {
			t.Error("база считает ключ каталога владельцем организации")
		}
		if !isDirectory {
			t.Error("база не считает ключ каталога каталогом — работать он не сможет")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("прогон от лица ключа: %v", err)
	}

	// Право кончается на подразделениях и их составе: доску ключ каталога
	// не заводит. Отдельной транзакцией, потому что отказ политики рвёт
	// текущую — и это ровно тот отказ, которого мы здесь ждём.
	if err := a.impl.db.InTenant(ctx, orgID, botID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`insert into boards (org_id, name, visibility) values ($1, 'Каталогова доска', 'org')`, orgID)
		return err
	}); err == nil {
		t.Error("ключ каталога завёл доску — значит, право каталога шире двух таблиц")
	}
}
