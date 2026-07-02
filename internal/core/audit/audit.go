// Package audit reads the audit log. Writes happen transactionally inside the
// domain services and adapters that perform a mutation (so the record and the
// change commit together); this package is the read side that surfaces that
// history — starting with the dashboard's recent-activity feed.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Entry is one recorded mutation, joined to the project it touched (project
// fields are empty for instance-level events).
type Entry struct {
	Actor       string
	Action      string
	TargetType  string
	TargetID    string
	Metadata    map[string]string
	ProjectKey  string
	ProjectName string
	CreatedAt   time.Time
}

// Service reads audit history.
type Service struct {
	q *db.Queries
}

// NewService wires the audit reader to an open database.
func NewService(sqlDB *sql.DB) *Service { return &Service{q: db.New(sqlDB)} }

// Recent returns the most recent audit entries across all projects, newest
// first, capped at limit.
func (s *Service) Recent(ctx context.Context, limit int) ([]Entry, error) {
	rows, err := s.q.ListRecentActivity(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("listing recent activity: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, Entry{
			Actor:       r.Actor,
			Action:      r.Action,
			TargetType:  r.TargetType,
			TargetID:    r.TargetID,
			Metadata:    parseMetadata(r.Metadata),
			ProjectKey:  r.ProjectKey.String,
			ProjectName: r.ProjectName.String,
			CreatedAt:   parseTime(r.CreatedAt),
		})
	}
	return entries, nil
}

// parseMetadata decodes the stored JSON object into a string map. Audit metadata
// is always a flat string map; anything unexpected degrades to empty.
func parseMetadata(raw string) map[string]string {
	if raw == "" || raw == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
