package org

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/team"
)

// Владелец подразделения приглашает в своё поддерево (этап 31, 0072).
//
// Право держит политика, а не обработчик, поэтому и проверяется
// сервис, под которым нет никакой проверки роли: всё, что здесь
// отказано, отказано базой.

type subdivision struct {
	*fixture
	orgID, ownerID string
	boris, vera    string // владелец «Разработки» и рядовой участник
	dev, platform  team.Team
	sales, empty   team.Team
}

// joined приглашает человека владельцем организации и принимает
// приглашение — так человек попадает в организацию в жизни.
func (f *fixture) joined(orgID, ownerID, role string) string {
	f.t.Helper()
	id, email := f.user("Сотрудник")
	inv, err := f.svc.Invite(f.ctx, orgID, ownerID, email, role, "", "http://example.test")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.svc.Accept(f.ctx, token(inv.Link), id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func newSubdivision(t *testing.T) *subdivision {
	f := newFixture(t)
	org, ownerID := f.org("Компания")
	s := &subdivision{fixture: f, orgID: org.OrgID, ownerID: ownerID}
	s.boris = f.joined(org.OrgID, ownerID, auth.RoleMember)
	s.vera = f.joined(org.OrgID, ownerID, auth.RoleMember)

	teams := team.New(f.db)
	mk := func(name string, parent *string) team.Team {
		tm, err := teams.Create(f.ctx, org.OrgID, ownerID, name, parent)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	s.dev = mk("Разработка", nil)
	s.platform = mk("Платформа", &s.dev.ID)
	s.sales = mk("Продажи", nil)
	s.empty = mk("Бывшая", &s.dev.ID)
	if _, err := teams.GrantAdmin(f.ctx, org.OrgID, ownerID, s.boris, s.dev.ID); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *subdivision) invite(by, teamID, role string) (Invite, error) {
	_, email := s.user("Гость")
	return s.svc.Invite(s.ctx, s.orgID, by, email, role, teamID, "http://example.test")
}

func TestSubdivisionOwnerInvitesOnlyIntoOwnSubtree(t *testing.T) {
	s := newSubdivision(t)

	inv, err := s.invite(s.boris, s.platform.ID, auth.RoleMember)
	if err != nil {
		t.Fatalf("владелец «Разработки» не пригласил во вложенную «Платформу»: %v", err)
	}
	if inv.TeamName == nil || *inv.TeamName != "Платформа" {
		t.Errorf("приглашение не называет узел: %+v", inv)
	}
	if _, err := s.invite(s.boris, s.dev.ID, auth.RoleViewer); err != nil {
		t.Errorf("наблюдателем в свой узел: %v", err)
	}

	refused := []struct {
		what, by, team, role string
	}{
		{"в соседнее подразделение", s.boris, s.sales.ID, auth.RoleMember},
		{"в организацию без узла", s.boris, "", auth.RoleMember},
		{"владельцем организации", s.boris, s.platform.ID, auth.RoleOwner},
		{"рядовым участником", s.vera, s.dev.ID, auth.RoleMember},
	}
	for _, c := range refused {
		if _, err := s.invite(c.by, c.team, c.role); !errors.Is(err, ErrInviteNotYours) {
			t.Errorf("%s: ждали ErrInviteNotYours, получили %v", c.what, err)
		}
	}

	// Владелец организации — куда угодно и кем угодно.
	if _, err := s.invite(s.ownerID, s.sales.ID, auth.RoleOwner); err != nil {
		t.Errorf("владелец организации: %v", err)
	}
}

func TestInvitesAreSeenAndRevokedWithinTheSubtree(t *testing.T) {
	s := newSubdivision(t)
	mine, err := s.invite(s.boris, s.platform.ID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.invite(s.ownerID, s.sales.ID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	ids := func(user string) map[string]bool {
		list, err := s.svc.PendingInvites(s.ctx, s.orgID, user)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, i := range list {
			out[i.ID] = true
		}
		return out
	}
	if got := ids(s.boris); !got[mine.ID] || got[theirs.ID] {
		t.Errorf("владелец подразделения видит %v: ждали только своё", got)
	}
	if got := ids(s.vera); len(got) != 0 {
		t.Errorf("рядовой участник видит приглашения: %v", got)
	}
	if got := ids(s.ownerID); !got[mine.ID] || !got[theirs.ID] {
		t.Errorf("владелец организации видит не все: %v", got)
	}

	if err := s.svc.RevokeInvite(s.ctx, s.orgID, s.boris, theirs.ID); !errors.Is(err, ErrRevokeNotYours) {
		t.Errorf("чужое приглашение: ждали ErrRevokeNotYours, получили %v", err)
	}
	if err := s.svc.RevokeInvite(s.ctx, s.orgID, s.boris, mine.ID); err != nil {
		t.Errorf("своё приглашение не отозвано: %v", err)
	}
	// Своё, но уже отозванное — «не найдено», а не «не вправе».
	if err := s.svc.RevokeInvite(s.ctx, s.orgID, s.boris, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторный отзыв своего: ждали ErrNotFound, получили %v", err)
	}
}

func TestAcceptedInviteJoinsTheSubdivision(t *testing.T) {
	s := newSubdivision(t)
	guestID, email := s.user("Гость")
	inv, err := s.svc.Invite(s.ctx, s.orgID, s.boris, email, auth.RoleMember, s.platform.ID, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.svc.Accept(s.ctx, token(inv.Link), guestID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Role != auth.RoleMember {
		t.Errorf("роль в организации %q", m.Role)
	}
	var inTeam bool
	s.as(s.orgID, s.ownerID, func(tx pgx.Tx) error {
		return tx.QueryRow(s.ctx, `
			select exists (select 1 from team_members where team_id = $1 and user_id = $2)`,
			s.platform.ID, guestID).Scan(&inTeam)
	})
	if !inTeam {
		t.Error("принявший не вошёл в узел приглашения")
	}
}

func TestInviteIntoArchivedNodeIsExplained(t *testing.T) {
	s := newSubdivision(t)
	teams := team.New(s.db)

	// Убран после приглашения: в организацию человек входит, в узел — нет.
	guestID, email := s.user("Гость")
	inv, err := s.svc.Invite(s.ctx, s.orgID, s.ownerID, email, auth.RoleMember, s.empty.ID, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.Archive(s.ctx, s.orgID, s.ownerID, s.empty.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.svc.Accept(s.ctx, token(inv.Link), guestID); err != nil {
		t.Errorf("приглашение в убранный узел не принялось: %v", err)
	}

	// Убран до приглашения — отказ называет причину, а не права.
	if _, err := s.invite(s.ownerID, s.empty.ID, auth.RoleMember); !errors.Is(err, ErrInviteTeamGone) {
		t.Errorf("ждали ErrInviteTeamGone, получили %v", err)
	}
}
