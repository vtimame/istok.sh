import { useMutation, useQueryClient } from "@tanstack/react-query"

import { apiSend } from "@/lib/api"
import type { Project, Run } from "@/lib/types"

// Mutations refresh every cached query: runs, tasks and project stats all
// change together, and the local API is cheap to re-read.

export function useAbandonRun() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: {
      runId: string
      expectedRevision: number
      reason: string
    }) =>
      apiSend<Run>("POST", `/runs/${input.runId}/abandon`, {
        expected_revision: input.expectedRevision,
        reason: input.reason,
      }),
    onSuccess: () => queryClient.invalidateQueries(),
  })
}

export function useDeleteProject() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (project: Project) =>
      apiSend<Project>("DELETE", `/projects/${project.id}`, {
        expected_revision: project.revision,
      }),
    onSuccess: () => queryClient.invalidateQueries(),
  })
}
