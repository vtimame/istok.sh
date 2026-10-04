import { useQuery } from "@tanstack/react-query"

import { apiGet } from "@/lib/api"
import type {
  KnowledgeItem,
  Project,
  Run,
  RunShow,
  TaskListItem,
  TaskShow,
} from "@/lib/types"

export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: () => apiGet<{ version: string; commit: string }>("/health"),
    staleTime: Infinity,
  })
}

export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: () => apiGet<Project[]>("/projects"),
  })
}

export function useProject(projectId: string) {
  const projects = useProjects()

  return {
    ...projects,
    data: projects.data?.find((project) => project.id === projectId),
  }
}

export function useTasks(projectId: string) {
  return useQuery({
    queryKey: ["projects", projectId, "tasks"],
    queryFn: () => apiGet<TaskListItem[]>(`/projects/${projectId}/tasks`),
  })
}

export function useTask(projectId: string, number: string) {
  return useQuery({
    queryKey: ["projects", projectId, "tasks", number],
    queryFn: () => apiGet<TaskShow>(`/projects/${projectId}/tasks/${number}`),
  })
}

export function useRuns(projectId: string, taskId?: string) {
  const query = taskId ? `?task_id=${encodeURIComponent(taskId)}` : ""

  return useQuery({
    queryKey: ["projects", projectId, "runs", taskId ?? "all"],
    queryFn: () => apiGet<Run[]>(`/projects/${projectId}/runs${query}`),
    enabled: taskId !== "",
  })
}

export function useRun(runId: string) {
  return useQuery({
    queryKey: ["runs", runId],
    queryFn: () => apiGet<RunShow>(`/runs/${runId}`),
  })
}

export function useKnowledge(projectId: string) {
  return useQuery({
    queryKey: ["projects", projectId, "knowledge"],
    queryFn: () => apiGet<KnowledgeItem[]>(`/projects/${projectId}/knowledge`),
  })
}
