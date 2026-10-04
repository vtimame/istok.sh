package uireadrepo

import (
	"context"
	"fmt"
)

// ChangeWatcher reports whether another connection, in this or another
// process, committed to the database since the previous check, using
// PRAGMA data_version.
//
// data_version is only comparable on one connection. The storage pool keeps a
// single open connection (SetMaxOpenConns(1)), so reading through the pool
// always hits the same one without pinning it, which would block every other
// query of the process. If the pool ever recreates that connection, the
// baseline changes and the watcher reports one spurious change, which is
// harmless for cache invalidation. Commits made through this same connection
// do not move data_version; callers that write must signal those themselves.
type ChangeWatcher struct {
	repository *Repository
	last       int64
}

func (r *Repository) NewChangeWatcher(ctx context.Context) (*ChangeWatcher, error) {
	watcher := &ChangeWatcher{repository: r}

	last, err := watcher.version(ctx)
	if err != nil {
		return nil, err
	}
	watcher.last = last

	return watcher, nil
}

// Changed reports whether data_version moved since the last call.
func (w *ChangeWatcher) Changed(ctx context.Context) (bool, error) {
	current, err := w.version(ctx)
	if err != nil {
		return false, err
	}

	changed := current != w.last
	w.last = current

	return changed, nil
}

func (w *ChangeWatcher) version(ctx context.Context) (int64, error) {
	var value int64
	if err := w.repository.db.QueryRowContext(ctx, "PRAGMA data_version").Scan(&value); err != nil {
		return 0, fmt.Errorf("read data_version: %w", err)
	}

	return value, nil
}
