//go:build load

package report

import (
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Выгрузка под нагрузкой (make load). Порогов в секундах нет — по той же
// причине, что в internal/board/load_test.go: они мерят машину. Мерится
// форма: время растёт с числом карточек линейно, а память сервера
// от числа строк не зависит — строки идут из курсора в ответ, и книга
// на пятьдесят тысяч карточек в памяти не собирается.
//
// Замер 24.09.2026 на стенде разработки: 50 000 карточек — XLSX
// за 4,6 с (3,9 МБ), CSV за 3,2 с, JSON за 3,5 с (24,7 МБ); пик памяти
// всего сервера — 29 МБ.

func (f *fixture) bulk(n int) {
	f.t.Helper()
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into cards (org_id, board_id, column_id, number, title, position,
			                   created_at, started_at, finished_at, outcome)
			select $1, $2, $3, 'ПОСТ-' || (100000 + g), 'Карточка замера ' || g, lpad(g::text, 8, '0'),
			       now() - (g % 300) * interval '1 day' - interval '2 days',
			       now() - (g % 300) * interval '1 day' - interval '1 day',
			       case when g % 3 = 0 then now() - (g % 300) * interval '1 day' end,
			       case when g % 3 = 0 then 'done' end
			  from generate_series($4::int, $5::int) g`,
			f.orgID, f.board, f.column[f.board], f.seq+1, f.seq+n)
		f.seq += n
		return err
	})
}

// measure выгружает книгу в никуда и возвращает время и пик кучи,
// снятый во время выгрузки.
func (f *fixture) measure() (time.Duration, uint64) {
	f.t.Helper()
	runtime.GC()
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapInuse > peak.Load() {
				peak.Store(m.HeapInuse)
			}
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	flt := period(365)
	err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) {
		return NewXLSX(f.ctx, io.Discard, flt)
	})
	took := time.Since(start)
	close(stop)
	<-done
	if err != nil {
		f.t.Fatal(err)
	}
	return took, peak.Load()
}

func TestReportScalesLinearlyInStreamedMemory(t *testing.T) {
	f := newFixture(t)
	f.bulk(5_000)
	smallTook, smallPeak := f.measure()
	f.bulk(45_000)
	largeTook, largePeak := f.measure()

	t.Logf("5 000 карточек: %v, куча до %d КБ; 50 000: %v, куча до %d КБ",
		smallTook, smallPeak>>10, largeTook, largePeak>>10)

	// Данных вдесятеро больше. Линейный рост — около десяти раз;
	// запрос на каждую карточку дал бы сотню.
	if ratio := float64(largeTook) / float64(smallTook); ratio > 20 {
		t.Errorf("выгрузка выросла в %.1f раза при десятикратном росте данных", ratio)
	}
	// Поток: куча не растёт вслед за строками. Полтора раза и два
	// мегабайта — запас на шум сборщика мусора; строки, собранные
	// в памяти, добавили бы десятки мегабайт.
	if largePeak > smallPeak*3/2+(2<<20) {
		t.Errorf("куча выросла с %d до %d КБ: выгрузка собирается в памяти, а не идёт потоком",
			smallPeak>>10, largePeak>>10)
	}
}
