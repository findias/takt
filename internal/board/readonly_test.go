package board

import (
	"errors"
	"testing"
)

// Тому, кто доску видит, не отвечают «доска не найдена».
//
// Правило записано давно и починено для операций над карточками: «не
// найдено» вместо «нельзя» отправляет искать несуществующую поломку,
// и наблюдатель уходил проверять адрес доски, которая у него на экране.
// Смена видимости и обещание доски отвечали по-старому — при том что
// у обещания рядом стоял комментарий про разбор, которого в коде не
// было. Комментарий, обещающий больше, чем делает код, — тот же дефект,
// только в словах.
func TestVisibleBoardDoesNotAnswerNotFound(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	watcher := addMember(t, f.svc.db, f.orgID, "viewer")
	f.observes(watcher, &company)

	if !f.sees(watcher) {
		t.Fatal("наблюдатель не видит доску — проверять нечего")
	}

	days := 5
	cases := []struct {
		what string
		err  error
	}{
		{"смена видимости", f.svc.SetAccess(f.ctx, f.orgID, watcher, f.boardID, VisibilityOrg, nil)},
		{"обещание доски", f.svc.SetSLE(f.ctx, f.orgID, watcher, f.boardID, &days, 85)},
	}
	for _, c := range cases {
		if !errors.Is(c.err, ErrReadOnlyBoard) {
			t.Errorf("%s: %v, ожидалось «только для чтения»", c.what, c.err)
		}
	}

	// И наоборот: чужой доски не видно, и вот там «не найдена» — правда.
	// Область берётся своя, чужая: организация в неё приходит из сессии,
	// а не из запроса, и подставлять чужому человеку нашу организацию
	// значило бы проверять положение, которого не бывает.
	otherOrg, stranger := newTenant(t, f.svc.db)
	if err := f.svc.SetAccess(f.ctx, otherOrg, stranger, f.boardID, VisibilityOrg, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("посторонний: %v, ожидалось «не найдена»", err)
	}
}
