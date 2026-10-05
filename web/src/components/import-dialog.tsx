import { useState } from "react"
import { AlertTriangle, Upload } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useImportBundle } from "@/hooks/mutations"
import type { ImportReport } from "@/lib/types"

// The tables people care about, in reading order. Others (history rows,
// snapshot details, leases) are part of the import but not worth a line.
const summaryTables: [string, string][] = [
  ["tasks", "Tasks"],
  ["runs", "Runs"],
  ["validations", "Validations"],
  ["context_records", "Context"],
  ["knowledge_items", "Knowledge"],
]

// ImportDialog reads a bundle file, shows a dry run of the import and applies
// it only after confirmation, so nothing changes by picking the wrong file.
export function ImportDialog() {
  const [open, setOpen] = useState(false)
  const [fileName, setFileName] = useState("")
  const [bundle, setBundle] = useState<unknown>(null)
  const [readError, setReadError] = useState("")
  const preview = useImportBundle()
  const apply = useImportBundle()

  const reset = () => {
    setFileName("")
    setBundle(null)
    setReadError("")
    preview.reset()
    apply.reset()
  }

  const close = (next: boolean) => {
    setOpen(next)
    if (!next) {
      reset()
    }
  }

  const pick = async (file: File | undefined) => {
    reset()
    if (!file) {
      return
    }

    setFileName(file.name)
    let parsed: unknown
    try {
      parsed = JSON.parse(await file.text())
    } catch {
      setReadError("This file is not an Istok export: it is not valid JSON.")
      return
    }

    setBundle(parsed)
    preview.mutate({ bundle: parsed, dryRun: true })
  }

  const submit = async () => {
    let report: ImportReport
    try {
      report = await apply.mutateAsync({ bundle, dryRun: false })
    } catch {
      return
    }

    const unbound = report.projects.filter((project) => !project.bound)
    toast.success(
      `Imported ${report.projects.map((project) => project.name).join(", ")}`,
      {
        description:
          unbound.length > 0
            ? "Open the project and bind its folder on this device."
            : undefined,
      }
    )
    close(false)
  }

  const error = readError || preview.error?.message || apply.error?.message

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogTrigger asChild>
        <Button variant="outline">
          <Upload />
          Import
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Import projects</DialogTitle>
          <DialogDescription>
            Choose a file exported on another device with{" "}
            <code>istok export</code> or the Export button. You see what changes
            before anything is written.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-2">
          <Label htmlFor="import-file">Export file</Label>
          <Input
            id="import-file"
            type="file"
            accept=".json,application/json"
            onChange={(event) => pick(event.target.files?.[0])}
          />
        </div>

        {preview.isPending && (
          <p className="text-sm text-muted-foreground">Reading {fileName}…</p>
        )}

        {preview.data && <ImportSummary report={preview.data} />}

        {error && <p className="text-sm text-destructive">{error}</p>}

        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button onClick={submit} disabled={!preview.data || apply.isPending}>
            {apply.isPending ? "Importing…" : "Import"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ImportSummary({ report }: { report: ImportReport }) {
  const rows = summaryTables
    .map(([table, label]) => ({ label, counts: report.tables[table] }))
    .filter(({ counts }) => counts)

  return (
    <div className="flex flex-col gap-3 text-sm">
      <ul className="flex flex-col gap-1.5">
        {report.projects.map((project) => (
          <li key={project.id} className="flex items-center gap-2">
            <span className="truncate font-medium">{project.name}</span>
            <Badge variant="secondary">
              {project.created ? "New" : "Merge"}
            </Badge>
            {!project.bound && (
              <Badge variant="outline">No folder on this device</Badge>
            )}
          </li>
        ))}
      </ul>

      {rows.length > 0 && (
        <table className="w-full text-xs">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-1 text-left font-normal" />
              <th className="py-1 text-right font-normal">New</th>
              <th className="py-1 text-right font-normal">Updated</th>
              <th className="py-1 text-right font-normal">Same</th>
              <th className="py-1 text-right font-normal">Kept local</th>
            </tr>
          </thead>
          <tbody className="tabular-nums">
            {rows.map(({ label, counts }) => (
              <tr key={label} className="border-t">
                <td className="py-1">{label}</td>
                <td className="py-1 text-right">{counts.created}</td>
                <td className="py-1 text-right">{counts.updated}</td>
                <td className="py-1 text-right">{counts.unchanged}</td>
                <td className="py-1 text-right">{counts.kept_local}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {report.renumbered.length > 0 && (
        <div className="flex flex-col gap-1">
          <p className="text-muted-foreground">
            Tasks renumbered because the number was taken here:
          </p>
          <ul className="flex flex-col gap-0.5">
            {report.renumbered.map((item) => (
              <li key={item.task_id}>
                <span className="font-mono">
                  #{item.from} → #{item.to}
                </span>{" "}
                {item.title}
              </li>
            ))}
          </ul>
        </div>
      )}

      {report.conflicts.length > 0 && (
        <p className="text-muted-foreground">
          {report.conflicts.length} change
          {report.conflicts.length === 1 ? " was" : "s were"} made on both
          devices; the version on this device is kept.
        </p>
      )}

      {report.warnings.length > 0 && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>Check after import</AlertTitle>
          <AlertDescription>
            <ul className="flex list-disc flex-col gap-1 pl-4">
              {report.warnings.map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
