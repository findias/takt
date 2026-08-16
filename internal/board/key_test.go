package board

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDeriveKey(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Продукт", "ПРОД"},
		// Пробелы и знаки выпадают, а не превращаются в буквы ключа.
		{"Мобильное приложение", "МОБИ"},
		{"  найм  ", "НАЙМ"},
		{"R&D", "RD"},
		{"2026", "2026"},
		// Название короче двух знаков основы не даёт: подпирать её нечем.
		{"Я", "ДОСКА"},
		{"—", "ДОСКА"},
		{"", "ДОСКА"},
	}
	for _, c := range cases {
		if got := deriveKey(c.name); got != c.want {
			t.Errorf("deriveKey(%q) = %q, ожидалось %q", c.name, got, c.want)
		}
	}
}

func TestValidKey(t *testing.T) {
	valid := []string{"ПРО", "AB", "ПРОЕКТ", "А1", "X9Y9Z9"}
	for _, k := range valid {
		if !validKey(k) {
			t.Errorf("ключ %q забракован, а он годный", k)
		}
	}

	invalid := []string{
		"П",       // короче двух знаков
		"ПРОЕКТЫ", // длиннее шести
		"1ПРО",    // начинается с цифры: 1ПРО-7 читается как диапазон
		"ПРО-1",   // дефис отделяет номер и в ключе быть не может
		"ПРО КТ",  // пробел
		"",        //
		"ПРО́",    // комбинирующий знак — не буква и не цифра
	}
	for _, k := range invalid {
		if validKey(k) {
			t.Errorf("ключ %q принят, а он негодный", k)
		}
	}
}

// Ключ, выведенный из названия, разводится с уже занятым, а заданный
// руками — нет: занятый ключ это ошибка, а не повод молча выдать соседний.
func TestBoardKeyCollision(t *testing.T) {
	f := newFixture(t)

	// Фикстура завела доску «Доска» — ключ ДОСК.
	first, err := f.svc.List(f.ctx, f.orgID, f.actorID)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Key != "ДОСК" {
		t.Fatalf("ключ первой доски %q, ожидался ДОСК", first[0].Key)
	}

	second, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Доска", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Key != "ДОСК2" {
		t.Fatalf("ключ второй доски %q, ожидался ДОСК2", second.Key)
	}

	if _, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Третья", "ДОСК"); !errors.Is(err, ErrKeyTaken) {
		t.Fatalf("заданный занятый ключ прошёл: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Третья", "ПРО-1"); !errors.Is(err, ErrBadKey) {
		t.Fatalf("негодный ключ прошёл: %v", err)
	}

	// Регистр приводится: ключ живёт в верхнем, как его ни задай.
	lower, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Четвёртая", "про")
	if err != nil {
		t.Fatal(err)
	}
	if lower.Key != "ПРО" {
		t.Fatalf("ключ %q, ожидался ПРО", lower.Key)
	}
}

// Ключ разводится и с той доской, которой человеку не видно.
//
// Проверять занятость через политики нельзя: закрытая доска чужой
// команды невидима, её ключ выглядел бы свободным — и вместо ответа
// человек получал бы пятисотую ошибку от уникального индекса. Здесь
// участник, не вписанный в закрытую доску, заводит доску с тем же
// началом названия и должен получить следующий свободный ключ.
func TestBoardKeyAvoidsInvisibleBoard(t *testing.T) {
	f := newFixture(t)
	member := addMember(t, f.svc.db, f.orgID, "member")

	// Доска фикстуры называется «Доска» и держит ключ ДОСК. Закрываем её
	// от всех, кроме владельца: закрыть доску можно только вокруг себя,
	// поэтому владелец сперва вписывает в неё себя.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into board_members (org_id, board_id, user_id)
			values ($1, $2, $3)`, f.orgID, f.boardID, f.actorID)
		return err
	})
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID,
		VisibilityPrivate, nil); err != nil {
		t.Fatal(err)
	}
	if f.sees(member) {
		t.Fatal("закрытая доска осталась видна участнику — проверка бессмысленна")
	}

	b, err := f.svc.Create(f.ctx, f.orgID, member, "Доска", "")
	if err != nil {
		t.Fatalf("создание доски участником: %v", err)
	}
	if b.Key != "ДОСК2" {
		t.Fatalf("ключ %q, ожидался ДОСК2: невидимая доска держит ДОСК", b.Key)
	}
}

// Ключ доски уникален в организации, но не между организациями: у соседа
// может быть свой ПРО, и это не наше дело.
func TestBoardKeyIsPerOrg(t *testing.T) {
	f := newFixture(t)
	otherOrg, otherUser := newTenant(t, f.svc.db)

	b, err := f.svc.Create(f.ctx, otherOrg, otherUser, "Доска", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Key != "ДОСК" {
		t.Fatalf("ключ в соседней организации %q, ожидался ДОСК", b.Key)
	}
}
