import { useEffect, useState } from "react"

// useNow returns the current time and refreshes it periodically, so render
// stays pure while relative states such as lease expiry stay current.
export function useNow(intervalMs = 30000): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(timer)
  }, [intervalMs])

  return now
}
