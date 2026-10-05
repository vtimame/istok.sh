package exchangerepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/vtimame/istok.sh/internal/exchange"
)

// Actor attributes history events the import writes, such as renumbering.
type Actor struct {
	ID   string
	Kind string
	Name string
}

// importer holds the state of one import transaction.
type importer struct {
	conn   *sql.Conn
	actor  Actor
	report exchange.Report

	// skipped and replaced hold row keys per table. A row is replaced when the
	// import created it or overwrote it with the incoming version.
	skipped  map[string]map[string]bool
	replaced map[string]map[string]bool

	createdProjects map[string]bool
}

// Import merges a bundle into the database in one transaction. A dry run
// performs the same work and rolls it back, so its report is exact.
func (r *Repository) Import(ctx context.Context, bundle exchange.Bundle, actor Actor, dryRun bool) (exchange.Report, error) {
	version, err := r.SchemaVersion(ctx)
	if err != nil {
		return exchange.Report{}, err
	}
	if err := bundle.Validate(version); err != nil {
		return exchange.Report{}, err
	}
	for name := range bundle.Tables {
		if !slices.ContainsFunc(tables, func(spec table) bool { return spec.name == name }) {
			return exchange.Report{}, exchange.Errorf(exchange.CodeInvalid, "bundle contains unknown table %q", name)
		}
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return exchange.Report{}, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return exchange.Report{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	// Rows reference each other within the bundle, for example superseded
	// records; foreign keys are checked once, before commit.
	if _, err := conn.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		return exchange.Report{}, err
	}

	state := &importer{
		conn:            conn,
		actor:           actor,
		report:          exchange.NewReport(dryRun),
		skipped:         map[string]map[string]bool{},
		replaced:        map[string]map[string]bool{},
		createdProjects: map[string]bool{},
	}

	for _, spec := range tables {
		if err := state.importTable(ctx, spec, bundle.Tables[spec.name]); err != nil {
			return exchange.Report{}, fmt.Errorf("import %s: %w", spec.name, err)
		}
	}

	projectIDs := make([]string, 0, len(bundle.Projects))
	for _, summary := range bundle.Projects {
		projectIDs = append(projectIDs, summary.ID)
	}

	if err := state.finish(ctx, projectIDs); err != nil {
		return exchange.Report{}, err
	}
	if err := state.describeProjects(ctx, bundle.Projects); err != nil {
		return exchange.Report{}, err
	}

	if dryRun {
		return state.report, nil
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return exchange.Report{}, fmt.Errorf("commit import: %w", err)
	}
	committed = true

	return state.report, nil
}

func (s *importer) importTable(ctx context.Context, spec table, rows []exchange.Row) error {
	names, err := columns(ctx, s.conn, spec)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := checkRow(spec, names, row); err != nil {
			return err
		}
	}

	switch spec.kind {
	case mutable:
		for _, row := range rows {
			if err := s.mergeMutable(ctx, spec, names, row); err != nil {
				return err
			}
		}
	case appendOnly:
		for _, row := range rows {
			if err := s.mergeAppend(ctx, spec, names, row); err != nil {
				return err
			}
		}
	case followsParent:
		return s.replaceChildren(ctx, spec, names, rows)
	}

	return nil
}

// checkRow requires exactly the local columns, so a bundle from a different
// schema cannot be applied partially.
func checkRow(spec table, names []string, row exchange.Row) error {
	for _, name := range names {
		if _, ok := row[name]; !ok {
			return exchange.Errorf(exchange.CodeInvalid, "row of %s is missing column %q", spec.name, name)
		}
	}
	for name := range row {
		if !slices.Contains(names, name) {
			return exchange.Errorf(exchange.CodeInvalid, "row of %s has unknown column %q", spec.name, name)
		}
	}

	return nil
}

func (s *importer) parentSkipped(spec table, row exchange.Row) bool {
	for _, link := range spec.parents {
		value := normalize(row[link.column])
		if value != nil && s.skipped[link.table][fmt.Sprint(value)] {
			return true
		}
	}

	return false
}

func (s *importer) mark(set map[string]map[string]bool, tableName, key string) {
	if set[tableName] == nil {
		set[tableName] = map[string]bool{}
	}
	set[tableName][key] = true
}

func (s *importer) count(tableName string, update func(*exchange.TableCounts)) {
	counts := s.report.Tables[tableName]
	update(&counts)
	s.report.Tables[tableName] = counts
}

func (s *importer) skip(spec table, row exchange.Row, reason string) {
	key := keyOf(spec, row)
	s.mark(s.skipped, spec.name, key)
	s.count(spec.name, func(c *exchange.TableCounts) { c.Skipped++ })
	if reason != "" {
		s.report.Conflicts = append(s.report.Conflicts, exchange.Conflict{Table: spec.name, ID: key, Reason: reason})
	}
}

// local reads the local version of a row, or nil when it does not exist.
func (s *importer) local(ctx context.Context, spec table, names []string, row exchange.Row) (exchange.Row, error) {
	where, args := keyWhere(spec, row)
	rows, err := s.conn.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(names, ", "), spec.name, where), args...)
	if err != nil {
		return nil, err
	}

	values, err := scanRows(rows, names)
	if err != nil || len(values) == 0 {
		return nil, err
	}

	return values[0], nil
}

func (s *importer) mergeMutable(ctx context.Context, spec table, names []string, row exchange.Row) error {
	if s.parentSkipped(spec, row) {
		s.skip(spec, row, "")
		return nil
	}

	key := keyOf(spec, row)
	current, err := s.local(ctx, spec, names, row)
	if err != nil {
		return err
	}

	if current == nil {
		if reason, err := s.insertBlocked(ctx, spec, row); err != nil || reason != "" {
			if err == nil {
				s.skip(spec, row, reason)
			}
			return err
		}
		if err := s.insertMutable(ctx, spec, names, row); err != nil {
			return err
		}
		if spec.name == "projects" {
			s.createdProjects[key] = true
		}
		s.mark(s.replaced, spec.name, key)
		s.count(spec.name, func(c *exchange.TableCounts) { c.Created++ })
		return nil
	}

	incoming, localRevision := normalize(row["revision"]), normalize(current["revision"])
	incomingRevision, ok1 := incoming.(int64)
	currentRevision, ok2 := localRevision.(int64)
	if !ok1 || !ok2 {
		return exchange.Errorf(exchange.CodeInvalid, "row %s of %s has no integer revision", key, spec.name)
	}

	switch {
	case incomingRevision > currentRevision:
		if reason, err := s.updateBlocked(ctx, spec, row); err != nil || reason != "" {
			if err == nil {
				s.count(spec.name, func(c *exchange.TableCounts) { c.KeptLocal++ })
				s.report.Conflicts = append(s.report.Conflicts, exchange.Conflict{Table: spec.name, ID: key, Reason: reason})
			}
			return err
		}
		if err := s.update(ctx, spec, names, row); err != nil {
			return err
		}
		s.mark(s.replaced, spec.name, key)
		s.count(spec.name, func(c *exchange.TableCounts) { c.Updated++ })
	case incomingRevision == currentRevision && sameContent(spec, names, row, current):
		s.count(spec.name, func(c *exchange.TableCounts) { c.Unchanged++ })
	case incomingRevision == currentRevision:
		s.count(spec.name, func(c *exchange.TableCounts) { c.KeptLocal++ })
		s.report.Conflicts = append(s.report.Conflicts, exchange.Conflict{
			Table:  spec.name,
			ID:     key,
			Reason: fmt.Sprintf("changed on both devices at revision %d; kept the local version", currentRevision),
		})
	default:
		s.count(spec.name, func(c *exchange.TableCounts) { c.KeptLocal++ })
	}

	return nil
}

func sameContent(spec table, names []string, incoming, current exchange.Row) bool {
	for _, name := range names {
		if slices.Contains(spec.keepLocal, name) {
			continue
		}
		if !equalValues(incoming[name], current[name]) {
			return false
		}
	}

	return true
}

func (s *importer) insertMutable(ctx context.Context, spec table, names []string, row exchange.Row) error {
	if spec.name == "tasks" {
		return s.insertTask(ctx, names, row)
	}

	return s.insert(ctx, spec, names, row)
}

func (s *importer) insert(ctx context.Context, spec table, names []string, row exchange.Row) error {
	args := make([]any, len(names))
	for index, name := range names {
		args[index] = normalize(row[name])
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", spec.name, strings.Join(names, ", "), placeholders(len(names)))
	if _, err := s.conn.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert %s %s: %w", spec.name, keyOf(spec, row), err)
	}

	return nil
}

func (s *importer) update(ctx context.Context, spec table, names []string, row exchange.Row) error {
	assignments := []string{}
	args := []any{}
	for _, name := range names {
		if slices.Contains(spec.key, name) || slices.Contains(spec.keepLocal, name) {
			continue
		}
		assignments = append(assignments, name+" = ?")
		args = append(args, normalize(row[name]))
	}

	where, keyArgs := keyWhere(spec, row)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", spec.name, strings.Join(assignments, ", "), where)
	if _, err := s.conn.ExecContext(ctx, query, append(args, keyArgs...)...); err != nil {
		return fmt.Errorf("update %s %s: %w", spec.name, keyOf(spec, row), err)
	}

	return nil
}

// insertBlocked and updateBlocked report unique invariants that an incoming
// row would break; the row is then left out with a conflict.
func (s *importer) insertBlocked(ctx context.Context, spec table, row exchange.Row) (string, error) {
	if spec.name == "runs" {
		return s.otherActiveRun(ctx, row)
	}

	return "", nil
}

func (s *importer) updateBlocked(ctx context.Context, spec table, row exchange.Row) (string, error) {
	if spec.name == "runs" {
		return s.otherActiveRun(ctx, row)
	}

	return "", nil
}

// otherActiveRun allows one active run per task: two devices may each have
// claimed the same task while apart.
func (s *importer) otherActiveRun(ctx context.Context, row exchange.Row) (string, error) {
	if fmt.Sprint(row["status"]) != "active" {
		return "", nil
	}

	var other string
	err := s.conn.QueryRowContext(ctx, "SELECT id FROM runs WHERE task_id = ? AND status = 'active' AND id <> ?",
		normalize(row["task_id"]), normalize(row["id"])).Scan(&other)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("the task already has active run %s on this device", other), nil
}

func (s *importer) mergeAppend(ctx context.Context, spec table, names []string, row exchange.Row) error {
	if s.parentSkipped(spec, row) {
		s.skip(spec, row, "")
		return nil
	}

	current, err := s.local(ctx, spec, names, row)
	if err != nil {
		return err
	}
	if current != nil {
		s.count(spec.name, func(c *exchange.TableCounts) { c.Unchanged++ })
		return nil
	}

	if spec.name == "task_completions" {
		var other string
		err := s.conn.QueryRowContext(ctx, "SELECT id FROM task_completions WHERE task_id = ?", normalize(row["task_id"])).Scan(&other)
		if err == nil {
			s.skip(spec, row, fmt.Sprintf("the task was already completed on this device (%s)", other))
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	if err := s.insert(ctx, spec, names, row); err != nil {
		return err
	}
	s.mark(s.replaced, spec.name, keyOf(spec, row))
	s.count(spec.name, func(c *exchange.TableCounts) { c.Created++ })

	return nil
}

// replaceChildren swaps the local child rows of every replaced parent for the
// incoming ones. Parents kept locally keep their local children.
func (s *importer) replaceChildren(ctx context.Context, spec table, names []string, rows []exchange.Row) error {
	ownerTable := ""
	for _, link := range spec.parents {
		if link.column == spec.owner {
			ownerTable = link.table
		}
	}

	incoming := map[string][]exchange.Row{}
	for _, row := range rows {
		owner := fmt.Sprint(normalize(row[spec.owner]))
		incoming[owner] = append(incoming[owner], row)
	}

	owners := make([]string, 0, len(s.replaced[ownerTable]))
	for owner := range s.replaced[ownerTable] {
		owners = append(owners, owner)
	}
	sort.Strings(owners)

	for _, owner := range owners {
		result, err := s.conn.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = ?", spec.name, spec.owner), owner)
		if err != nil {
			return err
		}
		removed, _ := result.RowsAffected()

		inserted := 0
		for _, row := range incoming[owner] {
			if s.parentSkipped(spec, row) {
				s.skip(spec, row, "")
				continue
			}
			if err := s.insert(ctx, spec, names, row); err != nil {
				return err
			}
			inserted++
		}

		s.count(spec.name, func(c *exchange.TableCounts) {
			c.Created += max(inserted-int(removed), 0)
			c.Updated += min(inserted, int(removed))
		})
	}

	for owner, group := range incoming {
		switch {
		case s.replaced[ownerTable][owner]:
		case s.skipped[ownerTable][owner]:
			for _, row := range group {
				s.skip(spec, row, "")
			}
		default:
			s.count(spec.name, func(c *exchange.TableCounts) { c.Unchanged += len(group) })
		}
	}

	return nil
}
