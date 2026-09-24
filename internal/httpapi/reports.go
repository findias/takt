package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/apiclient"
	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/report"
)

// Выгрузка для руководства (этап 24). Читается тем же разрешением,
// что доски и метрики: это те же карточки, только сведённые в таблицу.
// Ключу открыта нарочно — JSON тем же набором нужен ровно тем, кто
// обрабатывает отчёт программой.

func (s *Server) registerReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/reports/cards",
		s.scoped(apiclient.ScopeBoardsRead, s.handleReport))
	mux.HandleFunc("GET /api/reports/cards/count",
		s.scoped(apiclient.ScopeBoardsRead, s.handleReportCount))
}

// reportFilter читает параметры. Список можно передать повтором
// (?board=a&board=b) или через запятую — второе короче в ссылке,
// которую пересылают.
func reportFilter(q url.Values) (report.Filter, error) {
	var f report.Filter
	day := func(name string) (time.Time, error) {
		raw := q.Get(name)
		if raw == "" {
			return time.Time{}, nil
		}
		t, err := time.Parse(time.DateOnly, raw)
		if err != nil {
			return t, &report.BadFilter{Message: "дата периода пишется так: 2026-09-01"}
		}
		return t, nil
	}
	var err error
	if f.From, err = day("from"); err != nil {
		return f, err
	}
	if f.To, err = day("to"); err != nil {
		return f, err
	}
	list := func(name string) []string {
		var out []string
		for _, v := range q[name] {
			for _, part := range strings.Split(v, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
		}
		return out
	}
	ids := func(name string) ([]string, error) {
		out := list(name)
		for _, id := range out {
			if _, err := uuid.Parse(id); err != nil {
				return nil, &report.BadFilter{Message: "доски, подразделения, люди, метки и итерация " +
					"называются идентификаторами, а «" + id + "» — не он"}
			}
		}
		return out, nil
	}
	if f.Boards, err = ids("board"); err != nil {
		return f, err
	}
	if f.Teams, err = ids("team"); err != nil {
		return f, err
	}
	if f.Assignees, err = ids("assignee"); err != nil {
		return f, err
	}
	if f.Labels, err = ids("label"); err != nil {
		return f, err
	}
	iteration, err := ids("iteration")
	if err != nil {
		return f, err
	}
	if len(iteration) > 1 {
		return f, &report.BadFilter{Message: "итерация выбирается одна"}
	}
	if len(iteration) == 1 {
		f.Iteration = iteration[0]
	}
	f.Priorities = list("priority")
	f.States = list("state")
	f.WithArchive = q.Get("archived") == "1" || q.Get("archived") == "true"
	return f, nil
}

// failReport отвечает на отказ выгрузки. Отказ объясняет, что сделать:
// «слишком много» — сузить период, «не тот параметр» — какой именно.
func (s *Server) failReport(w http.ResponseWriter, err error) {
	var bad *report.BadFilter
	var many *report.TooMany
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Message)
	case errors.As(err, &many):
		// Свой код: экран по нему предлагает сузить выбор, а не
		// «попробовать ещё раз».
		writeCoded(w, http.StatusUnprocessableEntity, "report_too_large", many.Error())
	default:
		s.fail(w, "выгрузка карточек", err)
	}
}

// Сколько карточек попадёт в выгрузку — экран показывает это, пока
// человек выбирает параметры, и кнопка заранее знает, что файл
// не соберётся.
func (s *Server) handleReportCount(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	f, err := reportFilter(r.URL.Query())
	if err != nil {
		s.failReport(w, err)
		return
	}
	n, err := s.reports.Count(r.Context(), p.OrgID, p.ID, f)
	if err != nil {
		s.failReport(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"total": n, "limit": report.MaxRows})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	f, err := reportFilter(r.URL.Query())
	if err != nil {
		s.failReport(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "xlsx"
	}
	types := map[string]string{
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"csv":  "text/csv; charset=utf-8",
		"json": "application/json; charset=utf-8",
	}
	contentType, ok := types[format]
	if !ok {
		writeError(w, http.StatusBadRequest, "формат бывает xlsx, csv или json")
		return
	}

	ctx := r.Context()
	started := false
	open := func() (report.Sink, error) {
		// Заголовки — только когда отбор уже сосчитан: до этого
		// можно ответить отказом, после — уже нет.
		started = true
		name := "takt-cards-" + f.From.Format(time.DateOnly) + "--" + f.To.Format(time.DateOnly) + "." + format
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		switch format {
		case "csv":
			return report.NewCSV(ctx, w)
		case "json":
			return report.NewJSON(w, f)
		default:
			return report.NewXLSX(ctx, w, f)
		}
	}
	// Большая выгрузка пишется дольше общего предела записи сервера:
	// он стоит против медленных клиентов на обычных ответах, а здесь
	// медленный — сам ответ.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))

	err = s.reports.Export(ctx, p.OrgID, p.ID, f, open)
	switch {
	case err == nil:
	case !started:
		s.failReport(w, err)
	default:
		// Ответ уже начат: статус не поменять, остаётся журнал.
		// Файл при этом оборван, и программа, открывающая его, это
		// увидит — zip без оглавления и JSON без закрывающей скобки
		// не прочитать за целые.
		s.log.Error("выгрузка оборвалась", "err", err)
	}
}
