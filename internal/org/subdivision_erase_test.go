package org

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/team"
)

// Владелец подразделения стирает данные того, кто весь внутри его
// поддерева (этап 31, app_can_erase, 0074).

func (s *subdivision) inTeams(userID string, teamIDs ...string) {
	s.t.Helper()
	teams := team.New(s.db)
	for _, id := range teamIDs {
		if err := teams.AddMember(s.ctx, s.orgID, s.ownerID, id, userID); err != nil {
			s.t.Fatal(err)
		}
	}
}

func TestSubdivisionOwnerErasesOnlyWhoIsWhollyInside(t *testing.T) {
	s := newSubdivision(t)

	inside := s.joined(s.orgID, s.ownerID, auth.RoleMember)
	s.inTeams(inside, s.platform.ID)
	straddles := s.joined(s.orgID, s.ownerID, auth.RoleMember)
	s.inTeams(straddles, s.platform.ID, s.sales.ID)
	nowhere := s.joined(s.orgID, s.ownerID, auth.RoleMember)
	owner := s.joined(s.orgID, s.ownerID, auth.RoleOwner)
	s.inTeams(owner, s.platform.ID)
	// Второй владелец «Разработки» — ровня Борису, не подчинённый.
	peer := s.joined(s.orgID, s.ownerID, auth.RoleMember)
	s.inTeams(peer, s.platform.ID)
	if _, err := team.New(s.db).GrantAdmin(s.ctx, s.orgID, s.ownerID, peer, s.dev.ID); err != nil {
		t.Fatal(err)
	}

	refused := map[string]string{
		"состоит и в соседнем подразделении": straddles,
		"ни в одном подразделении":           nowhere,
		"владелец организации":               owner,
		"владелец того же узла":              peer,
		"сам себя": s.boris,
	}
	for what, who := range refused {
		if err := s.svc.Erase(s.ctx, s.orgID, s.boris, who); !errors.Is(err, ErrEraseNotYours) {
			t.Errorf("%s: ждали ErrEraseNotYours, получили %v", what, err)
		}
	}
	if err := s.svc.Erase(s.ctx, s.orgID, s.vera, inside); !errors.Is(err, ErrEraseNotYours) {
		t.Errorf("рядовой участник стёр человека: %v", err)
	}

	// Список «кого можно стереть» говорит то же самое.
	may, err := s.svc.ErasableBy(s.ctx, s.orgID, s.boris)
	if err != nil {
		t.Fatal(err)
	}
	if !may[inside] || may[straddles] || may[nowhere] || may[owner] || may[peer] {
		t.Errorf("ErasableBy расходится с правом: %v", may)
	}

	if err := s.svc.Erase(s.ctx, s.orgID, s.boris, inside); err != nil {
		t.Fatalf("владелец подразделения не стёр своего: %v", err)
	}

	// Приглашение, по которому человек пришёл, было без подразделения —
	// вне области Бориса, — и всё равно стёрто: почта не остаётся нигде.
	var left int
	s.as(s.orgID, s.ownerID, func(tx pgx.Tx) error {
		return tx.QueryRow(s.ctx,
			`select count(*) from invites where accepted_by = $1`, inside).Scan(&left)
	})
	if left != 0 {
		t.Errorf("после стирания осталось приглашений: %d", left)
	}
}
