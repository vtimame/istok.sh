package exchangerepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/vtimame/istok.sh/internal/exchange"
)

// TaskNumber is the number of one task.
type TaskNumber struct {
	TaskID string
	Number int64
}

// temporaryNumbers moves renumbered tasks out of the way, so numbers can be
// swapped without breaking the per-project uniqueness.
const temporaryNumbers = int64(1) << 40

// UnconfirmedTasks lists the tasks of a synced project whose number the hub
// has not confirmed, oldest first, so earlier tasks keep their numbers.
func (r *Repository) UnconfirmedTasks(ctx context.Context, projectID string) ([]TaskNumber, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT t.id, t.number FROM tasks t "+
			"LEFT JOIN sync_task_numbers n ON n.project_id = t.project_id AND n.task_id = t.id "+
			"WHERE t.project_id = ? AND (n.task_id IS NULL OR n.number != t.number) "+
			"ORDER BY t.created_at, t.id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TaskNumber
	for rows.Next() {
		var value TaskNumber
		if err := rows.Scan(&value.TaskID, &value.Number); err != nil {
			return nil, err
		}
		result = append(result, value)
	}

	return result, rows.Err()
}

// ApplyNumbers gives tasks the numbers the hub assigned and records them as
// confirmed. A renumbered task gets a history event. A task in the way of an
// assigned number moves to the next free number and has to claim it again.
func (r *Repository) ApplyNumbers(ctx context.Context, projectID string, assigned []TaskNumber, actor Actor) ([]exchange.Renumbering, error) {
	if len(assigned) == 0 {
		return nil, nil
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	numbers := &numbering{conn: conn, projectID: projectID, actor: actor}
	if err := numbers.apply(ctx, assigned); err != nil {
		return nil, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, err
	}
	committed = true

	return numbers.renumbered, nil
}

type numbering struct {
	conn       *sql.Conn
	projectID  string
	actor      Actor
	renumbered []exchange.Renumbering
}

type numberChange struct {
	taskID   string
	title    string
	from, to int64
}

func (n *numbering) apply(ctx context.Context, assigned []TaskNumber) error {
	// Read the current numbers of the assigned tasks; deleted tasks are gone.
	var changes []numberChange
	moving := map[string]bool{}
	highest := int64(0)
	for _, value := range assigned {
		var current int64
		var title string
		err := n.conn.QueryRowContext(ctx, "SELECT number, title FROM tasks WHERE id = ? AND project_id = ?",
			value.TaskID, n.projectID).Scan(&current, &title)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		highest = max(highest, value.Number)
		if current != value.Number {
			changes = append(changes, numberChange{taskID: value.TaskID, title: title, from: current, to: value.Number})
			moving[value.TaskID] = true
		}
	}

	// Free the numbers of the moving tasks first.
	for index, change := range changes {
		if _, err := n.conn.ExecContext(ctx, "UPDATE tasks SET number = ? WHERE id = ?", temporaryNumbers+int64(index), change.taskID); err != nil {
			return fmt.Errorf("move task %s aside: %w", change.taskID, err)
		}
	}

	next, err := n.nextNumber(ctx, highest)
	if err != nil {
		return err
	}

	for _, change := range changes {
		var blockerID, blockerTitle string
		err := n.conn.QueryRowContext(ctx, "SELECT id, title FROM tasks WHERE project_id = ? AND number = ?",
			n.projectID, change.to).Scan(&blockerID, &blockerTitle)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		default:
			if err := n.move(ctx, numberChange{taskID: blockerID, title: blockerTitle, from: change.to, to: next},
				"the hub assigned it to another task"); err != nil {
				return err
			}
			if _, err := n.conn.ExecContext(ctx, "DELETE FROM sync_task_numbers WHERE project_id = ? AND task_id = ?",
				n.projectID, blockerID); err != nil {
				return err
			}
			next++
		}

		if err := n.move(ctx, change, "the hub assigned it"); err != nil {
			return err
		}
	}

	for _, value := range assigned {
		if _, err := n.conn.ExecContext(ctx,
			"INSERT INTO sync_task_numbers (project_id, task_id, number) SELECT ?, id, ? FROM tasks WHERE id = ? AND project_id = ? "+
				"ON CONFLICT(project_id, task_id) DO UPDATE SET number = excluded.number",
			n.projectID, value.Number, value.TaskID, n.projectID); err != nil {
			return fmt.Errorf("confirm task number: %w", err)
		}
	}

	_, err = n.conn.ExecContext(ctx,
		"INSERT INTO project_task_counters (project_id, next_number) VALUES (?, ?) "+
			"ON CONFLICT(project_id) DO UPDATE SET next_number = MAX(next_number, excluded.next_number)", n.projectID, next)

	return err
}

// nextNumber is the first number after every task of the project and every
// number the hub assigned in this batch.
func (n *numbering) nextNumber(ctx context.Context, assigned int64) (int64, error) {
	var highest, counter sql.NullInt64
	if err := n.conn.QueryRowContext(ctx, "SELECT MAX(number) FROM tasks WHERE project_id = ? AND number < ?",
		n.projectID, temporaryNumbers).Scan(&highest); err != nil {
		return 0, err
	}
	err := n.conn.QueryRowContext(ctx, "SELECT next_number FROM project_task_counters WHERE project_id = ?", n.projectID).Scan(&counter)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	return max(highest.Int64+1, counter.Int64, assigned+1, 1), nil
}

func (n *numbering) move(ctx context.Context, change numberChange, reason string) error {
	if _, err := n.conn.ExecContext(ctx, "UPDATE tasks SET number = ? WHERE id = ?", change.to, change.taskID); err != nil {
		return fmt.Errorf("renumber task %s: %w", change.taskID, err)
	}

	var revision int64
	if err := n.conn.QueryRowContext(ctx, "SELECT revision FROM tasks WHERE id = ?", change.taskID).Scan(&revision); err != nil {
		return err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return err
	}

	body := fmt.Sprintf("Renumbered from #%d to #%d: %s.", change.from, change.to, reason)
	_, err = n.conn.ExecContext(ctx,
		"INSERT INTO task_events (id, task_id, type, body, task_revision, actor_id, actor_kind, actor_name, created_at) VALUES (?, ?, 'renumbered', ?, ?, ?, ?, ?, ?)",
		id.String(), change.taskID, body, revision, n.actor.ID, n.actor.Kind, n.actor.Name, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record renumbering of %s: %w", change.taskID, err)
	}

	n.renumbered = append(n.renumbered, exchange.Renumbering{
		ProjectID: n.projectID,
		TaskID:    change.taskID,
		Title:     change.title,
		From:      change.from,
		To:        change.to,
	})

	return nil
}
