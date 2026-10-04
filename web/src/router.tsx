import { createBrowserRouter } from "react-router"

import { Layout } from "@/components/layout"
import { RouteError } from "@/components/route-error"
import { KnowledgePage } from "@/pages/knowledge-page"
import { ProjectsPage } from "@/pages/projects-page"
import { RunPage } from "@/pages/run-page"
import { TaskPage } from "@/pages/task-page"
import { TasksPage } from "@/pages/tasks-page"

export const router = createBrowserRouter([
  {
    path: "/",
    Component: Layout,
    ErrorBoundary: RouteError,
    children: [
      { index: true, Component: ProjectsPage },
      { path: "projects/:projectId", Component: TasksPage },
      { path: "projects/:projectId/tasks/:number", Component: TaskPage },
      { path: "projects/:projectId/knowledge", Component: KnowledgePage },
      { path: "runs/:runId", Component: RunPage },
    ],
  },
])
