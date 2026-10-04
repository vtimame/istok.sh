import { Link, Outlet } from "react-router"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { useHealth } from "@/hooks/queries"
import { hasToken } from "@/lib/api"

export function Layout() {
  const health = useHealth()

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-6">
          <Link to="/" className="font-semibold tracking-tight">
            Istok
          </Link>
          <span className="font-mono text-xs text-muted-foreground">
            {health.data?.version}
          </span>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-8">
        {!hasToken() && (
          <Alert variant="destructive" className="mb-6">
            <AlertTitle>Missing access token</AlertTitle>
            <AlertDescription>
              Open the URL printed by <code>istok ui</code>; it carries the token for this session.
            </AlertDescription>
          </Alert>
        )}
        <Outlet />
      </main>
    </div>
  )
}
