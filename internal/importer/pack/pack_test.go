package pack

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

// Пакет проверяется по обещаниям docs/import-package.md: что записано,
// то и прочитано; всё, что формат не описывает, отвергается; испорченное
// по дороге — тоже, с названием части.

func ptr(s string) *string { return &s }

func sample() (Manifest, []Board) {
	m := Manifest{
		CreatedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC), CreatedBy: "takt-fetch тест",
		Source: Source{System: "yougile", Account: "Склад"}, Lost: []string{"файлы вложений"},
	}
	done := "done"
	b := Board{
		ExternalID: "b-1", Title: "Склад",
		Columns: []Column{{ExternalID: "c-1", Title: "Нужно сделать"}, {ExternalID: "c-2", Title: "Сделано", Kind: &done}},
		People: []Person{
			{ExternalID: "u-1", Email: ptr("Anna@Example.test"), Name: "Анна"},
			{ExternalID: "u-2", Name: "Иван Петров"},
		},
		Labels: []Label{{ExternalID: "l-1", Name: "Склад №2"}},
		Cards: []Card{
			{ExternalID: "t-1", Title: "Сверить остатки", Column: "c-1", Assignees: []string{"u-1", "u-2"}, Labels: []string{"l-1"},
				Priority: "high", Due: "2026-09-30", Links: []Link{{Kind: "blocks", To: "t-2"}},
				Comments: []Comment{{Author: ptr("u-2"), At: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC), Text: "Начал"}}},
			{ExternalID: "t-2", Title: "Часть: выгрузить", Column: "c-1", Parent: ptr("t-1")},
			{ExternalID: "t-3", Title: "Цикл А", Column: "c-2", Parent: ptr("t-4")},
			{ExternalID: "t-4", Title: "Цикл Б", Column: "c-2", Parent: ptr("t-3")},
			{ExternalID: "t-5", Title: "Чужой родитель", Column: "c-9", Parent: ptr("нет-такой")},
		},
	}
	return m, []Board{b}
}

func write(t *testing.T) []byte {
	t.Helper()
	m, boards := sample()
	var buf bytes.Buffer
	if err := Write(&buf, m, boards); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWhatIsWrittenIsRead(t *testing.T) {
	raw := write(t)
	if !IsPackage(raw) {
		t.Fatal("пакет не узнан")
	}
	p, err := Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.Format != Format || p.Manifest.Boards[0].Cards != 5 || len(p.Manifest.Boards[0].SHA256) != 64 {
		t.Fatalf("манифест: %+v", p.Manifest)
	}
	plan, err := p.Plan(1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != "yougile" || plan.SourceName != "YouGile" || strings.Join(plan.Columns, ",") != "Нужно сделать,Сделано" {
		t.Fatalf("план: %+v", plan)
	}
	first := plan.Cards[0]
	if strings.Join(first.Assignees, ",") != "anna@example.test" || len(first.Unmatched) != 1 || first.Unmatched[0].Name != "Иван Петров" || first.Unmatched[0].Key == "" {
		t.Fatalf("люди: почта найдена у одного, имя без почты — у другого: %+v", first)
	}
	if first.Priority != "high" || first.Due == nil || len(first.Links) != 1 || first.Comments[0].AuthorName != "Иван Петров" {
		t.Fatalf("карточка: %+v", first)
	}
	if plan.Cards[1].Parent != "t-1" {
		t.Fatalf("подзадача потеряла родителя: %+v", plan.Cards[1])
	}
	// Цикл: обе связи внутри него отброшены и названы.
	if plan.Cards[2].Parent != "" || plan.Cards[3].Parent != "" {
		t.Fatalf("цикл подзадач перенесён: %+v %+v", plan.Cards[2], plan.Cards[3])
	}
	var msgs []string
	for _, pr := range plan.Problems {
		msgs = append(msgs, pr.Message)
	}
	all := strings.Join(msgs, " | ")
	for _, want := range []string{"замыкаются в цикл", "колонки «c-9»", "родителя «нет-такой»"} {
		if !strings.Contains(all, want) {
			t.Fatalf("не названо %q: %s", want, all)
		}
	}
}

func TestDamagedOrForeignPackageIsRefusedWithAReason(t *testing.T) {
	raw := write(t)
	// Подменили доску, не тронув манифест, — сумма не сходится.
	tampered := rezip(t, raw, func(name string, data []byte) (string, []byte) {
		if name == "boards/1.json" {
			return name, bytes.Replace(data, []byte("Сверить"), []byte("Стереть"), 1)
		}
		return name, data
	})
	if _, err := Read(tampered); err == nil || !strings.Contains(err.Error(), "не сходится с суммой") {
		t.Fatalf("подмена: %v", err)
	}
	extra := rezip(t, raw, func(name string, data []byte) (string, []byte) { return name, data }, "script.sh")
	if _, err := Read(extra); err == nil || !strings.Contains(err.Error(), "лишняя часть «script.sh»") {
		t.Fatalf("лишняя часть: %v", err)
	}
	newer := rezip(t, raw, func(name string, data []byte) (string, []byte) {
		if name == "manifest.json" {
			return name, bytes.Replace(data, []byte(`"version": 1`), []byte(`"version": 2`), 1)
		}
		return name, data
	})
	if _, err := Read(newer); err == nil || !strings.Contains(err.Error(), "только версию 1") {
		t.Fatalf("новая версия: %v", err)
	}
	if _, err := Read([]byte("не архив")); err != ErrNotPackage {
		t.Fatalf("не архив: %v", err)
	}
}

// rezip переписывает архив, меняя части и дописывая лишние.
func rezip(t *testing.T, raw []byte, change func(string, []byte) (string, []byte), extra ...string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		var data bytes.Buffer
		_, _ = data.ReadFrom(rc)
		rc.Close()
		name, body := change(f.Name, data.Bytes())
		w, _ := zw.Create(name)
		_, _ = w.Write(body)
	}
	for _, name := range extra {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte("echo"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
