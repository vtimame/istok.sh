import { useInfiniteQuery, useQuery } from "@tanstack/react-query"

import { apiGet } from "@/lib/api"
import { list } from "@/lib/types"
import type {
  FeedItem,
  Health,
  KnowledgeItem,
  List,
  Project,
  Run,
  RunShow,
  TaskListItem,
  TaskShow,
} from "@/lib/types"

export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: () => apiGet<Health>("/health"),
    staleTime: Infinity,
  })
}

export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: () => apiGet<List<Project>>("/projects").then(list),
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
    queryFn: () =>
      apiGet<List<TaskListItem>>(`/projects/${projectId}/tasks`).then(list),
    enabled: projectId !== "",
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
    queryFn: () =>
      apiGet<List<Run>>(`/projects/${projectId}/runs${query}`).then(list),
    enabled: taskId !== "",
  })
}

export function useRun(runId: string) {
  return useQuery({
    queryKey: ["runs", runId],
    queryFn: () => apiGet<RunShow>(`/runs/${runId}`),
  })
}

export function useKnowledgeSearch(projectId: string, query: string) {
  return useQuery({
    queryKey: ["projects", projectId, "knowledge", "search", query],
    queryFn: () =>
      apiGet<List<KnowledgeItem>>(
        `/projects/${projectId}/knowledge?q=${encodeURIComponent(query)}`
      ).then(list),
    enabled: projectId !== "" && query.length >= 2,
    refetchInterval: false,
    placeholderData: (previous) => previous,
  })
}

export function useKnowledge(projectId: string) {
  return useQuery({
    queryKey: ["projects", projectId, "knowledge"],
    queryFn: () =>
      apiGet<List<KnowledgeItem>>(`/projects/${projectId}/knowledge`).then(
        list
      ),
  })
}

export const FEED_PAGE_SIZE = 50

export function useRunFeed(
  projectId: string,
  statuses: string[],
  lease: "" | "live" | "expired"
) {
  return useInfiniteQuery({
    queryKey: ["runs", "feed", projectId, statuses, lease],
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({
        limit: String(FEED_PAGE_SIZE),
        offset: String(pageParam),
      })
      if (projectId) {
        params.set("project", projectId)
      }
      if (lease) {
        params.set("lease", lease)
      }
      statuses.forEach((status) => params.append("status", status))

      return apiGet<List<FeedItem>>(`/runs?${params}`).then(list)
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage, pages) =>
      lastPage.length < FEED_PAGE_SIZE
        ? undefined
        : pages.length * FEED_PAGE_SIZE,
  })
}
