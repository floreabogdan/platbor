package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// This file adds authorization on top of authentication: per-project roles and
// the permission check every adapter and management handler consults. Instance
// admins bypass project roles; everyone else gets exactly what their role in a
// project permits. Authorization decisions live here so no adapter re-implements
// them (mirroring how authentication is centralized in auth.go).

// Role is a user's role within a project, in increasing order of privilege.
type Role string

const (
	RoleReader     Role = "reader"     // pull/read artifacts
	RoleMaintainer Role = "maintainer" // reader + push/write artifacts
	RoleAdmin      Role = "admin"      // maintainer + configure the project
)

// ValidRole reports whether r is a known role.
func ValidRole(r Role) bool {
	switch r {
	case RoleReader, RoleMaintainer, RoleAdmin:
		return true
	}
	return false
}

// Action is the permission an operation requires.
type Action int

const (
	ActionRead   Action = iota // pull/read an artifact or metadata
	ActionWrite                // push/write or delete an artifact
	ActionManage               // configure the project: repositories, members
)

var (
	// ErrForbidden means the user is authenticated but lacks the required role.
	ErrForbidden = errors.New("forbidden")
	// ErrUserNotFound means no account matched (e.g. adding a member by username).
	ErrUserNotFound = errors.New("user not found")
)

// roleAllows reports whether a role satisfies an action. Roles are cumulative:
// maintainer includes reader, admin includes maintainer.
func roleAllows(role Role, action Action) bool {
	switch action {
	case ActionRead:
		return role == RoleReader || role == RoleMaintainer || role == RoleAdmin
	case ActionWrite:
		return role == RoleMaintainer || role == RoleAdmin
	case ActionManage:
		return role == RoleAdmin
	}
	return false
}

// Authorize returns nil when user may perform action on the project, or
// ErrForbidden. Instance admins bypass project roles entirely.
func (s *Service) Authorize(ctx context.Context, user User, projectID string, action Action) error {
	if user.IsAdmin {
		return nil
	}
	role, err := s.MemberRole(ctx, projectID, user.ID)
	if err != nil {
		return err // ErrForbidden when not a member
	}
	if !roleAllows(role, action) {
		return ErrForbidden
	}
	return nil
}

// MemberRole returns the user's role in a project, or ErrForbidden if the user
// is not a member.
func (s *Service) MemberRole(ctx context.Context, projectID, userID string) (Role, error) {
	role, err := s.q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{ProjectID: projectID, UserID: userID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrForbidden
		}
		return "", fmt.Errorf("looking up membership: %w", err)
	}
	return Role(role), nil
}

// Member is a project member with their account details.
type Member struct {
	UserID    string
	Username  string
	Email     string
	Role      Role
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SetMember grants a user a role in a project, or updates their existing role.
func (s *Service) SetMember(ctx context.Context, projectID, userID string, role Role) error {
	if !ValidRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}
	ts := s.now().Format(time.RFC3339Nano)
	if err := s.q.UpsertProjectMember(ctx, db.UpsertProjectMemberParams{
		ProjectID: projectID, UserID: userID, Role: string(role), CreatedAt: ts, UpdatedAt: ts,
	}); err != nil {
		return fmt.Errorf("setting member: %w", err)
	}
	return nil
}

// ListMembers returns a project's members, ordered by username.
func (s *Service) ListMembers(ctx context.Context, projectID string) ([]Member, error) {
	rows, err := s.q.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing members: %w", err)
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		created, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		updated, _ := time.Parse(time.RFC3339Nano, r.UpdatedAt)
		out = append(out, Member{
			UserID: r.UserID, Username: r.Username, Email: r.Email,
			Role: Role(r.Role), CreatedAt: created, UpdatedAt: updated,
		})
	}
	return out, nil
}

// RemoveMember revokes a user's membership. It reports whether a row was removed
// (false means the user was not a member).
func (s *Service) RemoveMember(ctx context.Context, projectID, userID string) (bool, error) {
	n, err := s.q.DeleteProjectMember(ctx, db.DeleteProjectMemberParams{ProjectID: projectID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("removing member: %w", err)
	}
	return n > 0, nil
}

// UserByUsername resolves a username to its account, for member management.
func (s *Service) UserByUsername(ctx context.Context, username string) (User, error) {
	row, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("looking up user: %w", err)
	}
	return toUser(row), nil
}
