package cli

import (
	"context"

	runmodel "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/worktree"
)

// changedChecks returns the IDs of checks Istok ran whose code has changed
// since: completing the task on them would fail with evidence_stale. Checks
// not tied to code are skipped, and so is a check whose tree Git cannot
// fingerprint now: completion reports that case itself.
func changedChecks(ctx context.Context, root string, checks []runmodel.Validation) map[string]bool {
	changed := map[string]bool{}
	digests := map[string]string{}

	for _, check := range checks {
		if check.Source != runmodel.ValidationSourceExecuted || check.TreeState != runmodel.TreeStateBound {
			continue
		}

		digest, seen := digests[check.TreeCommit]
		if !seen {
			value, err := worktree.Digest(ctx, root, check.TreeCommit)
			if err != nil {
				value = ""
			}

			digests[check.TreeCommit] = value
			digest = value
		}

		if digest != "" && digest != check.TreeDigest {
			changed[check.ID] = true
		}
	}

	return changed
}
