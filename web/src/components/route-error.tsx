import { Link, isRouteErrorResponse, useRouteError } from "react-router"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"

export function RouteError() {
  const error = useRouteError()

  const message = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : error instanceof Error
      ? error.message
      : "Unknown error"

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4 px-6 py-16">
      <Alert variant="destructive">
        <AlertTitle>Something went wrong</AlertTitle>
        <AlertDescription>{message}</AlertDescription>
      </Alert>
      <Button variant="outline" asChild className="self-start">
        <Link to="/">Back to projects</Link>
      </Button>
    </div>
  )
}
