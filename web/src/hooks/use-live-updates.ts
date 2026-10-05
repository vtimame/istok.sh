import { useEffect } from "react"
import { useQueryClient } from "@tanstack/react-query"

import { setLiveState } from "@/lib/live"

// At most one refetch per interval: during continuous agent activity events
// arrive every server poll, and this keeps the read load at the 1/s level
// measured as harmless for agent writes (tools/uiload).
const MIN_REFRESH_INTERVAL_MS = 1000

// useLiveUpdates subscribes to /api/v1/events and refetches the queries on
// screen whenever the database changes.
export function useLiveUpdates() {
  const queryClient = useQueryClient()

  useEffect(() => {
    const source = new EventSource("/api/v1/events")
    let lastRefresh = 0
    let timer: number | undefined

    const refresh = () => {
      if (timer !== undefined) {
        return
      }

      const wait = Math.max(
        0,
        lastRefresh + MIN_REFRESH_INTERVAL_MS - Date.now()
      )
      timer = window.setTimeout(() => {
        timer = undefined
        lastRefresh = Date.now()
        void queryClient.invalidateQueries({ refetchType: "active" })
      }, wait)
    }

    // A (re)connected stream may have missed changes, so it refreshes too.
    source.addEventListener("ready", () => {
      setLiveState("live")
      refresh()
    })
    source.addEventListener("change", refresh)
    // EventSource reconnects by itself; until then polling takes over.
    source.onerror = () => setLiveState("offline")

    return () => {
      window.clearTimeout(timer)
      source.close()
      setLiveState("connecting")
    }
  }, [queryClient])
}
