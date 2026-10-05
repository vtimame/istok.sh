import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useBindProject } from "@/hooks/mutations"
import type { Project } from "@/lib/types"

type BindFolderDialogProps = {
  project: Project
  open: boolean
  onOpenChange: (open: boolean) => void
}

// BindFolderDialog attaches a folder on this device to a project, for example
// one that arrived by import. Istok never guesses the folder.
export function BindFolderDialog({
  project,
  open,
  onOpenChange,
}: BindFolderDialogProps) {
  const [path, setPath] = useState("")
  const bind = useBindProject()
  const ready = path.trim() !== ""

  const close = (next: boolean) => {
    onOpenChange(next)
    if (!next) {
      setPath("")
      bind.reset()
    }
  }

  const submit = async () => {
    let bound: Project
    try {
      bound = await bind.mutateAsync({ project, path: path.trim() })
    } catch {
      return
    }

    toast.success(`Bound ${project.name}`, {
      description: bound.root?.canonical_path,
    })
    close(false)
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {project.root ? "Change folder" : "Bind folder"}
          </DialogTitle>
          <DialogDescription>
            Point {project.name} at its repository on this device. Clone the
            repository first if it is not here yet.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-2">
          <Label htmlFor="bind-folder-path">Folder</Label>
          <Input
            id="bind-folder-path"
            value={path}
            placeholder={`~/src/${project.name}`}
            onChange={(event) => setPath(event.target.value)}
            onKeyDown={(event) => event.key === "Enter" && ready && submit()}
            autoComplete="off"
            spellCheck={false}
            className="font-mono"
          />
          <p className="text-xs text-muted-foreground">
            An absolute path or one starting with <code>~/</code>. The same as{" "}
            <code>istok project rebind {project.id.slice(0, 8)} PATH</code>.
          </p>
        </div>

        {bind.error && (
          <p className="text-sm text-destructive">{bind.error.message}</p>
        )}

        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button onClick={submit} disabled={!ready || bind.isPending}>
            {bind.isPending ? "Binding…" : "Bind folder"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
