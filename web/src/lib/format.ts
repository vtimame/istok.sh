const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
})

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })

export function formatDateTime(value?: string): string {
  return value ? dateTime.format(new Date(value)) : "—"
}

export function formatRelative(value?: string): string {
  if (!value) {
    return "—"
  }

  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000)
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ]

  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return relative.format(Math.round(seconds / size), unit)
    }
  }

  return relative.format(seconds, "second")
}

export function formatDuration(ms?: number): string {
  if (ms === undefined) {
    return "—"
  }
  if (ms < 1000) {
    return `${ms} ms`
  }

  const seconds = ms / 1000
  return seconds < 60 ? `${seconds.toFixed(1)} s` : `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`
}

export function formatBytes(bytes?: number): string {
  if (bytes === undefined) {
    return "—"
  }

  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KiB`
}
