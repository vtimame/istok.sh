import type { ReactNode } from "react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"

type QueryStateProps = {
  isPending: boolean
  error: Error | null
  children: ReactNode
}

// QueryState renders a skeleton while loading and an alert on failure, so
// pages only describe the success state.
export function QueryState({ isPending, error, children }: QueryStateProps) {
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load data</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    )
  }

  if (isPending) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-8 w-1/3" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  return <>{children}</>
}
