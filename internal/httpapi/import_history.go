package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/importer/pack"
	"github.com/findias/takt/internal/importer/yougile"
)

// Дотягивание истории и обсуждения YouGile фоном (ROADMAP 23.7, решение
// владельца 22.09.2026).
//
// Перенос по API переносит карточки сразу, а чат каждой задачи — два
// запроса (реплики и системные сообщения), и YouGile отвечает не чаще
// 50 раз в минуту: 800 задач — полчаса. Ждать этого на экране нельзя,
// поэтому после переноса запускается задание, а экран спрашивает, как
// идут дела.
//
// Ключ YouGile живёт только в памяти этого задания и умирает с ним: мы
// обещали его не хранить. Отсюда цена: перезапуск сервера обрывает
// задание, и дотягивают его повторным переносом той же доски — он
// знает, каким карточкам история ещё нужна (cards.source_history_at).

// historyTimeout — предел одного задания: доска в 10 000 задач при 50
// запросах в минуту — почти семь часов, а дольше держать ключ в памяти
// незачем.
const historyTimeout = 8 * time.Hour

type historyJobs struct {
	mu   sync.Mutex
	jobs map[string]*historyJob
}

// historyJob — ход дела по одной доске.
type historyJob struct {
	OrgID    string `json:"-"`
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Comments int    `json:"comments"`
	History  int    `json:"history"`
	// Skipped — карточки, чей чат YouGile не отдал: они остались ждать
	// следующего переноса этой доски.
	Skipped  int       `json:"skipped"`
	Failed   string    `json:"failed,omitempty"`
	Finished bool      `json:"finished"`
	Started  time.Time `json:"started"`
}

func (h *historyJobs) get(boardID string) (historyJob, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j, ok := h.jobs[boardID]
	if !ok {
		return historyJob{}, false
	}
	return *j, true
}

func (h *historyJobs) update(boardID string, fn func(*historyJob)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if j, ok := h.jobs[boardID]; ok {
		fn(j)
	}
}

// start заводит задание, если по этой доске оно не идёт. Одно на доску:
// два задания одновременно делили бы предел запросов YouGile пополам
// и писали бы одно и то же.
func (h *historyJobs) start(boardID, orgID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobs == nil {
		h.jobs = map[string]*historyJob{}
	}
	if j, ok := h.jobs[boardID]; ok && !j.Finished {
		return false
	}
	h.jobs[boardID] = &historyJob{OrgID: orgID, Started: time.Now()}
	return true
}

// startHistory запускает дотягивание по доске после переноса по API.
func (s *Server) startHistory(parent context.Context, p auth.Principal, key, boardID string) {
	if !s.histories.start(boardID, p.OrgID) {
		return
	}
	// Задание переживает запрос, который его запустил: у контекста
	// запроса отбирается отмена, но остаётся язык — реплика «из YouGile:
	// …» пишется на языке того, кто переносил.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), historyTimeout)
	go func() {
		defer cancel()
		err := s.pullHistory(ctx, p, key, boardID)
		s.histories.update(boardID, func(j *historyJob) {
			j.Finished = true
			if err != nil {
				j.Failed = err.Error()
			}
		})
		if err != nil {
			s.log.Error("история из YouGile", "board", boardID, "err", err)
		}
	}()
}

func (s *Server) pullHistory(ctx context.Context, p auth.Principal, key, boardID string) error {
	targets, err := s.boards.SourceHistoryTargets(ctx, p.OrgID, p.ID, boardID, yougile.Source)
	if err != nil {
		return err
	}
	s.histories.update(boardID, func(j *historyJob) { j.Total = len(targets) })
	if len(targets) == 0 {
		return nil
	}
	client := yougile.New(s.cfg.YougileURL, key)
	// Терпеливо: предел в 50 запросов в минуту здесь не отказ, а темп.
	client.Patient = true
	people, err := client.People(ctx)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	byID := map[string]pack.Person{}
	for _, person := range people {
		known[person.ExternalID] = true
		byID[person.ExternalID] = person
	}
	for _, t := range targets {
		chat, err := client.TaskChat(ctx, t.ExternalID, known, true)
		switch {
		// Пропавшая связь и отменённое задание — про всё дотягивание;
		// один непрочитанный чат — только про свою карточку: она
		// останется ждущей, и её возьмёт следующий перенос этой доски.
		case errors.Is(err, yougile.ErrUnreachable) || ctx.Err() != nil:
			return err
		case err != nil:
			s.log.Info("чат задачи не прочитан", "board", boardID, "task", t.ExternalID, "err", err)
			s.histories.update(boardID, func(j *historyJob) { j.Skipped++ })
			continue
		}
		comments := toImporter(chat.Comments, byID)
		history := toImporter(chat.History, byID)
		if err := s.boards.AddSourceHistory(ctx, p.OrgID, p.ID, boardID, t.CardID,
			yougile.Source, pack.Systems[yougile.Source], comments, history); err != nil {
			if errors.Is(err, board.ErrNotFound) {
				continue
			}
			return err
		}
		s.histories.update(boardID, func(j *historyJob) {
			j.Done++
			j.Comments += len(comments)
			j.History += len(history)
		})
	}
	return nil
}

// toImporter — записи чата в промежуточной модели: автор по имени
// и по ключу, как у реплик пакета.
func toImporter(list []pack.Comment, people map[string]pack.Person) []importer.Comment {
	out := make([]importer.Comment, 0, len(list))
	for _, c := range list {
		e := importer.Comment{At: c.At, Text: c.Text}
		if c.Author != nil {
			if person, ok := people[*c.Author]; ok {
				e.AuthorName = person.Name
				e.AuthorKey = "source:" + person.ExternalID
				if person.Email != nil && *person.Email != "" {
					e.AuthorEmail = strings.ToLower(strings.TrimSpace(*person.Email))
					e.AuthorKey = e.AuthorEmail
				}
			}
		}
		out = append(out, e)
	}
	return out
}

// Ход дотягивания по доске: экран переноса и доска спрашивают его,
// пока задание идёт. Нет задания — 404: дотягивать нечего или сервер
// перезапускали.
func (s *Server) handleHistoryProgress(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	job, ok := s.histories.get(r.PathValue("boardId"))
	if !ok || job.OrgID != p.OrgID {
		writeError(w, http.StatusNotFound, "история по этой доске не дотягивается")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
