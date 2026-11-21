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

// nudgeMaxDelay bounds how long steady writes can postpone a push.
const nudgeMaxDelay = 30 * time.Second

// nudgeTimeout bounds a background sync started by a nudge.
const nudgeTimeout = time.Minute

// Syncer runs syncs for long-lived processes and hooks: MCP servers and agent
// hooks. Processes share the database, so a file lock in
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
	timer    *time.Timer
	first    time.Time
	active   []chan struct{}
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

// Lock waits within ctx for the sync lock of dir, shared by every process
// of this device, and returns its release.
func Lock(ctx context.Context, dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	lock := flock.New(filepath.Join(dir, "sync.lock"), flock.SetPermissions(0o600))
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("wait for another sync: %w", err)
	}
	if !locked {
		return nil, errors.New("another sync is running")
	}

	return func() { _ = lock.Unlock() }, nil
}

// WithEngine runs action with an engine connected to the hub, under the
// sync lock.
func (s *Syncer) WithEngine(ctx context.Context, action func(*Engine) error) error {
	return s.locked(ctx, action)
}

func (s *Syncer) locked(ctx context.Context, action func(*Engine) error) error {
	unlock, err := Lock(ctx, s.dir)
	if err != nil {
		return err
	}
	defer unlock()

	hub, key, _, err := Connect(s.dir)
	if err != nil {
		return err
	}
	defer hub.Close()

	return action(NewEngine(s.exchange, s.state, hub, key))
}

// Nudge schedules a sync of the project shortly; nudges close together
// collapse into one sync, delayed at most nudgeMaxDelay after the first.
func (s *Syncer) Nudge(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending[projectID] = true
	if s.timer != nil {
		// A timer that already fired is about to take the pending set, this
		// project included; a waiting one is pushed back within the limit.
		if time.Since(s.first) < nudgeMaxDelay && s.timer.Stop() {
			s.timer.Reset(nudgeDelay)
		}
		return
	}

	s.first = time.Now()
	s.timer = time.AfterFunc(nudgeDelay, s.flushPending)
}

func (s *Syncer) flushPending() {
	s.mu.Lock()
	projects := s.pending
	s.pending = map[string]bool{}
	s.timer = nil
	done := make(chan struct{})
	s.active = append(s.active, done)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		for index, value := range s.active {
			if value == done {
				s.active = append(s.active[:index], s.active[index+1:]...)
				break
			}
		}
		s.mu.Unlock()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), nudgeTimeout)
	defer cancel()

	for projectID := range projects {
		_, err := s.SyncProject(ctx, projectID)
		s.report(err)
	}
}

// Flush runs pending nudges now and waits for running ones, within ctx.
// Processes call it before they exit so the last writes reach the hub.
func (s *Syncer) Flush(ctx context.Context) {
	s.mu.Lock()
	waiting := s.timer != nil && s.timer.Stop()
	s.mu.Unlock()
	if waiting {
		s.flushPending()
	}

	s.mu.Lock()
	running := append([]chan struct{}(nil), s.active...)
	s.mu.Unlock()

	for _, done := range running {
		select {
		case <-done:
		case <-ctx.Done():
			return
		}
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
