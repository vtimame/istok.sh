import { useState } from "react"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useAbandonRun } from "@/hooks/mutations"
import { formatRelative } from "@/lib/format"

type AbandonRunDialogProps = {
  runId: string
  revision: number
  actorName: string
  heartbeatAt?: string
  size?: "sm" | "default"
}

// AbandonRunDialog closes a stale run after confirmation. The dialog stays
// open until the server answers, so conflicts are shown in place.
export function AbandonRunDialog({
  runId,
  revision,
  actorName,
  heartbeatAt,
  size = "sm",
}: AbandonRunDialogProps) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState("")
  const abandon = useAbandonRun()

  // mutateAsync rather than mutate callbacks: the refetch after success can
  // remove the row that renders this dialog, and per-call callbacks do not run
  // once the component unmounts. Errors stay visible through abandon.error.
  const submit = async () => {
    try {
      await abandon.mutateAsync({ runId, expectedRevision: revision, reason })
    } catch {
      return
    }

    setOpen(false)
    setReason("")
    toast.success("Run abandoned", {
      description: "The task is free for another agent to claim.",
    })
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) {
          abandon.reset()
        }
      }}
    >
      <AlertDialogTrigger asChild>
        <Button
          variant="outline"
          size={size}
          onClick={(event) => event.stopPropagation()}
        >
          Abandon
        </Button>
      </AlertDialogTrigger>
      {/* React events bubble through portals; keep clicks and Enter inside the
          dialog from reaching clickable rows that render it. */}
      <AlertDialogContent
        onClick={(event) => event.stopPropagation()}
        onKeyDown={(event) => event.stopPropagation()}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>Abandon this run?</AlertDialogTitle>
          <AlertDialogDescription>
            {actorName} stopped sending heartbeats
            {heartbeatAt ? ` ${formatRelative(heartbeatAt)}` : ""}. Abandoning
            closes the run and releases the task so another agent can claim it.
            The reason is recorded in the task history.
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="flex flex-col gap-2">
          <Label htmlFor={`abandon-reason-${runId}`}>Reason (optional)</Label>
          <Textarea
            id={`abandon-reason-${runId}`}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder="The agent stopped sending heartbeats."
            rows={3}
          />
        </div>

        {abandon.error && (
          <p className="text-sm text-destructive">{abandon.error.message}</p>
        )}

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <Button
            variant="destructive"
            onClick={submit}
            disabled={abandon.isPending}
          >
            {abandon.isPending ? "Abandoning…" : "Abandon run"}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
