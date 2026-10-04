// Run-related task events store machine references in their body (see
// internal/storage/runrepo): parseEventBody splits them from the human text.
//
//   claimed       "<run id>" or JSON {run_id, without_retrieval, override_reason, ...}
//   run_finished  "<run id>\n<result summary>"
//   run_abandoned "run_id=<run id>\n<reason>"
//   completed     "<completion id>\n<note>" (the completion id is not shown)

export type ParsedEvent = {
  runId?: string
  text: string
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

function splitFirstLine(body: string): [string, string] {
  const newline = body.indexOf("\n")
  return newline === -1
    ? [body.trim(), ""]
    : [body.slice(0, newline).trim(), body.slice(newline + 1).trim()]
}

type ClaimBody = {
  run_id?: string
  without_retrieval?: boolean
  override_reason?: string
  context_override_reason?: string
}

function parseClaim(body: string): ParsedEvent {
  const trimmed = body.trim()
  if (UUID.test(trimmed)) {
    return { runId: trimmed, text: "" }
  }

  try {
    const value = JSON.parse(trimmed) as ClaimBody
    const notes = [
      value.without_retrieval && value.override_reason
        ? `Claimed without code retrieval: ${value.override_reason}`
        : "",
      value.context_override_reason
        ? `All context included: ${value.context_override_reason}`
        : "",
    ].filter(Boolean)

    return { runId: value.run_id, text: notes.join("\n\n") }
  } catch {
    return { text: body }
  }
}

export function parseEventBody(type: string, body: string): ParsedEvent {
  switch (type) {
    case "claimed":
      return parseClaim(body)

    case "run_finished": {
      const [first, rest] = splitFirstLine(body)
      return UUID.test(first) ? { runId: first, text: rest } : { text: body }
    }

    case "run_abandoned": {
      const [first, rest] = splitFirstLine(body)
      const runId = first.replace(/^run_id=/, "")
      return UUID.test(runId) ? { runId, text: rest } : { text: body }
    }

    case "completed": {
      const [first, rest] = splitFirstLine(body)
      return UUID.test(first) ? { text: rest } : { text: body }
    }

    default:
      return { text: body }
  }
}
