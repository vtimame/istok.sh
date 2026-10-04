import { useState } from "react"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useDeleteProject } from "@/hooks/mutations"
import type { Project } from "@/lib/types"

type DeleteProjectDialogProps = {
  project: Project
  open: boolean
  onOpenChange: (open: boolean) => void
}

// DeleteProjectDialog asks for the project name before a soft delete, so a
// stray click cannot remove a project.
export function DeleteProjectDialog({
  project,
  open,
  onOpenChange,
}: DeleteProjectDialogProps) {
  const navigate = useNavigate()
  const [confirmation, setConfirmation] = useState("")
  const remove = useDeleteProject()
  const confirmed = confirmation.trim() === project.name

  const close = (next: boolean) => {
    onOpenChange(next)
    if (!next) {
      setConfirmation("")
      remove.reset()
    }
  }

  // mutateAsync rather than mutate callbacks: the refetch after success drops
  // the project, which unmounts this dialog before per-call callbacks run.
  const submit = async () => {
    try {
      await remove.mutateAsync(project)
    } catch {
      return
    }

    toast.success(`Deleted ${project.name}`, {
      description: `Restore it with: istok project restore ${project.id}`,
    })
    navigate("/")
  }

  return (
    <AlertDialog open={open} onOpenChange={close}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {project.name}?</AlertDialogTitle>
          <AlertDialogDescription>
            The project disappears from Istok and its directory is released for
            a new <code>istok init</code>. Tasks, runs and history are kept, and{" "}
            <code>istok project restore</code> brings the project back. Files in
            the repository are not touched.
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="flex flex-col gap-2">
          <Label htmlFor="delete-project-confirmation">
            Type <span className="font-mono font-semibold">{project.name}</span>{" "}
            to confirm
          </Label>
          <Input
            id="delete-project-confirmation"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            onKeyDown={(event) =>
              event.key === "Enter" && confirmed && submit()
            }
            autoComplete="off"
          />
        </div>

        {remove.error && (
          <p className="text-sm text-destructive">{remove.error.message}</p>
        )}

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <Button
            variant="destructive"
            onClick={submit}
            disabled={!confirmed || remove.isPending}
          >
            {remove.isPending ? "Deleting…" : "Delete project"}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
