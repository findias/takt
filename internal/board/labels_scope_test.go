package board

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Области меток.
//
// Метка организации действует всюду, метка подразделения — на досках
// подразделения и всех вложенных, метка доски — только на ней. Отсюда
// проверяемое: что где предлагается, что где вешается, где одинаковые
// названия запрещены и кто метку доски вообще видит.

func (f *fixture) scopedLabel(name, teamID, boardID string) Label {
	f.t.Helper()
	l, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID,
		LabelDraft{Name: name, Tone: "blue", TeamID: teamID, BoardID: boardID})
	if err != nil {
		f.t.Fatalf("создание метки «%s»: %v", name, err)
	}
	return l
}

// boardOfTeam заводит доску и отдаёт её подразделению.
func (f *fixture) boardOfTeam(name string, teamID *string) string {
	f.t.Helper()
	b, err := f.svc.Create(f.ctx, f.orgID, f.actorID, name, "")
	if err != nil {
		f.t.Fatal(err)
	}
	if teamID != nil {
		if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, b.ID, VisibilityOrg, teamID); err != nil {
			f.t.Fatal(err)
		}
	}
	return b.ID
}

func (f *fixture) offeredOn(boardID string) map[string]BoardLabel {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]BoardLabel{}
	for _, l := range snap.Labels {
		if l.Offered {
			out[l.Name] = l
		}
	}
	return out
}

func TestLabelScopeDecidesWhereItIsOffered(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	platform := f.team("Платформа", &dev)
	sales := f.team("Продажи", nil)

	devBoard := f.boardOfTeam("Разработка", &dev)
	platformBoard := f.boardOfTeam("Платформа", &platform)
	salesBoard := f.boardOfTeam("Продажи", &sales)
	loose := f.boardOfTeam("Ничья", nil)

	f.scopedLabel("Общая", "", "")
	f.scopedLabel("Техдолг", dev, "")
	f.scopedLabel("Инфра", platform, "")
	f.scopedLabel("Только тут", "", platformBoard)

	cases := []struct {
		board string
		want  []string
	}{
		// Подразделение действует вниз по дереву, но не вверх.
		{devBoard, []string{"Общая", "Техдолг"}},
		{platformBoard, []string{"Общая", "Техдолг", "Инфра", "Только тут"}},
		// Соседнее подразделение чужого не видит.
		{salesBoard, []string{"Общая"}},
		// Доска без подразделения получает только общие.
		{loose, []string{"Общая"}},
	}
	for _, c := range cases {
		got := f.offeredOn(c.board)
		if len(got) != len(c.want) {
			t.Errorf("на доске %s предлагается %v, ждали %v", c.board, keys(got), c.want)
			continue
		}
		for _, name := range c.want {
			if _, ok := got[name]; !ok {
				t.Errorf("на доске %s нет «%s», есть %v", c.board, name, keys(got))
			}
		}
	}

	// Происхождение называется: без него две метки из разных мест
	// на экране не различить.
	infra := f.offeredOn(platformBoard)["Инфра"]
	if infra.Scope != ScopeTeam || infra.ScopeName == nil || *infra.ScopeName != "Платформа" {
		t.Errorf("метка подразделения без происхождения: %+v", infra.Label)
	}
	own := f.offeredOn(platformBoard)["Только тут"]
	if own.Scope != ScopeBoard || own.ScopeID == nil || *own.ScopeID != platformBoard {
		t.Errorf("метка доски без происхождения: %+v", own.Label)
	}
}

func TestLabelFromAnotherScopeIsNotHungAndSaysWhy(t *testing.T) {
	f := newFixture(t)
	sales := f.team("Продажи", nil)
	salesBoard := f.boardOfTeam("Продажи", &sales)
	theirs := f.scopedLabel("Их метка", "", salesBoard)
	cardID := f.createCard("Моя карточка", f.columnA)

	_, err := f.apply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": theirs.ID})
	if err == nil {
		t.Fatal("метка чужой доски повесилась")
	}
	// Отказ называет, чья метка: «метки нет» про видимую в списке метку
	// отправило бы искать пропажу.
	if !strings.Contains(err.Error(), "Продажи") {
		t.Errorf("отказ не называет, откуда метка: %v", err)
	}
	if got := f.labelsOf(cardID); len(got) != 0 {
		t.Errorf("на карточке чужая метка: %v", got)
	}
}

func TestLabelNamesDoNotRepeatWhereScopesOverlap(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	platform := f.team("Платформа", &dev)
	sales := f.team("Продажи", nil)
	platformBoard := f.boardOfTeam("Платформа", &platform)
	salesBoard := f.boardOfTeam("Продажи", &sales)

	f.scopedLabel("Срочно", dev, "")

	refused := []struct {
		what    string
		d       LabelDraft
		mention string
	}{
		{"у организации — она покрыла бы подразделение", LabelDraft{Name: "срочно"}, "Разработка"},
		{"у вложенного подразделения", LabelDraft{Name: "Срочно", TeamID: platform}, "Разработка"},
		{"у доски вложенного подразделения", LabelDraft{Name: "СРОЧНО", BoardID: platformBoard}, "Разработка"},
		{"в том же подразделении", LabelDraft{Name: "Срочно", TeamID: dev}, "Разработка"},
	}
	for _, c := range refused {
		_, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID, c.d)
		if !errors.Is(err, ErrLabelExists) {
			t.Errorf("вторая «Срочно» %s заведена, ошибка: %v", c.what, err)
			continue
		}
		if !strings.Contains(err.Error(), c.mention) {
			t.Errorf("отказ %s не говорит, где уже есть: %v", c.what, err)
		}
	}

	// Области, которые не пересекаются, свои названия выбирают сами.
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID,
		LabelDraft{Name: "Срочно", TeamID: sales}); err != nil {
		t.Errorf("«Срочно» соседнего подразделения не заведена: %v", err)
	}
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID,
		LabelDraft{Name: "Своя", BoardID: salesBoard}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID,
		LabelDraft{Name: "Своя", BoardID: platformBoard}); err != nil {
		t.Errorf("одноимённая метка на другой доске не заведена: %v", err)
	}
}

func TestArchivedLabelComesBack(t *testing.T) {
	f := newFixture(t)
	old := f.label("Ждём")
	if err := f.svc.ArchiveLabel(f.ctx, f.orgID, f.actorID, old.ID); err != nil {
		t.Fatal(err)
	}
	// Повтор безобиден: две вкладки, одна кнопка.
	if err := f.svc.ArchiveLabel(f.ctx, f.orgID, f.actorID, old.ID); err != nil {
		t.Errorf("повторная архивация: %v", err)
	}
	if err := f.svc.RestoreLabel(f.ctx, f.orgID, f.actorID, old.ID); err != nil {
		t.Fatal(err)
	}
	if l := f.boardLabel(old.ID); l == nil || !l.Offered {
		t.Error("возвращённая метка не предлагается")
	}

	// Пока метка лежала в архиве, её название могли занять.
	if err := f.svc.ArchiveLabel(f.ctx, f.orgID, f.actorID, old.ID); err != nil {
		t.Fatal(err)
	}
	f.scopedLabel("Ждём", "", f.boardID)
	err := f.svc.RestoreLabel(f.ctx, f.orgID, f.actorID, old.ID)
	if !errors.Is(err, ErrLabelExists) {
		t.Errorf("вернулась вторая «Ждём», ошибка: %v", err)
	}

	if err := f.svc.RestoreLabel(f.ctx, f.orgID, f.actorID, uuid.NewString()); !errors.Is(err, ErrLabelNotFound) {
		t.Errorf("возврат несуществующей метки: %v", err)
	}
}

// Метку доски видит тот, кто видит доску: название само бывает
// сведением.
func TestBoardLabelIsHiddenWithTheBoard(t *testing.T) {
	f := newFixture(t)
	secret := f.boardOfTeam("Найм", nil)
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, secret, VisibilityPrivate, nil); err != nil {
		t.Fatal(err)
	}
	hidden := f.scopedLabel("Увольнение", "", secret)
	outsider := f.inviteMember("Посторонний")

	list, err := f.svc.Labels(f.ctx, f.orgID, outsider)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range list {
		if l.ID == hidden.ID {
			t.Error("метка закрытой доски видна тому, кому доска закрыта")
		}
	}
	// И заводить на ней он не может.
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, outsider,
		LabelDraft{Name: "Своя", BoardID: secret}); err == nil {
		t.Error("посторонний завёл метку на закрытой доске")
	}
}

// Метку подразделения заводят его люди; посторонний — нет, и отказ
// говорит, кто может.
func TestTeamLabelIsManagedByItsPeople(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	platform := f.team("Платформа", &dev)
	insider := f.inviteMember("Свой")
	outsider := f.inviteMember("Чужой")
	f.joins(insider, dev)

	// Права наследуются вниз: участник «Разработки» заводит и для
	// вложенной «Платформы».
	l, err := f.svc.CreateLabel(f.ctx, f.orgID, insider, LabelDraft{Name: "Релиз", TeamID: platform})
	if err != nil {
		t.Fatalf("участник подразделения не завёл метку: %v", err)
	}
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, outsider,
		LabelDraft{Name: "Чужое", TeamID: dev}); !errors.Is(err, ErrLabelNotYours) {
		t.Errorf("посторонний завёл метку подразделения, ошибка: %v", err)
	}
	if err := f.svc.ArchiveLabel(f.ctx, f.orgID, outsider, l.ID); !errors.Is(err, ErrLabelNotYours) {
		t.Errorf("посторонний убрал метку подразделения, ошибка: %v", err)
	}

	// Список заранее говорит, где можно: экран не предлагает кнопку,
	// которая откажет.
	list, err := f.svc.Labels(f.ctx, f.orgID, outsider)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if m.ID == l.ID && m.CanManage {
			t.Error("постороннему обещано управление чужой меткой")
		}
	}
	places, err := f.svc.LabelPlaces(f.ctx, f.orgID, insider, "")
	if err != nil {
		t.Fatal(err)
	}
	var teams []string
	for _, p := range places {
		if p.Scope == ScopeTeam {
			teams = append(teams, p.Name)
		}
	}
	if strings.Join(teams, ",") != "Разработка,Платформа" {
		t.Errorf("места участника «Разработки»: %v", teams)
	}
}

// Места для заведения с карточки — только те, чьи метки на этой доске
// действуют: метку соседнего подразделения сюда не повесить.
func TestLabelPlacesForABoardAreOnlyThoseThatApply(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	platform := f.team("Платформа", &dev)
	f.team("Продажи", nil)
	platformBoard := f.boardOfTeam("Платформа", &platform)
	f.boardOfTeam("Соседняя", &dev)

	places, err := f.svc.LabelPlaces(f.ctx, f.orgID, f.actorID, platformBoard)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range places {
		got = append(got, p.Scope+":"+p.Name)
	}
	want := "org:,team:Разработка,team:Платформа,board:Платформа"
	if strings.Join(got, ",") != want {
		t.Errorf("места для доски «Платформа»: %v, ждали %s", got, want)
	}
}

// Оттенок, если его не назвали, — наименее занятый: метку с карточки
// заводят на бегу, и пять одинаково серых меток выглядели бы ошибкой.
func TestLabelWithoutToneGetsTheLeastUsedOne(t *testing.T) {
	f := newFixture(t)
	seen := map[string]bool{}
	for i := range len(Tones) {
		l, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID,
			LabelDraft{Name: "Метка " + string(rune('А'+i))})
		if err != nil {
			t.Fatal(err)
		}
		if seen[l.Tone] {
			t.Errorf("оттенок %s выдан второй раз, пока были свободные: %v", l.Tone, seen)
		}
		seen[l.Tone] = true
	}
	// Названный оттенок остаётся названным.
	if l, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID,
		LabelDraft{Name: "Своя", Tone: "rose"}); err != nil || l.Tone != "rose" {
		t.Errorf("названный оттенок заменён: %v, %v", l.Tone, err)
	}
}

// Патч навешивания несёт описания меток: у соседа, у которого доска
// открыта, метки, заведённой только что, в словаре нет.
func TestLabelPatchCarriesTheDictionary(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Помечу", f.columnA)
	fresh := f.scopedLabel("Свежая", "", f.boardID)

	res := f.mustApply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": fresh.ID})
	if len(res.Patch.Labels) != 1 || res.Patch.Labels[0].ID != fresh.ID ||
		res.Patch.Labels[0].Name != "Свежая" || !res.Patch.Labels[0].Offered {
		t.Errorf("патч без описания метки: %+v", res.Patch.Labels)
	}
}

// Убранная, но действующая здесь метка есть в снимке: выбор метки
// предложит вернуть её, а не заводить вторую с тем же названием.
func TestArchivedLabelThatAppliesIsInTheSnapshot(t *testing.T) {
	f := newFixture(t)
	old := f.label("Ждём")
	if err := f.svc.ArchiveLabel(f.ctx, f.orgID, f.actorID, old.ID); err != nil {
		t.Fatal(err)
	}
	l := f.boardLabel(old.ID)
	if l == nil {
		t.Fatal("убранной метки нет в снимке — предложить вернуть её нечем")
	}
	if !l.Applies || l.Offered || !l.Archived {
		t.Errorf("убранная метка: applies=%v offered=%v archived=%v", l.Applies, l.Offered, l.Archived)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
