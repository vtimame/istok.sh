import { useSyncExternalStore } from "react"

// LiveState tracks the server event stream. Queries poll slowly while it is
// connected and fall back to frequent polling while it is not.
export type LiveState = "connecting" | "live" | "offline"

let state: LiveState = "connecting"
const listeners = new Set<() => void>()

export function setLiveState(next: LiveState) {
  if (next === state) {
    return
  }

  state = next
  listeners.forEach((listener) => listener())
}

export function getLiveState(): LiveState {
  return state
}

export function useLiveState(): LiveState {
  return useSyncExternalStore((listener) => {
    listeners.add(listener)
    return () => listeners.delete(listener)
  }, getLiveState)
}

// pollInterval keeps a safety net under the event stream: events trigger
// refetches, polling only catches anything a dropped stream missed.
export function pollInterval(): number {
  return state === "live" ? 60_000 : 5_000
}
