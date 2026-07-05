package repository

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
)

func testDB(t *testing.T) *sql.DB {
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
	return sqlDB
}

func seedProject(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	p, err := project.NewService(sqlDB).Create(context.Background(), project.CreateInput{Key: "p", Name: "P", Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p.ID
}

func mkRepo(t *testing.T, s *Service, projectID, key string, mode Mode) {
	t.Helper()
	in := CreateInput{ProjectID: projectID, Key: key, Name: key, Format: FormatOCI, Mode: mode, Actor: "admin"}
	if mode == ModeProxy {
		in.Upstream = &Upstream{URL: "https://registry-1.docker.io"}
	}
	if _, err := s.Create(context.Background(), in); err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
}

func TestCreateVirtualRepositoryOrdersMembers(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	projectID := seedProject(t, sqlDB)
	s := NewService(sqlDB)

	mkRepo(t, s, projectID, "local-a", ModeLocal)
	mkRepo(t, s, projectID, "proxy-hub", ModeProxy)

	v, err := s.Create(ctx, CreateInput{
		ProjectID: projectID, Key: "group", Name: "group", Format: FormatOCI, Mode: ModeVirtual,
		MemberKeys: []string{"local-a", "proxy-hub"}, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("create virtual: %v", err)
	}
	if len(v.MemberKeys) != 2 || v.MemberKeys[0] != "local-a" || v.MemberKeys[1] != "proxy-hub" {
		t.Errorf("MemberKeys = %v, want [local-a proxy-hub]", v.MemberKeys)
	}

	members, err := s.Members(ctx, v.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 2 || members[0].Key != "local-a" || members[1].Key != "proxy-hub" {
		t.Fatalf("members order wrong: %v", members)
	}
}

func TestVirtualRepositoryValidation(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	projectID := seedProject(t, sqlDB)
	s := NewService(sqlDB)
	mkRepo(t, s, projectID, "local-a", ModeLocal)
	// A virtual repo to prove nesting is rejected.
	if _, err := s.Create(ctx, CreateInput{
		ProjectID: projectID, Key: "inner", Name: "inner", Format: FormatOCI, Mode: ModeVirtual,
		MemberKeys: []string{"local-a"}, Actor: "admin",
	}); err != nil {
		t.Fatalf("seed inner virtual: %v", err)
	}

	cases := []struct {
		name    string
		members []string
		format  Format
		wantSub string
	}{
		{"no members", nil, FormatOCI, "at least one member"},
		{"missing member", []string{"nope"}, FormatOCI, "does not exist"},
		{"duplicate member", []string{"local-a", "local-a"}, FormatOCI, "duplicate member"},
		{"nested virtual", []string{"inner"}, FormatOCI, "another virtual"},
		{"self reference", []string{"group"}, FormatOCI, "cannot contain itself"},
		{"non-oci format", []string{"local-a"}, FormatNPM, "oci format only"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.Create(ctx, CreateInput{
				ProjectID: projectID, Key: "group", Name: "group", Format: c.format, Mode: ModeVirtual,
				MemberKeys: c.members, Actor: "admin",
			})
			if err == nil {
				t.Fatalf("expected an error for %s", c.name)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %v, want ValidationError", err)
			}
			if !strings.Contains(ve.Msg, c.wantSub) {
				t.Errorf("message = %q, want substring %q", ve.Msg, c.wantSub)
			}
		})
	}
}
