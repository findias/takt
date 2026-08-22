package org

import (
	"errors"
	"testing"
)

// Действия над человеком не берут человека из чужой организации.
//
// Здесь это держится тем, что каждый запрос ограничен парой
// (org_id, user_id): чужой в неё не попадает, и строк меняется ноль.
// Проверяется не «ноль строк», а отказ: молчаливый успех на нулевой
// правке выглядел бы как сделанное дело.
//
// Смысл проверки в том, что `users`, `memberships`, `orgs` и `sessions`
// под RLS не попадают намеренно, и границу у них держит код. Значит,
// эта граница обязана быть проверена так же, как политики — прогоном,
// а не чтением.
func TestStrangerFromAnotherOrgIsRefused(t *testing.T) {
	f := newFixture(t)
	mine, myOwner := f.org("Своя")
	_, stranger := f.org("Соседи")

	if err := f.svc.SetRole(f.ctx, mine.OrgID, myOwner, stranger, "member"); !errors.Is(err, ErrNotFound) {
		t.Errorf("смена роли постороннему: %v, ожидалось %v", err, ErrNotFound)
	}
	if err := f.svc.Remove(f.ctx, mine.OrgID, myOwner, stranger); !errors.Is(err, ErrNotFound) {
		t.Errorf("исключение постороннего: %v, ожидалось %v", err, ErrNotFound)
	}
	if err := f.svc.Erase(f.ctx, mine.OrgID, myOwner, stranger); !errors.Is(err, ErrNotFound) {
		t.Errorf("обезличивание постороннего: %v, ожидалось %v", err, ErrNotFound)
	}

	// Обезличивание необратимо, поэтому отдельно: чужая личность цела.
	// Ошибка здесь стирает человека в соседней организации навсегда.
	var name string
	var anonymized *string
	err := f.db.Pool.QueryRow(f.ctx,
		`select name, anonymized_at::text from users where id = $1`, stranger).Scan(&name, &anonymized)
	if err != nil {
		t.Fatalf("чтение чужой личности: %v", err)
	}
	if anonymized != nil {
		t.Errorf("чужая личность обезличена: %v", *anonymized)
	}
	if name != "Владелец" {
		t.Errorf("чужое имя изменилось: %q", name)
	}
}
