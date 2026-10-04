import { Fragment, type ReactNode } from "react"
import { Link } from "react-router"

import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"

export type Crumb = { label: string; to?: string }

type PageHeaderProps = {
  crumbs: Crumb[]
  title: ReactNode
  actions?: ReactNode
}

export function PageHeader({ crumbs, title, actions }: PageHeaderProps) {
  return (
    <div className="mb-6 flex flex-col gap-2">
      {crumbs.length > 1 && (
        <Breadcrumb>
          <BreadcrumbList>
            {crumbs.map((crumb, index) => (
              <Fragment key={`${crumb.label}-${index}`}>
                {index > 0 && <BreadcrumbSeparator />}
                <BreadcrumbItem>
                  {crumb.to ? (
                    <BreadcrumbLink asChild>
                      <Link to={crumb.to}>{crumb.label}</Link>
                    </BreadcrumbLink>
                  ) : (
                    <BreadcrumbPage>{crumb.label}</BreadcrumbPage>
                  )}
                </BreadcrumbItem>
              </Fragment>
            ))}
          </BreadcrumbList>
        </Breadcrumb>
      )}

      <div className="flex items-center justify-between gap-4">
        <h1 className="min-w-0 text-2xl font-semibold tracking-tight">
          {title}
        </h1>
        {actions}
      </div>
    </div>
  )
}
