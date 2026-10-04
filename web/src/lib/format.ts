// The UI copy is English, so dates use an English locale instead of the
// browser default to avoid mixed-language strings.
const LOCALE = "en-US"

const dateTime = new Intl.DateTimeFormat(LOCALE, {
  dateStyle: "medium",
  timeStyle: "short",
})

const relative = new Intl.RelativeTimeFormat(LOCALE, { numeric: "auto" })

export function formatDateTime(value?: string): string {
  return value ? dateTime.format(new Date(value)) : "—"
}

export function formatRelative(value?: string): string {
  if (!value) {
    return "—"
  }

  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000)
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 31536000],
    ["month", 2592000],
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ]

  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return relative.format(Math.round(seconds / size), unit)
    }
  }

  return "just now"
}

export function formatDuration(ms?: number): string {
  if (ms === undefined) {
    return "—"
  }
  if (ms < 1000) {
    return `${ms} ms`
  }

  const seconds = Math.round(ms / 1000)
  if (seconds < 60) {
    return `${(ms / 1000).toFixed(1)} s`
  }

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    return `${minutes}m ${seconds % 60}s`
  }

  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}

export function formatBytes(bytes?: number): string {
  if (bytes === undefined) {
    return "—"
  }

  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KiB`
}

// shortenPath replaces the home directory with ~ and collapses the middle of
// long paths, keeping the leaf directories that identify a project.
export function shortenPath(path: string, home?: string): string {
  let value = path
  if (home && (value === home || value.startsWith(`${home}/`))) {
    value = `~${value.slice(home.length)}`
  }

  const segments = value.split("/")
  if (segments.length <= 5) {
    return value
  }

  return [...segments.slice(0, 2), "…", ...segments.slice(-2)].join("/")
}
