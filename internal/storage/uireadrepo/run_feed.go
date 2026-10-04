package uireadrepo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Lease filters active runs by whether their agent still heartbeats.
type Lease string

const (
	LeaseAny     Lease = ""
	LeaseLive    Lease = "live"
	LeaseExpired Lease = "expired"
)

// FeedOptions filters the run feed; an empty ProjectID covers all projects.
// A Lease other than LeaseAny restricts the feed to active runs.
type FeedOptions struct {
	ProjectID string
	Statuses  []string
	Lease     Lease
	Limit     int
	Offset    int
}

type FeedRun struct {
	ID            string     `json:"id"`
	Revision      int64      `json:"revision"`
	Status        string     `json:"status"`
	ActorName     string     `json:"actor_name"`
	ActorKind     string     `json:"actor_kind"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	HeartbeatAt   *time.Time `json:"heartbeat_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	ResultSummary string     `json:"result_summary"`
}

type FeedTask struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type FeedProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FeedValidations struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// FeedItem is one run with the task, project and validation outcome needed to
// render it without further requests.
type FeedItem struct {
	Run         FeedRun         `json:"run"`
	Task        FeedTask        `json:"task"`
	Project     FeedProject     `json:"project"`
	Validations FeedValidations `json:"validations"`
}

const feedQuery = `
SELECT r.id, r.revision, r.status, r.actor_name, r.actor_kind, r.started_at, r.finished_at, r.result_summary,
       l.heartbeat_at, l.expires_at,
       t.id, t.number, t.title, t.status,
       p.id, p.name,
       (SELECT COUNT(*) FROM validations v JOIN executions e ON e.id = v.execution_id
         WHERE e.run_id = r.id AND v.status = 'passed'),
       (SELECT COUNT(*) FROM validations v JOIN executions e ON e.id = v.execution_id
         WHERE e.run_id = r.id AND v.status = 'failed')
FROM runs r
JOIN tasks t ON t.id = r.task_id
JOIN projects p ON p.id = t.project_id
LEFT JOIN run_leases l ON l.run_id = r.id
WHERE t.deleted_at IS NULL AND p.deleted_at IS NULL`

// RunFeed lists runs newest first across projects.
func (r *Repository) RunFeed(ctx context.Context, options FeedOptions) ([]FeedItem, error) {
	query := feedQuery
	args := make([]any, 0, len(options.Statuses)+3)

	if options.ProjectID != "" {
		query += " AND t.project_id = ?"
		args = append(args, options.ProjectID)
	}
	if len(options.Statuses) > 0 {
		query += " AND r.status IN (" + strings.TrimSuffix(strings.Repeat("?,", len(options.Statuses)), ",") + ")"
		for _, status := range options.Statuses {
			args = append(args, status)
		}
	}
	// Stored times are RFC3339Nano UTC, so a string comparison with the same
	// format orders correctly except within a single second.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	switch options.Lease {
	case LeaseLive:
		query += " AND r.status = 'active' AND l.expires_at > ?"
		args = append(args, now)
	case LeaseExpired:
		query += " AND r.status = 'active' AND (l.expires_at IS NULL OR l.expires_at <= ?)"
		args = append(args, now)
	}

	query += " ORDER BY r.started_at DESC, r.id DESC LIMIT ? OFFSET ?"
	args = append(args, options.Limit, options.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query run feed: %w", err)
	}
	defer rows.Close()

	items := make([]FeedItem, 0)
	for rows.Next() {
		item, err := scanFeedItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func scanFeedItem(rows *sql.Rows) (FeedItem, error) {
	var (
		item                         FeedItem
		started                      string
		finished, heartbeat, expires sql.NullString
	)

	err := rows.Scan(
		&item.Run.ID, &item.Run.Revision, &item.Run.Status, &item.Run.ActorName, &item.Run.ActorKind, &started, &finished, &item.Run.ResultSummary,
		&heartbeat, &expires,
		&item.Task.ID, &item.Task.Number, &item.Task.Title, &item.Task.Status,
		&item.Project.ID, &item.Project.Name,
		&item.Validations.Passed, &item.Validations.Failed,
	)
	if err != nil {
		return FeedItem{}, fmt.Errorf("scan run feed: %w", err)
	}

	if item.Run.StartedAt, err = parseTime(started); err != nil {
		return FeedItem{}, err
	}
	if item.Run.FinishedAt, err = parseOptionalTime(finished); err != nil {
		return FeedItem{}, err
	}
	if item.Run.HeartbeatAt, err = parseOptionalTime(heartbeat); err != nil {
		return FeedItem{}, err
	}
	if item.Run.ExpiresAt, err = parseOptionalTime(expires); err != nil {
		return FeedItem{}, err
	}

	return item, nil
}
