import { Link, Outlet } from "react-router"

import { Logo } from "@/components/logo"
import { SearchPalette } from "@/components/search-palette"
import { ThemeToggle } from "@/components/theme-toggle"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { hasToken } from "@/lib/api"

export function Layout() {
  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-6">
          <Link to="/" className="flex items-center gap-2 font-semibold tracking-tight">
            <Logo size={24} />
            Istok
          </Link>
          <div className="flex items-center gap-2">
            <SearchPalette />
            <ThemeToggle />
          </div>
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
