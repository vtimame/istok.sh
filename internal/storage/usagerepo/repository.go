// Package usagerepo stores the device-local MCP call log in SQLite.
package usagerepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vtimame/istok.sh/internal/usage"
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }

// Record appends one call.
func (r *Repository) Record(ctx context.Context, call usage.Call) error {
	var projectID any
	if call.ProjectID != "" {
		projectID = call.ProjectID
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO mcp_calls(id,session_id,client_name,client_version,actor_id,project_id,tool,started_at,duration_ms,outcome,error_code) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		call.ID, call.SessionID, call.ClientName, call.ClientVersion, call.ActorID, projectID, call.Tool,
		stamp(call.StartedAt), call.Duration.Milliseconds(), string(call.Outcome), call.ErrorCode,
	)
	if err != nil {
		return fmt.Errorf("record MCP call: %w", err)
	}

	return nil
}

// Prune deletes calls that started before the cutoff.
func (r *Repository) Prune(ctx context.Context, before time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM mcp_calls WHERE started_at < ?`, stamp(before)); err != nil {
		return fmt.Errorf("prune MCP call log: %w", err)
	}

	return nil
}

// Counts returns call counts grouped by session, tool and outcome.
func (r *Repository) Counts(ctx context.Context, filter usage.Filter) ([]usage.ToolCount, error) {
	query := `SELECT session_id,MAX(client_name),tool,outcome,error_code,COUNT(*) FROM mcp_calls WHERE started_at >= ?`
	args := []any{stamp(filter.Since)}

	if filter.ProjectID != "" {
		query += ` AND project_id = ?`
		args = append(args, filter.ProjectID)
	}
	query += ` GROUP BY session_id,tool,outcome,error_code`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read MCP call log: %w", err)
	}
	defer rows.Close()

	counts := []usage.ToolCount{}
	for rows.Next() {
		var value usage.ToolCount
		var outcome string

		if err := rows.Scan(&value.SessionID, &value.ClientName, &value.Tool, &outcome, &value.ErrorCode, &value.Calls); err != nil {
			return nil, fmt.Errorf("scan MCP call log: %w", err)
		}
		value.Outcome = usage.Outcome(outcome)
		counts = append(counts, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read MCP call log: %w", err)
	}

	return counts, nil
}

// stampLayout has a fixed width, so stamps compare correctly as strings.
const stampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func stamp(value time.Time) string { return value.UTC().Format(stampLayout) }
