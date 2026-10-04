import type { Run } from "@/lib/types"

// isRunStale reports an active run whose lease expired: the agent stopped
// sending heartbeats without finishing the run.
export function isRunStale(run: Run, now: number): boolean {
  return run.status === "active" && new Date(run.expires_at).getTime() < now
}

// runDurationMs measures a finished run to its end, a stale run to its last
// heartbeat and a live run to now.
export function runDurationMs(run: Run, now: number): number {
  const end = run.finished_at
    ? new Date(run.finished_at).getTime()
    : isRunStale(run, now)
      ? new Date(run.heartbeat_at).getTime()
      : now

  return end - new Date(run.started_at).getTime()
}
