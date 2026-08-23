package version_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// У версии обязана быть запись о том, что в ней изменилось.
//
// «Что изменилось между двумя установками» не было перечислено нигде:
// ROADMAP пишется для нас и рассказывает о решениях, а заказчику нужен
// список того, что менять при обновлении. Проверка следит за самым
// дешёвым и самым забываемым: тег выпущен, а строки о нём нет.
func TestEveryTagHasAChangelogEntry(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git не найден — теги не проверяются")
	}
	out, err := exec.Command("git", "tag", "--list", "v*").Output()
	if err != nil {
		t.Skipf("git не отвечает: %v", err)
	}
	теги := strings.Fields(string(out))
	if len(теги) == 0 {
		t.Skip("тегов ещё нет — проверять нечего")
	}

	changelog, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("списка изменений нет вовсе: %v", err)
	}
	текст := string(changelog)
	for _, тег := range теги {
		if !strings.Contains(текст, "## "+тег+" ") && !strings.HasSuffix(текст, "## "+тег) {
			t.Errorf("тег %s выпущен, а записи о нём в CHANGELOG.md нет: "+
				"заказчику нечем ответить, что изменилось", тег)
		}
	}
}

// Заголовок записи — «## vX.Y.Z — дата». Формат проверяется потому,
// что по нему список читают глазами и ищут свою версию: запись,
// названная иначе, теряется в середине файла.
func TestChangelogEntriesAreNamedTheSameWay(t *testing.T) {
	changelog, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	заголовок := regexp.MustCompile(`^## (.+)$`)
	верный := regexp.MustCompile(`^v\d+\.\d+\.\d+ — \d{1,2} \S+ \d{4}$`)
	нашлось := 0
	for _, строка := range strings.Split(string(changelog), "\n") {
		m := заголовок.FindStringSubmatch(строка)
		if m == nil {
			continue
		}
		нашлось++
		if !верный.MatchString(m[1]) {
			t.Errorf("запись названа %q, а ожидается «vX.Y.Z — 1 января 2026»", m[1])
		}
	}
	if нашлось == 0 {
		t.Error("в списке изменений нет ни одной записи о версии")
	}
}
