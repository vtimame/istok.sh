package replica

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
	"github.com/vtimame/istok.sh/internal/storage/syncrepo"
)

// nudgeDelay groups the writes of one agent step into one sync.
const nudgeDelay = 3 * time.Second

// nudgeTimeout bounds a background sync started by a nudge.
const nudgeTimeout = time.Minute

// Syncer runs syncs for long-lived processes and hooks: the UI service, MCP
// servers and agent hooks. Processes share the database, so a file lock in
// the sync directory lets one sync run at a time. A device that has not
// joined a hub, or a project that does not sync, is not an error: nothing
// happens.
type Syncer struct {
	dir      string
	exchange *exchangerepo.Repository
	state    *syncrepo.Repository
	warn     func(error)

	mu       sync.Mutex
	lastWarn string
	pending  map[string]bool
	timer   *time.Timer
	running sync.WaitGroup
}

// Dir is the sync directory: ISTOK_SYNC_DIR or the default.
func Dir() string {
	if value := os.Getenv("ISTOK_SYNC_DIR"); value != "" {
		return value
	}

	return DefaultDir()
}

func NewSyncer(dir string, exchange *exchangerepo.Repository, state *syncrepo.Repository, warn func(error)) *Syncer {
	if warn == nil {
		warn = func(error) {}
	}

	return &Syncer{dir: dir, exchange: exchange, state: state, warn: warn, pending: map[string]bool{}}
}

// Joined reports whether this device belongs to a hub with a key.
func (s *Syncer) Joined() bool {
	config, err := LoadConfig(s.dir)

	return err == nil && config.Key != ""
}

// SyncProject syncs one project now, waiting for another process's sync to
// finish within ctx. It returns false without error when there is nothing to
// sync: no hub, or the project does not sync.
func (s *Syncer) SyncProject(ctx context.Context, projectID string) (bool, error) {
	if !s.Joined() {
		return false, nil
	}

	_, enabled, err := s.state.Project(ctx, projectID)
	if err != nil || !enabled {
		return false, err
	}

	return true, s.locked(ctx, func(engine *Engine) error {
		_, err := engine.Sync(ctx, projectID)
		return err
	})
}

// SyncAll syncs every project this device syncs.
func (s *Syncer) SyncAll(ctx context.Context) error {
	if !s.Joined() {
		return nil
	}

	projects, err := s.state.Projects(ctx)
	if err != nil || len(projects) == 0 {
		return err
	}

	return s.locked(ctx, func(engine *Engine) error {
		var failures []error
		for _, value := range projects {
			if _, err := engine.Sync(ctx, value.ID); err != nil {
				failures = append(failures, fmt.Errorf("sync project %s: %w", value.ID, err))
			}
		}

		return errors.Join(failures...)
	})
}

func (s *Syncer) locked(ctx context.Context, action func(*Engine) error) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}

	lock := flock.New(filepath.Join(s.dir, "sync.lock"), flock.SetPermissions(0o600))
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("wait for another sync: %w", err)
	}
	if !locked {
		return errors.New("another sync is running")
	}
	defer func() { _ = lock.Unlock() }()

	hub, key, _, err := Connect(s.dir)
	if err != nil {
		return err
	}
	defer hub.Close()

	return action(NewEngine(s.exchange, s.state, hub, key))
}

// Nudge schedules a sync of the project shortly; nudges close together
// collapse into one sync.
func (s *Syncer) Nudge(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending[projectID] = true
	if s.timer != nil {
		// A timer that already fired is about to take the pending set,
		// this project included; only a waiting timer is pushed back.
		if s.timer.Stop() {
			s.timer.Reset(nudgeDelay)
		}
		return
	}

	s.running.Add(1)
	s.timer = time.AfterFunc(nudgeDelay, s.flushPending)
}

func (s *Syncer) flushPending() {
	defer s.running.Done()

	s.mu.Lock()
	projects := s.pending
	s.pending = map[string]bool{}
	s.timer = nil
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), nudgeTimeout)
	defer cancel()

	for projectID := range projects {
		_, err := s.SyncProject(ctx, projectID)
		s.report(err)
	}
}

// Flush runs pending nudges now and waits for them, within ctx. Processes
// call it before they exit so the last writes reach the hub.
func (s *Syncer) Flush(ctx context.Context) {
	s.mu.Lock()
	if s.timer != nil && s.timer.Stop() {
		s.timer = nil
		s.mu.Unlock()
		s.flushPending()
	} else {
		s.mu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// report warns once per distinct failure, so an offline laptop does not
// fill the log, and again after a sync succeeds.
func (s *Syncer) report(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err == nil {
		s.lastWarn = ""
		return
	}
	if err.Error() == s.lastWarn {
		return
	}

	s.lastWarn = err.Error()
	s.warn(err)
}

// WarnTo writes sync failures to a log stream.
func WarnTo(output io.Writer) func(error) {
	return func(err error) {
		fmt.Fprintf(output, "%s istok sync: %v\n", time.Now().Format(time.RFC3339), err)
	}
}

// Run syncs every project at the interval until ctx ends.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		syncCtx, cancel := context.WithTimeout(ctx, interval)
		err := s.SyncAll(syncCtx)
		cancel()
		if ctx.Err() == nil {
			s.report(err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
