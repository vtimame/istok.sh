package exchangerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/vtimame/istok.sh/internal/exchange"
)

// querier is the part of *sql.DB and *sql.Conn the repository reads through.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// columns returns the exported columns of a table in schema order.
func columns(ctx context.Context, q querier, spec table) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT name FROM pragma_table_info(?) ORDER BY cid", spec.name)
	if err != nil {
		return nil, fmt.Errorf("read columns of %s: %w", spec.name, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !slices.Contains(spec.exclude, name) {
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("table %s has no columns", spec.name)
	}

	return names, nil
}

// scanRows reads every row of a query into column-keyed maps.
func scanRows(rows *sql.Rows, names []string) ([]exchange.Row, error) {
	defer rows.Close()

	result := []exchange.Row{}
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}

		row := exchange.Row{}
		for index, name := range names {
			row[name] = normalize(values[index])
		}
		result = append(result, row)
	}

	return result, rows.Err()
}

// normalize turns database and JSON values into one comparable form:
// integers become int64, other numbers float64, bytes strings.
func normalize(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if float, err := typed.Float64(); err == nil {
			return float
		}
		return typed.String()
	case int:
		return int64(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return int64(typed)
		}
		return typed
	case bool:
		if typed {
			return int64(1)
		}
		return int64(0)
	default:
		return typed
	}
}

// equalValues compares two normalized values.
func equalValues(left, right any) bool {
	left, right = normalize(left), normalize(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return fmt.Sprint(left) == fmt.Sprint(right)
}

// keyOf joins the key column values of a row.
func keyOf(spec table, row exchange.Row) string {
	parts := make([]string, len(spec.key))
	for index, column := range spec.key {
		parts[index] = fmt.Sprint(normalize(row[column]))
	}

	return strings.Join(parts, "/")
}

// keyWhere builds the WHERE clause and arguments selecting one row by key.
func keyWhere(spec table, row exchange.Row) (string, []any) {
	conditions := make([]string, len(spec.key))
	args := make([]any, len(spec.key))
	for index, column := range spec.key {
		conditions[index] = column + " = ?"
		args[index] = normalize(row[column])
	}

	return strings.Join(conditions, " AND "), args
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

// expandProjects replaces {projects} with placeholders and repeats the
// project IDs once per occurrence.
func expandProjects(clause string, projectIDs []string) (string, []any) {
	occurrences := strings.Count(clause, "{projects}")
	expanded := strings.ReplaceAll(clause, "{projects}", placeholders(len(projectIDs)))

	args := make([]any, 0, occurrences*len(projectIDs))
	for range occurrences {
		for _, id := range projectIDs {
			args = append(args, id)
		}
	}

	return expanded, args
}
