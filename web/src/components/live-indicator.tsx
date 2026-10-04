import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useLiveState } from "@/lib/live"
import { cn } from "@/lib/utils"

const labels = {
  live: "Live: updates arrive as agents write",
  connecting: "Connecting to live updates…",
  offline: "Live updates disconnected; refreshing every 5 seconds",
}

export function LiveIndicator() {
  const state = useLiveState()

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          role="status"
          aria-label={labels[state]}
          className="flex size-8 items-center justify-center"
        >
          <span
            className={cn(
              "size-2 rounded-full",
              state === "live" && "bg-emerald-500",
              state === "connecting" && "animate-pulse bg-muted-foreground/50",
              state === "offline" && "bg-amber-500"
            )}
          />
        </span>
      </TooltipTrigger>
      <TooltipContent>{labels[state]}</TooltipContent>
    </Tooltip>
  )
}
