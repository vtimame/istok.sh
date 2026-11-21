package replica

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/protocol"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
	"github.com/vtimame/istok.sh/internal/storage/syncrepo"
)

const (
	// coordinationTimeout bounds how long a task creation or a claim waits
	// for the hub.
	coordinationTimeout = 2 * time.Second

	// coordinationBackoff skips the hub for a while after it was out of
	// reach, so a device without a network does not wait on every call.
	coordinationBackoff = 30 * time.Second
)

// Coordinator asks the hub for task numbers and leases while tasks are
// created and claimed. Without a hub, for projects that do not sync and when
// the hub is out of reach, it steps aside and the device works locally; the
// next sync reconciles.
type Coordinator struct {
	dir      string
	exchange *exchangerepo.Repository
	state    *syncrepo.Repository

	mu        sync.Mutex
	skipUntil time.Time
	now       func() time.Time
}

func NewCoordinator(dir string, exchange *exchangerepo.Repository, state *syncrepo.Repository) *Coordinator {
	return &Coordinator{dir: dir, exchange: exchange, state: state, now: time.Now}
}

// ReserveNumber claims a number for a new task. Unconfirmed tasks of the
// project claim theirs first, so the new number cannot collide with them.
func (c *Coordinator) ReserveNumber(ctx context.Context, projectID, taskID string) (int64, error) {
	var number int64
	err := c.withEngine(ctx, projectID, func(ctx context.Context, engine *Engine) error {
		reserved, err := engine.reconcileNumbers(ctx, projectID, taskID, nil)
		number = reserved

		return err
	})
	if err != nil {
		return 0, nil
	}

	return number, nil
}

// AcquireLease takes the task's lease on the hub for a run of this device.
// The run is remembered either way, so the next sync takes the lease when
// the hub was out of reach now.
func (c *Coordinator) AcquireLease(ctx context.Context, projectID, taskID, runID string, ttl time.Duration) error {
	synced, err := c.state.Synced(ctx, projectID)
	if err != nil || !synced {
		return err
	}

	var holder *protocol.LeaseHolder
	err = c.withEngine(ctx, projectID, func(ctx context.Context, engine *Engine) error {
		results, err := engine.hub.Leases(ctx, protocol.LeasesRequest{ProjectID: projectID,
			Acquire: []protocol.LeaseClaim{{TaskID: taskID, RunID: runID, TTLSeconds: ttlSeconds(ttl)}}})
		if err != nil {
			return err
		}
		if !results[0].Granted {
			holder = results[0].Holder
		}

		return nil
	})

	if err == nil && holder != nil {
		return run.NewError(run.CodeActiveRunExists, "%s", describeHolder(holder))
	}

	return c.state.HoldLease(ctx, projectID, taskID, runID)
}

// ReleaseLease gives up a lease taken for a claim that failed.
func (c *Coordinator) ReleaseLease(ctx context.Context, projectID, taskID, runID string) {
	_ = c.state.DropLease(ctx, projectID, taskID, runID)

	_ = c.withEngine(ctx, projectID, func(ctx context.Context, engine *Engine) error {
		_, err := engine.hub.Leases(ctx, protocol.LeasesRequest{ProjectID: projectID, Release: []string{taskID}})
		return err
	})
}

func describeHolder(holder *protocol.LeaseHolder) string {
	message := fmt.Sprintf("the task is being worked on from device %s", holder.DeviceName)
	if expires, err := time.Parse(time.RFC3339Nano, holder.ExpiresAt); err == nil {
		message += fmt.Sprintf(" (its lease runs until %s unless renewed)", expires.Local().Format("15:04"))
	}

	return message + "; pick another task or wait"
}

// errSkipped means the hub was not asked.
var errSkipped = errors.New("hub coordination skipped")

// withEngine runs an action against the hub within coordinationTimeout. It
// returns errSkipped when the project does not sync, the device has not
// joined a hub, or the hub was out of reach recently.
func (c *Coordinator) withEngine(ctx context.Context, projectID string, action func(context.Context, *Engine) error) error {
	synced, err := c.state.Synced(ctx, projectID)
	if err != nil {
		return err
	}
	if !synced || c.skipping() {
		return errSkipped
	}

	hub, key, _, err := Connect(c.dir)
	if err != nil {
		return errSkipped
	}
	defer hub.Close()

	ctx, cancel := context.WithTimeout(ctx, coordinationTimeout)
	defer cancel()

	err = action(ctx, NewEngine(c.exchange, c.state, hub, key))

	var hubError *protocol.Error
	if err != nil && !errors.As(err, &hubError) {
		c.backOff()
	}

	return err
}

func (c *Coordinator) skipping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now().Before(c.skipUntil)
}

func (c *Coordinator) backOff() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.skipUntil = c.now().Add(coordinationBackoff)
}
