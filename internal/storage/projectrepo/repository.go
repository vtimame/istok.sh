package projectrepo

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"

	"s26.dev/istok-cli/internal/project"
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Init(ctx context.Context, value project.Project, root project.Root) (result project.InitResult, err error) {
	projectValue, err := r.write(ctx, func(conn *sql.Conn) (project.Project, error) {
		if current, found, err := currentAt(ctx, conn, root.PathKey); err != nil {
			return project.Project{}, err
		} else if found {
			return current, nil
		}
		if err := rejectOverlap(ctx, conn, root.PathKey, ""); err != nil {
			return project.Project{}, err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO projects(id,name,revision,created_at,updated_at) VALUES(?,?,?,?,?)`, value.ID, value.Name, 1, stamp(value.CreatedAt), stamp(value.UpdatedAt)); err != nil {
			return project.Project{}, mapSQLError(err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO project_roots(project_id,canonical_path,path_key,active_at) VALUES(?,?,?,?)`, value.ID, root.CanonicalPath, root.PathKey, stamp(root.ActiveAt)); err != nil {
			return project.Project{}, mapSQLError(err)
		}
		return getProject(ctx, conn, value.ID)
	})
	if err != nil {
		return project.InitResult{}, err
	}
	created := projectValue.ID == value.ID
	return project.InitResult{Project: projectValue, Created: created}, nil
}

func (r *Repository) List(ctx context.Context, deleted bool) ([]project.Project, error) {
	where := "WHERE p.deleted_at IS NULL"
	if deleted {
		where = ""
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.name,p.revision,p.created_at,p.updated_at,p.deleted_at,pr.canonical_path,pr.path_key,pr.active_at,pr.detached_at FROM projects p LEFT JOIN project_roots pr ON pr.id=(SELECT id FROM project_roots WHERE project_id=p.id ORDER BY CASE WHEN detached_at IS NULL THEN 0 ELSE 1 END,id DESC LIMIT 1) `+where+` ORDER BY p.created_at,p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjects(rows)
}

func (r *Repository) Current(ctx context.Context, pathKey string) (project.Project, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.name,p.revision,p.created_at,p.updated_at,p.deleted_at,pr.canonical_path,pr.path_key,pr.active_at,pr.detached_at FROM projects p JOIN project_roots pr ON pr.project_id=p.id WHERE p.deleted_at IS NULL AND pr.detached_at IS NULL`)
	if err != nil {
		return project.Project{}, err
	}
	defer rows.Close()
	items, err := scanProjects(rows)
	if err != nil {
		return project.Project{}, err
	}
	var selected *project.Project
	for i := range items {
		if contains(items[i].Root.PathKey, pathKey) && (selected == nil || len(items[i].Root.PathKey) > len(selected.Root.PathKey)) {
			selected = &items[i]
		}
	}
	if selected == nil {
		return project.Project{}, notFound()
	}
	return *selected, nil
}

func (r *Repository) Resolve(ctx context.Context, selector project.Selector, includeDeleted bool) (project.Project, error) {
	items, err := r.List(ctx, includeDeleted)
	if err != nil {
		return project.Project{}, err
	}
	key := string(selector)
	matches := make(map[string]project.Project)
	for _, p := range items {
		if p.ID == key || (strings.HasPrefix(p.ID, key) && len(key) >= 1) || p.Name == key {
			matches[p.ID] = p
		}
	}
	if len(matches) == 0 {
		return project.Project{}, notFound()
	}
	if len(matches) > 1 {
		return project.Project{}, projectError(project.CodeAmbiguous, "project selector is ambiguous")
	}
	for _, p := range matches {
		return p, nil
	}
	panic("unreachable")
}

func (r *Repository) Rename(ctx context.Context, id, name string, expected *int64) (project.Project, error) {
	return r.write(ctx, func(c *sql.Conn) (project.Project, error) {
		p, err := getProject(ctx, c, id)
		if err != nil {
			return project.Project{}, err
		}
		if p.Name == name {
			return p, nil
		}
		if err = cas(p, expected); err != nil {
			return project.Project{}, err
		}

		now := time.Now().UTC()
		if _, err = c.ExecContext(ctx, `UPDATE projects SET name=?,revision=revision+1,updated_at=? WHERE id=?`, name, stamp(now), id); err != nil {
			return project.Project{}, err
		}
		return getProject(ctx, c, id)
	})
}

func (r *Repository) Rebind(ctx context.Context, id string, root project.Root, expected *int64) (project.Project, error) {
	return r.write(ctx, func(c *sql.Conn) (project.Project, error) {
		p, err := getProject(ctx, c, id)
		if err != nil {
			return project.Project{}, err
		}
		if p.DeletedAt != nil {
			return project.Project{}, projectError(project.CodeConflict, "cannot rebind a deleted project")
		}
		if p.Root != nil && p.Root.PathKey == root.PathKey {
			return p, nil
		}
		if err = cas(p, expected); err != nil {
			return project.Project{}, err
		}
		if err = rejectOverlap(ctx, c, root.PathKey, id); err != nil {
			return project.Project{}, err
		}
		now := time.Now().UTC()
		if _, err = c.ExecContext(ctx, `UPDATE project_roots SET detached_at=? WHERE project_id=? AND detached_at IS NULL`, stamp(now), id); err != nil {
			return project.Project{}, err
		}
		if _, err = c.ExecContext(ctx, `INSERT INTO project_roots(project_id,canonical_path,path_key,active_at) VALUES(?,?,?,?)`, id, root.CanonicalPath, root.PathKey, stamp(root.ActiveAt)); err != nil {
			return project.Project{}, mapSQLError(err)
		}
		if _, err = c.ExecContext(ctx, `UPDATE projects SET revision=revision+1,updated_at=? WHERE id=?`, stamp(now), id); err != nil {
			return project.Project{}, err
		}
		return getProject(ctx, c, id)
	})
}

func (r *Repository) Delete(ctx context.Context, id string, expected *int64) (project.Project, error) {
	return r.write(ctx, func(c *sql.Conn) (project.Project, error) {
		p, err := getProject(ctx, c, id)
		if err != nil {
			return project.Project{}, err
		}
		if p.DeletedAt != nil {
			return p, nil
		}
		if err = cas(p, expected); err != nil {
			return project.Project{}, err
		}

		now := time.Now().UTC()
		if _, err = c.ExecContext(ctx, `UPDATE project_roots SET detached_at=? WHERE project_id=? AND detached_at IS NULL`, stamp(now), id); err != nil {
			return project.Project{}, err
		}
		if _, err = c.ExecContext(ctx, `UPDATE projects SET deleted_at=?,revision=revision+1,updated_at=? WHERE id=?`, stamp(now), stamp(now), id); err != nil {
			return project.Project{}, err
		}
		return getProject(ctx, c, id)
	})
}

func (r *Repository) Restore(ctx context.Context, id string, requested *project.Root, expected *int64) (project.Project, error) {
	return r.write(ctx, func(c *sql.Conn) (project.Project, error) {
		p, err := getProject(ctx, c, id)
		if err != nil {
			return project.Project{}, err
		}
		if p.DeletedAt == nil {
			if requested == nil || (p.Root != nil && p.Root.PathKey == requested.PathKey) {
				return p, nil
			}
			if err = cas(p, expected); err != nil {
				return project.Project{}, err
			}

			return project.Project{}, projectError(project.CodeConflict, "project is already active at another root")
		}
		if err = cas(p, expected); err != nil {
			return project.Project{}, err
		}

		var root project.Root
		if requested != nil {
			root = *requested
		} else {
			row := c.QueryRowContext(ctx, `SELECT canonical_path,path_key,active_at,detached_at FROM project_roots WHERE project_id=? ORDER BY id DESC LIMIT 1`, id)
			if err := scanRoot(row, &root); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return project.Project{}, projectError(project.CodeConflict, "project has no root to restore")
				}
				return project.Project{}, err
			}

		}
		if err = rejectOverlap(ctx, c, root.PathKey, id); err != nil {
			return project.Project{}, err
		}
		now := time.Now().UTC()
		if requested == nil {
			if _, err = c.ExecContext(ctx, `UPDATE project_roots SET detached_at=NULL,active_at=?,canonical_path=?,path_key=? WHERE project_id=? AND id=(SELECT id FROM project_roots WHERE project_id=? ORDER BY id DESC LIMIT 1)`, stamp(now), root.CanonicalPath, root.PathKey, id, id); err != nil {
				return project.Project{}, mapSQLError(err)
			}
		} else if _, err = c.ExecContext(ctx, `INSERT INTO project_roots(project_id,canonical_path,path_key,active_at) VALUES(?,?,?,?)`, id, root.CanonicalPath, root.PathKey, stamp(root.ActiveAt)); err != nil {
			return project.Project{}, mapSQLError(err)
		}
		if _, err = c.ExecContext(ctx, `UPDATE projects SET deleted_at=NULL,revision=revision+1,updated_at=? WHERE id=?`, stamp(now), id); err != nil {
			return project.Project{}, err
		}
		return getProject(ctx, c, id)
	})
}

func (r *Repository) write(ctx context.Context, fn func(*sql.Conn) (project.Project, error)) (project.Project, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return project.Project{}, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return project.Project{}, err
	}
	done := false
	defer func() {
		if !done {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	value, err := fn(conn)
	if err != nil {
		return project.Project{}, err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return project.Project{}, err
	}
	done = true
	return value, nil
}

func currentAt(ctx context.Context, c *sql.Conn, key string) (project.Project, bool, error) {
	rows, err := c.QueryContext(ctx, projectQuery+` WHERE p.deleted_at IS NULL AND pr.detached_at IS NULL`)
	if err != nil {
		return project.Project{}, false, err
	}
	defer rows.Close()
	items, err := scanProjects(rows)
	if err != nil {
		return project.Project{}, false, err
	}
	var selected *project.Project
	for i := range items {
		if contains(items[i].Root.PathKey, key) && (selected == nil || len(items[i].Root.PathKey) > len(selected.Root.PathKey)) {
			selected = &items[i]
		}
	}
	if selected == nil {
		return project.Project{}, false, nil
	}
	return *selected, true, nil
}

const projectQuery = `SELECT p.id,p.name,p.revision,p.created_at,p.updated_at,p.deleted_at,pr.canonical_path,pr.path_key,pr.active_at,pr.detached_at FROM projects p LEFT JOIN project_roots pr ON pr.project_id=p.id AND pr.detached_at IS NULL`

func getProject(ctx context.Context, c *sql.Conn, id string) (project.Project, error) {
	rows, err := c.QueryContext(ctx, projectQuery+` WHERE p.id=?`, id)
	if err != nil {
		return project.Project{}, err
	}
	defer rows.Close()
	items, err := scanProjects(rows)
	if err != nil {
		return project.Project{}, err
	}
	if len(items) == 0 {
		return project.Project{}, notFound()
	}
	return items[0], nil
}

func rejectOverlap(ctx context.Context, c *sql.Conn, key, except string) error {
	rows, err := c.QueryContext(ctx, `SELECT project_id,path_key FROM project_roots WHERE detached_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, existing string
		if err := rows.Scan(&id, &existing); err != nil {
			return err
		}
		if id != except && (contains(existing, key) || contains(key, existing)) {
			return projectError(project.CodeConflict, "project root overlaps an active project")
		}
	}
	return rows.Err()
}

func contains(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func cas(p project.Project, expected *int64) error {
	if expected != nil && p.Revision != *expected {
		return projectError(project.CodeRevisionConflict, "project revision does not match expected_revision")
	}
	return nil
}

func notFound() error                             { return projectError(project.CodeNotFound, "project was not found") }
func projectError(c project.Code, m string) error { return &project.Error{Code: c, Message: m} }

func mapSQLError(err error) error {
	var sqliteError sqlite3.Error
	if errors.As(err, &sqliteError) && (sqliteError.ExtendedCode == sqlite3.ErrConstraintPrimaryKey || sqliteError.ExtendedCode == sqlite3.ErrConstraintUnique) {
		return projectError(project.CodeConflict, "project root conflicts with an active project")
	}

	return err
}

func stamp(v time.Time) string { return v.UTC().Format(time.RFC3339Nano) }

type rootScanner interface{ Scan(...any) error }

func scanRoot(row rootScanner, root *project.Root) error {
	var active string
	var detached sql.NullString
	if err := row.Scan(&root.CanonicalPath, &root.PathKey, &active, &detached); err != nil {
		return err
	}
	var err error
	root.ActiveAt, err = time.Parse(time.RFC3339Nano, active)
	if err != nil {
		return err
	}
	if detached.Valid {
		v, e := time.Parse(time.RFC3339Nano, detached.String)
		if e != nil {
			return e
		}
		root.DetachedAt = &v
	}
	return nil
}

func scanProjects(rows *sql.Rows) ([]project.Project, error) {
	var result []project.Project
	for rows.Next() {
		var p project.Project
		var created, updated string
		var deleted sql.NullString
		var canonical, key, active, detached sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.Revision, &created, &updated, &deleted, &canonical, &key, &active, &detached); err != nil {
			return nil, err
		}
		var err error
		p.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if deleted.Valid {
			v, e := time.Parse(time.RFC3339Nano, deleted.String)
			if e != nil {
				return nil, e
			}
			p.DeletedAt = &v
		}
		if canonical.Valid {
			root := &project.Root{CanonicalPath: canonical.String, PathKey: key.String}
			root.ActiveAt, err = time.Parse(time.RFC3339Nano, active.String)
			if err != nil {
				return nil, err
			}
			if detached.Valid {
				v, e := time.Parse(time.RFC3339Nano, detached.String)
				if e != nil {
					return nil, e
				}
				root.DetachedAt = &v
			}
			p.Root = root
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
