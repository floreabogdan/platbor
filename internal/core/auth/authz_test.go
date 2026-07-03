package auth_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
)

// newAuthWithProject returns an auth service and a real project id (membership
// rows reference projects by FK, so the project must exist).
func newAuthWithProject(t *testing.T) (*auth.Service, string) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(ctx, sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "proj", Name: "Proj", Actor: "system"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return auth.NewService(sqlDB), proj.ID
}

func TestAuthorizeInstanceAdminBypassesProjectRoles(t *testing.T) {
	svc, projectID := newAuthWithProject(t)
	ctx := context.Background()
	admin, err := svc.CreateUser(ctx, "root", "", "pw", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// No membership row at all, yet every action is allowed.
	for _, a := range []auth.Action{auth.ActionRead, auth.ActionWrite, auth.ActionManage} {
		if err := svc.Authorize(ctx, admin, projectID, a); err != nil {
			t.Errorf("instance admin denied action %d: %v", a, err)
		}
	}
}

func TestAuthorizeRolesGrantExpectedActions(t *testing.T) {
	svc, projectID := newAuthWithProject(t)
	ctx := context.Background()
	user, err := svc.CreateUser(ctx, "dev", "", "pw", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cases := []struct {
		role                auth.Role
		read, write, manage bool
	}{
		{auth.RoleReader, true, false, false},
		{auth.RoleMaintainer, true, true, false},
		{auth.RoleAdmin, true, true, true},
	}
	for _, c := range cases {
		if err := svc.SetMember(ctx, projectID, user.ID, c.role); err != nil {
			t.Fatalf("SetMember %s: %v", c.role, err)
		}
		got := func(a auth.Action) bool { return svc.Authorize(ctx, user, projectID, a) == nil }
		if got(auth.ActionRead) != c.read || got(auth.ActionWrite) != c.write || got(auth.ActionManage) != c.manage {
			t.Errorf("role %s: read=%v write=%v manage=%v, want %v/%v/%v",
				c.role, got(auth.ActionRead), got(auth.ActionWrite), got(auth.ActionManage), c.read, c.write, c.manage)
		}
	}
}

func TestAuthorizeNonMemberIsForbidden(t *testing.T) {
	svc, projectID := newAuthWithProject(t)
	ctx := context.Background()
	user, err := svc.CreateUser(ctx, "stranger", "", "pw", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := svc.Authorize(ctx, user, projectID, auth.ActionRead); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("non-member read: got %v, want ErrForbidden", err)
	}
}

func TestMembersLifecycle(t *testing.T) {
	svc, projectID := newAuthWithProject(t)
	ctx := context.Background()
	u, err := svc.CreateUser(ctx, "alice", "alice@example.com", "pw", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := svc.SetMember(ctx, projectID, u.ID, auth.RoleReader); err != nil {
		t.Fatalf("SetMember: %v", err)
	}
	// Upsert changes the role in place.
	if err := svc.SetMember(ctx, projectID, u.ID, auth.RoleMaintainer); err != nil {
		t.Fatalf("SetMember update: %v", err)
	}
	members, err := svc.ListMembers(ctx, projectID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].Username != "alice" || members[0].Role != auth.RoleMaintainer {
		t.Fatalf("members = %+v, want one maintainer alice", members)
	}

	removed, err := svc.RemoveMember(ctx, projectID, u.ID)
	if err != nil || !removed {
		t.Fatalf("RemoveMember: removed=%v err=%v", removed, err)
	}
	if removed, _ := svc.RemoveMember(ctx, projectID, u.ID); removed {
		t.Error("removing a non-member should report removed=false")
	}

	if _, err := svc.UserByUsername(ctx, "nobody"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("UserByUsername(nobody): got %v, want ErrUserNotFound", err)
	}
}

func TestCreateUserRejectsDuplicate(t *testing.T) {
	svc, _ := newAuthWithProject(t)
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, "dupe", "", "pw", false); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	if _, err := svc.CreateUser(ctx, "dupe", "", "pw", false); !errors.Is(err, auth.ErrDuplicateUser) {
		t.Errorf("duplicate CreateUser: got %v, want ErrDuplicateUser", err)
	}
}
