package exchangerepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vtimame/istok.sh/internal/exchange"
)

// insertTask inserts a task that is new to this device. Task numbers are
// unique per project, and two devices can hand out the same number while
// apart: the task created later gets the next free number and a history event.
func (s *importer) insertTask(ctx context.Context, names []string, row exchange.Row) error {
	tasks := table{name: "tasks", key: []string{"id"}}
	projectID := normalize(row["project_id"])
	number := normalize(row["number"])

	var localID, localCreated, localTitle string
	err := s.conn.QueryRowContext(ctx, "SELECT id, created_at, title FROM tasks WHERE project_id = ? AND number = ?",
		projectID, number).Scan(&localID, &localCreated, &localTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return s.insert(ctx, tasks, names, row)
	}
	if err != nil {
		return err
	}

	from, ok := number.(int64)
	if !ok {
		return exchange.Errorf(exchange.CodeInvalid, "task %v has no integer number", row["id"])
	}
	to, err := s.nextNumber(ctx, projectID)
	if err != nil {
		return err
	}

	incomingID := fmt.Sprint(normalize(row["id"]))
	if createdLater(fmt.Sprint(row["created_at"]), incomingID, localCreated, localID) {
		row["number"] = to
		if err := s.insert(ctx, tasks, names, row); err != nil {
			return err
		}
		return s.recordRenumbering(ctx, fmt.Sprint(projectID), incomingID, fmt.Sprint(row["title"]), from, to)
	}

	if _, err := s.conn.ExecContext(ctx, "UPDATE tasks SET number = ? WHERE id = ?", to, localID); err != nil {
		return fmt.Errorf("renumber local task %s: %w", localID, err)
	}
	if err := s.insert(ctx, tasks, names, row); err != nil {
		return err
	}

	return s.recordRenumbering(ctx, fmt.Sprint(projectID), localID, localTitle, from, to)
}

// createdLater orders tasks by creation time, then by ID for equal times.
func createdLater(leftCreated, leftID, rightCreated, rightID string) bool {
	left, leftErr := time.Parse(time.RFC3339Nano, leftCreated)
	right, rightErr := time.Parse(time.RFC3339Nano, rightCreated)
	if leftErr == nil && rightErr == nil && !left.Equal(right) {
		return left.After(right)
	}

	return leftID > rightID
}

func (s *importer) nextNumber(ctx context.Context, projectID any) (int64, error) {
	var highest, counter sql.NullInt64
	if err := s.conn.QueryRowContext(ctx, "SELECT MAX(number) FROM tasks WHERE project_id = ?", projectID).Scan(&highest); err != nil {
		return 0, err
	}
	err := s.conn.QueryRowContext(ctx, "SELECT next_number FROM project_task_counters WHERE project_id = ?", projectID).Scan(&counter)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	return max(highest.Int64+1, counter.Int64, 1), nil
}

func (s *importer) recordRenumbering(ctx context.Context, projectID, taskID, title string, from, to int64) error {
	var revision int64
	if err := s.conn.QueryRowContext(ctx, "SELECT revision FROM tasks WHERE id = ?", taskID).Scan(&revision); err != nil {
		return err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return err
	}

	body := fmt.Sprintf("Renumbered from #%d to #%d: another task already had #%d on this device.", from, to, from)
	_, err = s.conn.ExecContext(ctx,
		"INSERT INTO task_events (id, task_id, type, body, task_revision, actor_id, actor_kind, actor_name, created_at) VALUES (?, ?, 'renumbered', ?, ?, ?, ?, ?, ?)",
		id.String(), taskID, body, revision, s.actor.ID, s.actor.Kind, s.actor.Name, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record renumbering of %s: %w", taskID, err)
	}

	s.report.Renumbered = append(s.report.Renumbered, exchange.Renumbering{
		ProjectID: projectID,
		TaskID:    taskID,
		Title:     title,
		From:      from,
		To:        to,
	})

	return nil
}

// finish restores derived state and checks references before commit.
func (s *importer) finish(ctx context.Context, projectIDs []string) error {
	in := placeholders(len(projectIDs))
	args := make([]any, len(projectIDs))
	for index, id := range projectIDs {
		args[index] = id
	}

	// Records may point at local-only records that never left the other
	// device.
	for _, name := range []string{"context_records", "knowledge_items"} {
		query := fmt.Sprintf("UPDATE %s SET superseded_by = NULL WHERE project_id IN (%s) AND superseded_by IS NOT NULL "+
			"AND superseded_by NOT IN (SELECT id FROM %s)", name, in, name)
		if _, err := s.conn.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("clear dangling references in %s: %w", name, err)
		}
	}

	query := fmt.Sprintf("INSERT INTO project_task_counters (project_id, next_number) "+
		"SELECT p.id, COALESCE((SELECT MAX(number) FROM tasks t WHERE t.project_id = p.id), 0) + 1 FROM projects p WHERE p.id IN (%s) "+
		"ON CONFLICT(project_id) DO UPDATE SET next_number = MAX(next_number, excluded.next_number)", in)
	if _, err := s.conn.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update task counters: %w", err)
	}

	rows, err := s.conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var tableName, parentTable string
		var rowID sql.NullInt64
		var index int
		if err := rows.Scan(&tableName, &rowID, &parentTable, &index); err != nil {
			return err
		}
		problems = append(problems, fmt.Sprintf("%s row %d references a missing %s", tableName, rowID.Int64, parentTable))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return exchange.Errorf(exchange.CodeInvalid, "bundle references rows it does not contain: %s", strings.Join(problems, "; "))
	}

	return nil
}

// describeProjects reports each project and warns about what needs a person.
func (s *importer) describeProjects(ctx context.Context, summaries []exchange.ProjectSummary) error {
	for _, summary := range summaries {
		var name string
		var deleted sql.NullString
		var bound bool
		err := s.conn.QueryRowContext(ctx,
			"SELECT name, deleted_at, EXISTS(SELECT 1 FROM project_roots WHERE project_id = p.id AND detached_at IS NULL) FROM projects p WHERE id = ?",
			summary.ID).Scan(&name, &deleted, &bound)
		if err != nil {
			return fmt.Errorf("describe project %s: %w", summary.ID, err)
		}

		created := s.createdProjects[summary.ID]
		s.report.Projects = append(s.report.Projects, exchange.ProjectOutcome{ID: summary.ID, Name: name, Created: created, Bound: bound})

		if deleted.Valid {
			s.report.Warnings = append(s.report.Warnings,
				fmt.Sprintf("Project %q is in the trash on this device; restore it to see the imported data.", name))
			continue
		}

		if created {
			var duplicates []string
			rows, err := s.conn.QueryContext(ctx,
				"SELECT id FROM projects WHERE id <> ? AND deleted_at IS NULL AND lower(name) = lower(?) ORDER BY id", summary.ID, name)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				duplicates = append(duplicates, id)
			}
			rows.Close()

			if len(duplicates) > 0 {
				s.report.Warnings = append(s.report.Warnings, fmt.Sprintf(
					"Project %q was imported as a new project, but this device already has a project with the same name (%s). "+
						"If both are the same repository, keep one of them and bind its folder.",
					name, strings.Join(duplicates, ", ")))
			}
		}

		if !bound {
			s.report.Warnings = append(s.report.Warnings, fmt.Sprintf(
				"Project %q has no folder on this device. Clone the repository and bind it: istok project rebind %s PATH",
				name, summary.ID))
		}
	}

	return nil
}
