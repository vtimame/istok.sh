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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, init)

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

export function apiGet<T>(path: string): Promise<T> {
  return request<T>(path)
}

// apiSend sends a JSON body; the server only accepts mutations as
// application/json, which a cross-site form cannot produce.
export function apiSend<T>(
  method: "POST" | "DELETE",
  path: string,
  body: unknown
): Promise<T> {
  return request<T>(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}
