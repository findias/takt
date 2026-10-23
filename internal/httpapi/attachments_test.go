package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/findias/takt/internal/config"
)

// upload шлёт файл формой, как это делает браузер: поле file, имя
// из заголовка части.
func (s *session) upload(path, filename string, body []byte) (int, []byte) {
	s.api.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		s.api.t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		s.api.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		s.api.t.Fatal(err)
	}
	req, err := http.NewRequest("POST", s.api.server.URL+path, &buf)
	if err != nil {
		s.api.t.Fatal(err)
	}
	req.Header.Set("content-type", mw.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		s.api.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (s *session) download(path string) (*http.Response, []byte) {
	s.api.t.Helper()
	resp, err := s.client.Get(s.api.server.URL + path)
	if err != nil {
		s.api.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// Вложение к карточке (ROADMAP 36.1): приложить, увидеть в списке и в
// ленте, скачать тем же байтом и с тем же именем, убрать.
func TestAttachmentTravelsWithTheCard(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Вложения")
	boardID := owner.board("Поставка")
	cardID := owner.cardOn(boardID, "Снимок экрана")
	base := "/api/boards/" + boardID + "/cards/" + cardID + "/attachments"

	content := []byte("%PDF-1.4 условный документ")
	code, raw := owner.upload(base, "Отчёт за неделю.pdf", content)
	if code != http.StatusCreated {
		t.Fatalf("загрузка: код %d, %s", code, raw)
	}
	id, _ := field(t, raw, "id").(string)
	if got, _ := field(t, raw, "mediaType").(string); got != "application/pdf" {
		t.Errorf("тип выведен из расширения как %q, ожидался application/pdf", got)
	}

	// Список: имя, размер, автор, можно ли убрать; пределы — рядом.
	var list struct {
		Attachments []struct {
			ID, Name  string
			Size      int64
			Author    string
			Deletable bool
		}
		MaxBytes, CardBytes int64
		Extensions          []string
	}
	if err := json.Unmarshal(owner.mustDo("GET", base, nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Attachments) != 1 || list.Attachments[0].Name != "Отчёт за неделю.pdf" ||
		list.Attachments[0].Size != int64(len(content)) || !list.Attachments[0].Deletable {
		t.Errorf("список вложений не тот: %+v", list.Attachments)
	}
	if list.MaxBytes != config.AttachmentMaxDefault || list.CardBytes != 10*config.AttachmentMaxDefault || len(list.Extensions) == 0 {
		t.Errorf("пределы не доехали до экрана: %d / %d / %v", list.MaxBytes, list.CardBytes, list.Extensions)
	}

	// Скачивание: те же байты, имя в заголовке, вложением и без угадывания типа.
	resp, body := owner.download("/api/attachments/" + id)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, content) {
		t.Fatalf("скачалось не то: код %d, %d байт", resp.StatusCode, len(body))
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=UTF-8''") ||
		!strings.Contains(cd, ".pdf") {
		t.Errorf("Content-Disposition %q: ожидалось вложение с именем в UTF-8", cd)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("заголовки типа: %q / %q", resp.Header.Get("Content-Type"), resp.Header.Get("X-Content-Type-Options"))
	}

	// В ленте карточки — событие с именем файла.
	events := owner.mustDo("GET", "/api/boards/"+boardID+"/events?cardId="+cardID, nil, http.StatusOK)
	if !strings.Contains(string(events), `"attached"`) || !strings.Contains(string(events), "Отчёт за неделю.pdf") {
		t.Errorf("в ленте нет события о вложении: %s", events)
	}

	// Убрать — и список пуст, а скачивание отвечает «не найдено».
	owner.mustDo("DELETE", "/api/attachments/"+id, nil, http.StatusNoContent)
	if err := json.Unmarshal(owner.mustDo("GET", base, nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Attachments) != 0 {
		t.Errorf("после удаления список не пуст: %+v", list.Attachments)
	}
	if resp, _ := owner.download("/api/attachments/" + id); resp.StatusCode != http.StatusNotFound {
		t.Errorf("удалённое вложение скачивается: код %d", resp.StatusCode)
	}
	events = owner.mustDo("GET", "/api/boards/"+boardID+"/events?cardId="+cardID, nil, http.StatusOK)
	if !strings.Contains(string(events), `"detached"`) {
		t.Errorf("в ленте нет события об удалении вложения: %s", events)
	}
}

func TestAttachmentRefusesWhatItShould(t *testing.T) {
	a := newAPIWith(t, func(c *config.Config) { c.AttachmentMaxBytes = 1024 })
	owner := a.registerOrg("Пределы вложений")
	boardID := owner.board("Поставка")
	cardID := owner.cardOn(boardID, "Карточка")
	base := "/api/boards/" + boardID + "/cards/" + cardID + "/attachments"

	// Исполняемое и архивы — отказ по виду, с объяснением, что подойдёт.
	for _, name := range []string{"setup.exe", "сборка.zip", "страница.html", "скрипт.sh", "без-расширения"} {
		code, raw := owner.upload(base, name, []byte("x"))
		if code != http.StatusBadRequest {
			t.Errorf("%s: код %d, ожидался 400; %s", name, code, raw)
		}
		if got, _ := field(t, raw, "code").(string); got != "attachment_type" {
			t.Errorf("%s: код отказа %q, ожидался attachment_type", name, got)
		}
	}

	// Больше предела — 413 с числом.
	code, raw := owner.upload(base, "большой.txt", bytes.Repeat([]byte("a"), 1025))
	if code != http.StatusRequestEntityTooLarge {
		t.Errorf("файл больше предела: код %d, %s", code, raw)
	}
	if got, _ := field(t, raw, "code").(string); got != "attachment_too_big" {
		t.Errorf("код отказа %q, ожидался attachment_too_big", got)
	}
	if text, _ := field(t, raw, "error").(string); !strings.Contains(text, "1 КБ") {
		t.Errorf("отказ не называет предел: %q", text)
	}

	// Ровно предел — проходит; одиннадцатый такой же не помещается в карточку.
	for i := 0; i < 10; i++ {
		if code, raw := owner.upload(base, "часть.txt", bytes.Repeat([]byte("a"), 1024)); code != http.StatusCreated {
			t.Fatalf("файл №%d ровно в предел: код %d, %s", i+1, code, raw)
		}
	}
	code, raw = owner.upload(base, "лишний.txt", []byte("a"))
	if code != http.StatusConflict {
		t.Errorf("переполнение карточки: код %d, %s", code, raw)
	}
	if got, _ := field(t, raw, "code").(string); got != "attachments_full" {
		t.Errorf("код отказа %q, ожидался attachments_full", got)
	}

	// Пустой файл и форма без файла.
	if code, _ := owner.upload(base, "пусто.txt", nil); code != http.StatusBadRequest {
		t.Errorf("пустой файл: код %d", code)
	}
	code, raw = owner.do("POST", base, map[string]any{"file": "не форма"})
	if code != http.StatusBadRequest {
		t.Errorf("JSON вместо формы: код %d, %s", code, raw)
	}
	if got, _ := field(t, raw, "code").(string); got != "attachment_missing" {
		t.Errorf("код отказа %q, ожидался attachment_missing", got)
	}

	// Имя чистится: путь не доезжает, кавычки убраны. На свежей карточке —
	// предыдущая заполнена до предела.
	fresh := "/api/boards/" + boardID + "/cards/" + owner.cardOn(boardID, "Вторая") + "/attachments"
	code, raw = owner.upload(fresh, `..\..\"evil".png`, []byte("png"))
	if code != http.StatusCreated {
		t.Fatalf("файл с грязным именем: код %d, %s", code, raw)
	}
	if got, _ := field(t, raw, "name").(string); got != "evil.png" {
		t.Errorf("имя после чистки %q, ожидалось evil.png", got)
	}
}

// Права: читающий не прикладывает, автор и владелец убирают, чужой
// участник — нет; посторонний не видит ничего.
func TestAttachmentRightsFollowTheCard(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Права вложений")
	member := owner.join("member")
	other := owner.join("member")
	viewer := owner.join("viewer")
	boardID := owner.board("Общая")
	cardID := owner.cardOn(boardID, "Карточка")
	base := "/api/boards/" + boardID + "/cards/" + cardID + "/attachments"

	if code, _ := viewer.upload(base, "a.txt", []byte("a")); code != http.StatusForbidden {
		t.Errorf("читающий приложил файл: код %d", code)
	}
	code, raw := member.upload(base, "a.txt", []byte("a"))
	if code != http.StatusCreated {
		t.Fatalf("участник не смог приложить: код %d, %s", code, raw)
	}
	id, _ := field(t, raw, "id").(string)

	// Другой участник видит и скачивает, но убрать не может.
	if resp, _ := other.download("/api/attachments/" + id); resp.StatusCode != http.StatusOK {
		t.Errorf("коллега не скачал вложение: код %d", resp.StatusCode)
	}
	other.mustDo("DELETE", "/api/attachments/"+id, nil, http.StatusForbidden)
	var list struct {
		Attachments []struct{ Deletable bool }
	}
	if err := json.Unmarshal(other.mustDo("GET", base, nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Attachments) != 1 || list.Attachments[0].Deletable {
		t.Errorf("коллеге обещана кнопка удаления, которой у него нет: %+v", list.Attachments)
	}

	// Посторонний: ни скачать, ни убрать — «не найдено».
	stranger := a.registerOrg("Чужие")
	if resp, _ := stranger.download("/api/attachments/" + id); resp.StatusCode != http.StatusNotFound {
		t.Errorf("посторонний скачал вложение: код %d", resp.StatusCode)
	}
	stranger.mustDo("DELETE", "/api/attachments/"+id, nil, http.StatusNotFound)

	// Владелец убирает чужое.
	owner.mustDo("DELETE", "/api/attachments/"+id, nil, http.StatusNoContent)
}
