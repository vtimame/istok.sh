package exchangerepo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vtimame/istok.sh/internal/exchange"
)

// Repository reads and writes exchange bundles.
type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// SchemaVersion returns the applied migration version of this database.
func (r *Repository) SchemaVersion(ctx context.Context) (int64, error) {
	var version int64
	if err := r.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}

	return version, nil
}

// Export reads the selected projects into a bundle. Deleted projects are not
// exported; tasks, context and knowledge in the trash are, so deletions reach
// the other device.
func (r *Repository) Export(ctx context.Context, projectIDs []string) (exchange.Bundle, error) {
	if len(projectIDs) == 0 {
		return exchange.Bundle{}, exchange.Errorf(exchange.CodeInvalid, "no projects selected for export")
	}

	version, err := r.SchemaVersion(ctx)
	if err != nil {
		return exchange.Bundle{}, err
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return exchange.Bundle{}, err
	}
	defer conn.Close()

	// One read transaction gives a consistent snapshot across tables.
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return exchange.Bundle{}, err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()

	bundle := exchange.Bundle{
		Format:        exchange.Format,
		SchemaVersion: version,
		ExportedAt:    time.Now().UTC(),
		Tables:        map[string][]exchange.Row{},
	}

	for _, spec := range tables {
		names, err := columns(ctx, conn, spec)
		if err != nil {
			return exchange.Bundle{}, err
		}

		where, args := expandProjects(spec.where, projectIDs)
		if spec.name == "projects" {
			where += " AND deleted_at IS NULL"
		}

		query := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s",
			strings.Join(names, ", "), spec.name, where, strings.Join(spec.key, ", "))
		rows, err := conn.QueryContext(ctx, query, args...)
		if err != nil {
			return exchange.Bundle{}, fmt.Errorf("export %s: %w", spec.name, err)
		}

		values, err := scanRows(rows, names)
		if err != nil {
			return exchange.Bundle{}, fmt.Errorf("export %s: %w", spec.name, err)
		}
		bundle.Tables[spec.name] = values
	}

	bundle.Projects = summaries(bundle)
	if len(bundle.Projects) != len(projectIDs) {
		return exchange.Bundle{}, exchange.Errorf(exchange.CodeNotFound, "some selected projects do not exist or are deleted")
	}

	return bundle, nil
}

func summaries(bundle exchange.Bundle) []exchange.ProjectSummary {
	counts := map[string]int{}
	for _, row := range bundle.Tables["tasks"] {
		counts[fmt.Sprint(row["project_id"])]++
	}

	result := []exchange.ProjectSummary{}
	for _, row := range bundle.Tables["projects"] {
		id := fmt.Sprint(row["id"])
		result = append(result, exchange.ProjectSummary{ID: id, Name: fmt.Sprint(row["name"]), Tasks: counts[id]})
	}

	return result
}
