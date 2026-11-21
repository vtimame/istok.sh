package replica

import (
	"context"
	"math"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/protocol"
	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
)

// LostLease is a task another device took while a run of this device held
// it offline.
type LostLease struct {
	TaskID     string `json:"task_id"`
	RunID      string `json:"run_id"`
	DeviceName string `json:"device_name"`
}

// reconcileNumbers claims the numbers of every unconfirmed task of the
// project, plus a number for newTaskID when it is set, and applies the
// hub's answer locally. It returns the number for newTaskID.
func (e *Engine) reconcileNumbers(ctx context.Context, projectID, newTaskID string, result *Result) (int64, error) {
	unconfirmed, err := e.exchange.UnconfirmedTasks(ctx, projectID)
	if err != nil {
		return 0, err
	}

	claims := make([]protocol.NumberClaim, 0, len(unconfirmed)+1)
	for _, value := range unconfirmed {
		claims = append(claims, protocol.NumberClaim{TaskID: value.TaskID, Number: value.Number})
	}
	if newTaskID != "" {
		claims = append(claims, protocol.NumberClaim{TaskID: newTaskID})
	}

	var reserved int64
	for start := 0; start < len(claims); start += protocol.MaxNumberClaims {
		batch := claims[start:min(start+protocol.MaxNumberClaims, len(claims))]

		numbers, err := e.hub.Numbers(ctx, protocol.NumbersRequest{ProjectID: projectID, Claims: batch})
		if err != nil {
			return 0, err
		}

		assigned := make([]exchangerepo.TaskNumber, 0, len(numbers))
		for _, value := range numbers {
			if value.TaskID == newTaskID {
				reserved = value.Number
				continue
			}
			assigned = append(assigned, exchangerepo.TaskNumber{TaskID: value.TaskID, Number: value.Number})
		}

		renumbered, err := e.exchange.ApplyNumbers(ctx, projectID, assigned, Actor)
		if err != nil {
			return 0, err
		}
		if result != nil {
			result.Renumbered = append(result.Renumbered, renumbered...)
		}
	}

	return reserved, nil
}

// renewLeases extends on the hub the leases of runs claimed on this device
// and releases those of runs that ended.
func (e *Engine) renewLeases(ctx context.Context, projectID string, result *Result) error {
	held, err := e.state.HeldLeases(ctx, projectID)
	if err != nil || len(held) == 0 {
		return err
	}

	now := time.Now()
	request := protocol.LeasesRequest{ProjectID: projectID}
	var ended []heldRun
	for _, value := range held {
		ttl := value.Expires.Sub(now)
		if !value.Active || ttl <= 0 {
			request.Release = append(request.Release, value.TaskID)
			ended = append(ended, heldRun{taskID: value.TaskID, runID: value.RunID})
			continue
		}

		request.Acquire = append(request.Acquire, protocol.LeaseClaim{TaskID: value.TaskID, RunID: value.RunID, TTLSeconds: ttlSeconds(ttl)})
	}

	results, err := e.hub.Leases(ctx, request)
	if err != nil {
		return err
	}

	for _, value := range ended {
		if err := e.state.DropLease(ctx, projectID, value.taskID, value.runID); err != nil {
			return err
		}
	}

	for index, value := range results {
		if value.Granted || value.Holder == nil {
			continue
		}

		claim := request.Acquire[index]
		if err := e.state.DropLease(ctx, projectID, claim.TaskID, claim.RunID); err != nil {
			return err
		}
		result.LostLeases = append(result.LostLeases, LostLease{TaskID: claim.TaskID, RunID: claim.RunID, DeviceName: value.Holder.DeviceName})
	}

	return nil
}

type heldRun struct {
	taskID, runID string
}

// ttlSeconds rounds a lease duration up to whole seconds within the hub's
// bounds.
func ttlSeconds(ttl time.Duration) int64 {
	seconds := int64(math.Ceil(ttl.Seconds()))

	return min(max(seconds, 1), protocol.MaxLeaseTTL)
}
