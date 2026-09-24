// Package pack — пакет переноса (docs/import-package.md): файл, в котором
// доски другого трекера переезжают в takt за периметром закрытого
// контура.
//
// Здесь и чтение, и запись. Пишет выгрузчик takt-fetch снаружи, читает
// takt внутри; держать обе стороны в одном месте — значит проверять их
// друг против друга одной проверкой, а не надеяться, что два описания
// одного формата не разойдутся.
//
// Чтение недоверчивое: файл пришёл из-за периметра, и всё, что не
// описано форматом, отвергается, а не пропускается.
package pack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Format и Version — чем пакет называет себя. Читаем только эту версию.
const (
	Format  = "takt-import-package"
	Version = 1
)

// Пределы из спецификации.
const (
	MaxCards   = 10000
	MaxEntries = 1000
	maxPart    = 256 << 20
)

// Systems — источники, которые формат знает, и как их назвать человеку.
var Systems = map[string]string{
	"yougile": "YouGile", "jira": "Jira", "trello": "Trello", "kaiten": "Kaiten",
	"weeek": "Weeek", "asana": "Asana", "notion": "Notion", "clickup": "ClickUp",
	"monday": "monday",
}

// ErrNotPackage — файл не пакет переноса вовсе.
var ErrNotPackage = errors.New("файл не похож на пакет переноса takt — его собирает takt-fetch")

// Manifest — manifest.json.
type Manifest struct {
	Format      string       `json:"format"`
	Version     int          `json:"version"`
	CreatedAt   time.Time    `json:"createdAt"`
	CreatedBy   string       `json:"createdBy"`
	CollectedBy string       `json:"collectedBy,omitempty"`
	Source      Source       `json:"source"`
	Boards      []BoardEntry `json:"boards"`
	Lost        []string     `json:"lost,omitempty"`
}

type Source struct {
	System  string `json:"system"`
	URL     string `json:"url,omitempty"`
	Account string `json:"account,omitempty"`
}

type BoardEntry struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Title  string `json:"title"`
	Cards  int    `json:"cards"`
}

// Board — boards/<n>.json.
type Board struct {
	ExternalID string   `json:"externalId"`
	Title      string   `json:"title"`
	Columns    []Column `json:"columns"`
	People     []Person `json:"people"`
	Labels     []Label  `json:"labels"`
	Cards      []Card   `json:"cards"`
	Lost       []string `json:"lost,omitempty"`
	// HistoryCollected — выгрузчик собирал историю задач: у карточки без
	// history её в источнике просто нет, и дотягивать нечего.
	HistoryCollected bool `json:"historyCollected,omitempty"`
	// Level — уровень доски: пусто или team — доска команды, portfolio —
	// доска-портфель эпиков (этап 33.6). Эпики источника едут отдельной
	// доской, а их части на досках команд ссылаются на них родителем.
	Level string `json:"level,omitempty"`
}

// Уровни доски, которые формат знает.
const (
	LevelTeam      = "team"
	LevelPortfolio = "portfolio"
)

type Column struct {
	ExternalID string  `json:"externalId"`
	Title      string  `json:"title"`
	Kind       *string `json:"kind"`
}

type Person struct {
	ExternalID string  `json:"externalId"`
	Email      *string `json:"email"`
	Name       string  `json:"name"`
}

type Label struct {
	ExternalID string `json:"externalId"`
	Name       string `json:"name"`
}

type Card struct {
	ExternalID string `json:"externalId"`
	// Number — номер задачи в источнике так, как его видят люди
	// («DEV-12»). Необязателен: externalId у YouGile — uuid, и по нему
	// задачу в старой системе не найти, а по номеру — найти.
	Number      string     `json:"number,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Column      string     `json:"column"`
	Assignees   []string   `json:"assignees,omitempty"`
	Labels      []string   `json:"labels,omitempty"`
	Estimate    *float64   `json:"estimate,omitempty"`
	Priority    string     `json:"priority,omitempty"`
	Due         string     `json:"due,omitempty"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Parent      *string    `json:"parent,omitempty"`
	Links       []Link     `json:"links,omitempty"`
	Comments    []Comment  `json:"comments,omitempty"`
	// History — история задачи в источнике: системные сообщения, «кто
	// что сделал и когда», текстом. Необязательна; есть — карточка
	// покажет её блоком «до переноса».
	History []Comment `json:"history,omitempty"`
}

type Link struct {
	Kind string `json:"kind"`
	To   string `json:"to"`
}

type Comment struct {
	Author *string   `json:"author"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

// Package — прочитанный и проверенный пакет.
type Package struct {
	Manifest Manifest
	Boards   []Board
}

// Write пишет пакет: манифест с суммами досок. Суммы и число карточек
// считает сам — у пишущего нет способа их перепутать.
func Write(w io.Writer, m Manifest, boards []Board) error {
	m.Format, m.Version = Format, Version
	m.Boards = nil
	zw := zip.NewWriter(w)
	for i, b := range boards {
		raw, err := json.MarshalIndent(b, "", "  ")
		if err != nil {
			return err
		}
		name := fmt.Sprintf("boards/%d.json", i+1)
		sum := sha256.Sum256(raw)
		m.Boards = append(m.Boards, BoardEntry{File: name, SHA256: hex.EncodeToString(sum[:]), Title: b.Title, Cards: len(b.Cards)})
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := f.Write(raw); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		return err
	}
	return zw.Close()
}

var boardName = regexp.MustCompile(`^boards/[1-9][0-9]*\.json$`)

// IsPackage — похож ли файл на пакет: zip с manifest.json внутри.
// Книга Excel тоже zip, и различать их нужно до разбора.
func IsPackage(raw []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			return true
		}
	}
	return false
}

// Read читает пакет и проверяет всё, что обещает формат: только
// описанные части, суммы досок, версию, пределы.
func Read(raw []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, ErrNotPackage
	}
	if len(zr.File) > MaxEntries {
		return nil, fmt.Errorf("в пакете больше %d частей — это не пакет переноса", MaxEntries)
	}
	parts := map[string][]byte{}
	for _, f := range zr.File {
		// Каталог «boards/» пишут не все архиваторы; он ничего не несёт.
		if f.Name == "boards/" {
			continue
		}
		if f.Name != "manifest.json" && !boardName.MatchString(f.Name) {
			return nil, fmt.Errorf("в пакете лишняя часть «%s» — такого формат не описывает, и пакет не читается", f.Name)
		}
		data, err := unpack(f)
		if err != nil {
			return nil, err
		}
		parts[f.Name] = data
	}
	manifestRaw, ok := parts["manifest.json"]
	if !ok {
		return nil, ErrNotPackage
	}
	var p Package
	if err := json.Unmarshal(manifestRaw, &p.Manifest); err != nil {
		return nil, fmt.Errorf("manifest.json не разобран: %w", err)
	}
	m := p.Manifest
	if m.Format != Format {
		return nil, ErrNotPackage
	}
	if m.Version != Version {
		return nil, fmt.Errorf("пакет версии %d, а эта установка читает только версию %d — обновите takt или соберите пакет выгрузчиком той же версии", m.Version, Version)
	}
	if _, ok := Systems[m.Source.System]; !ok {
		return nil, fmt.Errorf("источник «%s» формату незнаком", m.Source.System)
	}
	listed := map[string]bool{}
	for _, e := range m.Boards {
		data, ok := parts[e.File]
		if !ok {
			return nil, fmt.Errorf("манифест называет «%s», а в пакете её нет", e.File)
		}
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), e.SHA256) {
			return nil, fmt.Errorf("«%s» не сходится с суммой в манифесте — пакет повреждён по дороге, принесите его заново", e.File)
		}
		listed[e.File] = true
		var b Board
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, fmt.Errorf("«%s» не разобран: %w", e.File, err)
		}
		if b.Level != "" && b.Level != LevelTeam && b.Level != LevelPortfolio {
			return nil, fmt.Errorf("у доски «%s» уровень «%s» — формат знает только team и portfolio", b.Title, b.Level)
		}
		if len(b.Cards) > MaxCards {
			return nil, fmt.Errorf("на доске «%s» %d карточек — за раз переносим до %d, разделите её в источнике", b.Title, len(b.Cards), MaxCards)
		}
		p.Boards = append(p.Boards, b)
	}
	for name := range parts {
		if name != "manifest.json" && !listed[name] {
			return nil, fmt.Errorf("часть «%s» не названа в манифесте — без суммы её не проверить, и пакет не читается", name)
		}
	}
	if len(p.Boards) == 0 {
		return nil, errors.New("в пакете нет ни одной доски")
	}
	return &p, nil
}

func unpack(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("часть «%s» не распаковывается: %w", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxPart+1))
	if err != nil {
		return nil, fmt.Errorf("часть «%s» не распаковывается: %w", f.Name, err)
	}
	if len(data) > maxPart {
		return nil, fmt.Errorf("часть «%s» распаковывается больше чем в %d МБ — это не пакет переноса", f.Name, maxPart>>20)
	}
	return data, nil
}
