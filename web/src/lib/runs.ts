// RunTiming is the part of a run that both the full Run and the feed carry.
type RunTiming = {
  status: string
  started_at: string
  finished_at?: string
  heartbeat_at?: string
  expires_at?: string
}

// isRunStale reports an active run whose lease expired: the agent stopped
// sending heartbeats without finishing the run.
export function isRunStale(
  run: Pick<RunTiming, "status" | "expires_at">,
  now: number
): boolean {
  return (
    run.status === "active" &&
    run.expires_at !== undefined &&
    new Date(run.expires_at).getTime() < now
  )
}

// runDurationMs measures a finished run to its end, a stale run to its last
// heartbeat and a live run to now.
export function runDurationMs(run: RunTiming, now: number): number {
  const end = run.finished_at
    ? new Date(run.finished_at).getTime()
    : isRunStale(run, now) && run.heartbeat_at
      ? new Date(run.heartbeat_at).getTime()
      : now

  return end - new Date(run.started_at).getTime()
}
