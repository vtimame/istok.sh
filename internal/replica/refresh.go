package replica

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/protocol"
)

// RefreshAge is how old the last sync of a project may be before a read
// syncs it again. Tests lower it.
var RefreshAge = 30 * time.Second

const (
	// refreshTimeout bounds how long a read waits for the hub.
	refreshTimeout = 2 * time.Second

	// refreshBackoff skips the hub for reads after it was out of reach. It is
	// kept in a file because the CLI runs one short process per command.
	refreshBackoff = 30 * time.Second
)

func unreachablePath(dir string) string { return filepath.Join(dir, "hub-unreachable") }

// Refresh syncs a project before it is read, so task lists and briefs show
// what other devices did, unless the project synced within RefreshAge or the
// hub was out of reach recently. It waits for the hub at most refreshTimeout
// and reports nothing: a read never fails because of the hub.
func (s *Syncer) Refresh(ctx context.Context, projectID string) {
	if !s.Joined() || s.unreachableRecently() {
		return
	}

	value, enabled, err := s.state.Project(ctx, projectID)
	if err != nil || !enabled {
		return
	}
	if value.SyncedAt != nil && time.Since(*value.SyncedAt) < RefreshAge {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	// Another process syncing right now brings the same changes.
	unlock, err := Lock(ctx, s.dir)
	if err != nil {
		return
	}
	defer unlock()

	hub, key, _, err := Connect(s.dir)
	if err != nil {
		return
	}
	defer hub.Close()

	_, err = NewEngine(s.exchange, s.state, hub, key).Sync(ctx, projectID)

	var hubError *protocol.Error
	switch {
	case err == nil:
		_ = os.Remove(unreachablePath(s.dir))
	case !errors.As(err, &hubError):
		path, now := unreachablePath(s.dir), time.Now()
		if os.WriteFile(path, nil, 0o600) == nil {
			_ = os.Chtimes(path, now, now)
		}
	}
}

func (s *Syncer) unreachableRecently() bool {
	info, err := os.Stat(unreachablePath(s.dir))

	return err == nil && time.Since(info.ModTime()) < refreshBackoff
}
