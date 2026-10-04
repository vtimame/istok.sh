import { Link } from "react-router"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"

// ProjectMissing covers stale links to deleted or unknown projects.
export function ProjectMissing() {
  return (
    <div className="flex max-w-xl flex-col gap-4">
      <Alert>
        <AlertTitle>Project not found</AlertTitle>
        <AlertDescription>
          It may have been deleted. <code>istok project list --deleted</code>{" "}
          shows deleted projects, and <code>istok project restore</code> brings
          one back.
        </AlertDescription>
      </Alert>
      <Button variant="outline" asChild className="self-start">
        <Link to="/">Back to projects</Link>
      </Button>
    </div>
  )
}
