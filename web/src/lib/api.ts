const TOKEN_KEY = "istok.token"

// The server prints a URL with #token=...; the fragment never reaches the
// server, so the token is moved into sessionStorage and removed from the URL.
function captureToken(): string | null {
  const hash = new URLSearchParams(window.location.hash.slice(1))
  const fromHash = hash.get("token")

  if (fromHash) {
    sessionStorage.setItem(TOKEN_KEY, fromHash)
    history.replaceState(null, "", window.location.pathname + window.location.search)
    return fromHash
  }

  return sessionStorage.getItem(TOKEN_KEY)
}

const token = captureToken()

export function hasToken(): boolean {
  return token !== null
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

type Envelope<T> = {
  schema_version: string
  result?: T
  error?: { code: string; message: string }
}

export async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    headers: { Authorization: `Bearer ${token ?? ""}` },
  })

  const body = (await response.json().catch(() => null)) as Envelope<T> | null
  if (!response.ok || !body || body.error) {
    throw new ApiError(
      response.status,
      body?.error?.code ?? "http_error",
      body?.error?.message ?? `Request failed with status ${response.status}`
    )
  }

  return body.result as T
}
