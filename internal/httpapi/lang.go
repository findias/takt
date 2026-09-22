package httpapi

import (
	"net/http"

	"github.com/findias/takt/internal/i18n"
)

// speaking запоминает язык запроса на самом ответе.
//
// На ответе, а не в контексте запроса: отказ пишет writeCoded, а ему
// передают только ответ — и так в сотне мест. Протащить запрос в каждое
// значило бы переписать все отказы ради одного слова, и первый же новый
// обработчик, забывший его передать, отвечал бы по-русски.
func speaking(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := i18n.FromRequest(r)
		next.ServeHTTP(&langWriter{ResponseWriter: w, lang: l}, r.WithContext(i18n.WithLang(r.Context(), l)))
	})
}

type langWriter struct {
	http.ResponseWriter
	lang i18n.Lang
}

// Flush — по той же причине, что у обёртки журнала: поток изменений
// доски обязан отдавать написанное сразу.
func (w *langWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *langWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// langOf находит язык сквозь обёртки, которые легли поверх: журнал,
// запись ответа для повтора. Обёртки без Unwrap язык прячут, и отказ
// уходит по-русски — поэтому Unwrap есть у каждой, и это проверено
// тестом на английский отказ через все слои.
func langOf(w http.ResponseWriter) i18n.Lang {
	for {
		switch v := w.(type) {
		case *langWriter:
			return v.lang
		case interface{ Unwrap() http.ResponseWriter }:
			w = v.Unwrap()
		default:
			return i18n.RU
		}
	}
}

// say переводит сообщение на язык того, кому отвечаем.
func say(w http.ResponseWriter, msg string) string {
	return i18n.Say(langOf(w), msg)
}

// rememberLang ставит cookie языка, выбранного человеком.
//
// Её же ставит и клиент, но только в том браузере, где выбирали.
// Сервер ставит её при входе, чтобы с чужого устройства отказы
// и названия, которые он заводит, шли на выбранном языке с первого
// же ответа, а не после того, как клиент прочтёт «кто я» и переключится.
func (s *Server) rememberLang(w http.ResponseWriter, lang *string) {
	if lang == nil {
		return
	}
	// #nosec G124 -- без HttpOnly намеренно: язык читает клиент, чтобы
	// выбрать каталог подписей до первой отрисовки; секрета в ней нет.
	// Secure — из схемы BASE_URL, как у cookie сессии.
	http.SetCookie(w, &http.Cookie{
		Name:     "lang",
		Value:    *lang,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}
