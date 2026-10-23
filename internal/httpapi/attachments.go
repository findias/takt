package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
)

// Вложения к карточке (ROADMAP 36.1).
//
// Файл приходит multipart-формой, а не JSON с base64: так его не надо
// раздувать на треть и держать в памяти дважды. Отдаётся всегда
// вложением (Content-Disposition: attachment) и с запретом угадывать
// тип: ничто из приложенного не исполняется в браузере на нашем адресе.

func (s *Server) registerAttachmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/boards/{id}/cards/{cardId}/attachments", s.authed(s.handleAttachments))
	mux.HandleFunc("POST /api/boards/{id}/cards/{cardId}/attachments", s.authed(s.handleAttach))
	mux.HandleFunc("GET /api/attachments/{attachmentId}", s.authed(s.handleDownload))
	mux.HandleFunc("DELETE /api/attachments/{attachmentId}", s.authed(s.handleDetach))
}

func (s *Server) handleAttachments(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	list, err := s.boards.Attachments(r.Context(), p.OrgID, p.ID, r.PathValue("cardId"))
	if s.failAttachment(w, "список вложений", err) {
		return
	}
	// Пределы едут со списком: экран говорит «до 5 МБ» до выбора файла,
	// а не отказом после.
	writeJSON(w, http.StatusOK, map[string]any{
		"attachments": list,
		"maxBytes":    s.cfg.AttachmentLimit(),
		"cardBytes":   s.cfg.AttachmentCardBytes(),
		"extensions":  board.AttachmentExtensions(),
	})
}

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	limit := s.cfg.AttachmentLimit()
	// Запас — на заголовки частей формы; сам файл проверяется точно ниже.
	r.Body = http.MaxBytesReader(w, r.Body, limit+64<<10)
	mr, err := r.MultipartReader()
	if err != nil {
		writeCoded(w, http.StatusBadRequest, "attachment_missing", "ожидалась форма с файлом в поле file")
		return
	}
	var name string
	var body []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Тело оборвано пределом — это «файл больше предела», а не
			// «форма сломана»: человеку важно первое.
			if strings.Contains(err.Error(), "request body too large") {
				s.failAttachment(w, "вложение", board.AttachmentTooBig{Limit: limit})
				return
			}
			writeCoded(w, http.StatusBadRequest, "attachment_missing", "ожидалась форма с файлом в поле file")
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		name = part.FileName()
		// На один байт больше предела — чтобы отличить «ровно предел» от
		// «больше»: читать весь хвост ради отказа незачем.
		body, err = io.ReadAll(io.LimitReader(part, limit+1))
		_ = part.Close()
		if err != nil {
			if strings.Contains(err.Error(), "request body too large") {
				s.failAttachment(w, "вложение", board.AttachmentTooBig{Limit: limit})
				return
			}
			s.fail(w, "чтение вложения", err)
			return
		}
		break
	}
	if name == "" && len(body) == 0 {
		writeCoded(w, http.StatusBadRequest, "attachment_missing", "ожидалась форма с файлом в поле file")
		return
	}
	a, err := s.boards.Attach(r.Context(), p.OrgID, p.ID, r.PathValue("id"), r.PathValue("cardId"),
		name, body, limit, s.cfg.AttachmentCardBytes())
	if s.failAttachment(w, "вложение", err) {
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	a, err := s.boards.Attachment(r.Context(), p.OrgID, p.ID, r.PathValue("attachmentId"))
	if s.failAttachment(w, "скачивание вложения", err) {
		return
	}
	w.Header().Set("Content-Type", a.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Body)))
	w.Header().Set("Content-Disposition", contentDisposition(a.Name))
	// Тип назван сервером, и браузеру угадывать нечего: иначе текстовый
	// файл с разметкой внутри открылся бы страницей на нашем адресе.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.Body)
}

// contentDisposition — имя для скачивания по RFC 6266: ASCII-запасной
// вариант и полное имя в UTF-8. Кавычки и управляющие знаки из имени
// убраны при загрузке, поэтому заголовок не разрывается.
func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r > 0x7e || r < 0x20 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return mime.FormatMediaType("attachment", map[string]string{"filename": ascii}) +
		"; filename*=UTF-8''" + url.PathEscape(name)
}

func (s *Server) handleDetach(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.boards.Detach(r.Context(), p.OrgID, p.ID, r.PathValue("attachmentId"))
	if s.failAttachment(w, "удаление вложения", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) failAttachment(w http.ResponseWriter, what string, err error) bool {
	var tooBig board.AttachmentTooBig
	var full board.AttachmentsFull
	switch {
	case err == nil:
		return false
	case errors.As(err, &tooBig):
		writeCoded(w, http.StatusRequestEntityTooLarge, "attachment_too_big", err.Error())
	case errors.As(err, &full):
		writeCoded(w, http.StatusConflict, "attachments_full", err.Error())
	case errors.Is(err, board.ErrAttachmentType):
		writeCoded(w, http.StatusBadRequest, "attachment_type", err.Error())
	case errors.Is(err, board.ErrAttachmentNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, board.ErrNotAttachmentOwner):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		return s.failAccess(w, what, err)
	}
	return true
}
