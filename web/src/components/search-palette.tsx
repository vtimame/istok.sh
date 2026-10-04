import { useEffect, useState } from "react"
import { BookOpen, FolderGit2, ListTodo, Search } from "lucide-react"
import { useNavigate, useParams } from "react-router"

import { InlineCode } from "@/components/markdown"
import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  useHealth,
  useKnowledgeSearch,
  useProjects,
  useTasks,
} from "@/hooks/queries"
import { useDebouncedValue } from "@/hooks/use-debounced-value"
import { shortenPath } from "@/lib/format"
import { list } from "@/lib/types"

// SearchPalette is the global Ctrl/⌘+K search over projects, the current
// project's tasks and its knowledge (full-text on the server).
export function SearchPalette() {
  const navigate = useNavigate()
  const { projectId = "" } = useParams()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const debouncedQuery = useDebouncedValue(query.trim(), 250)

  const projects = useProjects()
  const health = useHealth()
  const tasks = useTasks(projectId)
  const knowledge = useKnowledgeSearch(projectId, debouncedQuery)

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault()
        setOpen((current) => !current)
      }
    }

    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [])

  const go = (path: string) => {
    setOpen(false)
    setQuery("")
    navigate(path)
  }

  const isMac = navigator.platform.toLowerCase().includes("mac")

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="w-56 justify-between text-muted-foreground"
        onClick={() => setOpen(true)}
      >
        <span className="flex items-center gap-2">
          <Search />
          Search…
        </span>
        <kbd className="font-mono text-xs">{isMac ? "⌘K" : "Ctrl K"}</kbd>
      </Button>

      <CommandDialog
        open={open}
        onOpenChange={setOpen}
        title="Search"
        description="Search projects, tasks and knowledge"
        className="sm:max-w-2xl"
      >
        <Command>
          <CommandInput
            placeholder={
              projectId
                ? "Search tasks, knowledge, projects…"
                : "Search projects…"
            }
            value={query}
            onValueChange={setQuery}
          />
          <CommandList>
            <CommandEmpty>Nothing found.</CommandEmpty>

            {projectId && (
              <CommandGroup heading="Tasks">
                {list(tasks.data ?? null).map((task) => (
                  <CommandItem
                    key={task.id}
                    value={`#${task.number} ${task.title}`}
                    onSelect={() =>
                      go(`/projects/${projectId}/tasks/${task.number}`)
                    }
                  >
                    <ListTodo />
                    <span className="font-mono text-muted-foreground">
                      #{task.number}
                    </span>
                    <span className="truncate">
                      <InlineCode text={task.title} />
                    </span>
                    <span className="ml-auto">
                      <StatusBadge status={task.status} />
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {projectId && debouncedQuery.length >= 2 && (
              <CommandGroup heading="Knowledge">
                {list(knowledge.data ?? null).map((item) => (
                  <CommandItem
                    key={item.id}
                    value={`knowledge ${item.id} ${item.title}`}
                    keywords={[debouncedQuery]}
                    onSelect={() => go(`/projects/${projectId}/knowledge`)}
                  >
                    <BookOpen />
                    <span className="truncate">{item.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            <CommandGroup heading="Projects">
              {list(projects.data ?? null).map((project) => (
                <CommandItem
                  key={project.id}
                  value={`project ${project.name}`}
                  onSelect={() => go(`/projects/${project.id}`)}
                >
                  <FolderGit2 />
                  <span>{project.name}</span>
                  <span className="ml-auto truncate font-mono text-xs text-muted-foreground">
                    {project.root
                      ? shortenPath(
                          project.root.canonical_path,
                          health.data?.home
                        )
                      : ""}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </CommandDialog>
    </>
  )
}
