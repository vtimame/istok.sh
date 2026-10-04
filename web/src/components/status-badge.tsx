import { Badge } from "@/components/ui/badge"

type Tone = "default" | "secondary" | "destructive" | "outline"

const tones: Record<string, Tone> = {
  open: "outline",
  blocked: "destructive",
  done: "secondary",
  active: "default",
  running: "default",
  succeeded: "secondary",
  passed: "secondary",
  failed: "destructive",
  cancelled: "outline",
  abandoned: "outline",
  current: "secondary",
  draft: "outline",
  superseded: "outline",
}

export function StatusBadge({ status }: { status: string }) {
  return <Badge variant={tones[status] ?? "outline"}>{status}</Badge>
}
