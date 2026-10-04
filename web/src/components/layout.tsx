import { Link, Outlet, useLocation } from "react-router"

import { Logo } from "@/components/logo"
import { SearchPalette } from "@/components/search-palette"
import { ThemeToggle } from "@/components/theme-toggle"
import { cn } from "@/lib/utils"

type NavItem = {
  label: string
  to: string
  isActive: (pathname: string) => boolean
}

// A run page belongs to a project's task, but people reach it from the feed
// more often, so /runs/* highlights Runs.
const navItems: NavItem[] = [
  {
    label: "Projects",
    to: "/",
    isActive: (pathname) =>
      pathname === "/" || pathname.startsWith("/projects"),
  },
  {
    label: "Runs",
    to: "/runs",
    isActive: (pathname) => pathname.startsWith("/runs"),
  },
]

export function Layout() {
  const { pathname } = useLocation()

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-6 px-6">
          <div className="flex items-center gap-6">
            <Link
              to="/"
              className="flex items-center gap-2 font-semibold tracking-tight"
            >
              <Logo size={24} />
              Istok
            </Link>
            <nav className="flex items-center gap-1 text-sm">
              {navItems.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  className={cn(
                    "rounded-md px-2.5 py-1.5 text-muted-foreground transition-colors hover:text-foreground",
                    item.isActive(pathname) &&
                      "bg-muted font-medium text-foreground"
                  )}
                >
                  {item.label}
                </Link>
              ))}
            </nav>
          </div>
          <div className="flex items-center gap-2">
            <SearchPalette />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-8">
        <Outlet />
      </main>
    </div>
  )
}
