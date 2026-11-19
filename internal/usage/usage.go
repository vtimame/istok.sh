// Package usage models the device-local log of MCP tool calls and the report
// built from it. The log answers how agents actually use Istok: which tools
// they call, how far a session gets through the workflow and which errors
// they hit. It never holds tool arguments or results.
package usage

import (
	"sort"
	"time"
)

// Retention is how long the log keeps a call.
const Retention = 90 * 24 * time.Hour

// Outcome tells how a tool call ended.
type Outcome string

const (
	OK    Outcome = "ok"
	Error Outcome = "error"
)

// Call is one MCP tool call.
type Call struct {
	ID            string
	SessionID     string
	ClientName    string
	ClientVersion string
	ActorID       string
	ProjectID     string
	Tool          string
	StartedAt     time.Time
	Duration      time.Duration
	Outcome       Outcome
	ErrorCode     string
}

// Filter selects the calls a report covers. An empty ProjectID covers every
// project, including calls made outside a registered project.
type Filter struct {
	Since     time.Time
	ProjectID string
}

// ToolCount is a call count of one tool in one session.
type ToolCount struct {
	SessionID  string
	ClientName string
	Tool       string
	Outcome    Outcome
	ErrorCode  string
	Calls      int
}

// Step is one stage of the Istok workflow. A session reaches the step when it
// called any of the step's tools at least once.
type Step struct {
	Name  string
	Tools []string
}

// Funnel lists the workflow stages in order.
var Funnel = []Step{
	{Name: "project", Tools: []string{"project_current"}},
	{Name: "find or create task", Tools: []string{"task_list", "task_ready", "task_show", "task_create"}},
	{Name: "claim", Tools: []string{"task_claim"}},
	{Name: "progress", Tools: []string{"task_progress", "task_comment", "task_block"}},
	{Name: "heartbeat", Tools: []string{"run_heartbeat"}},
	{Name: "validate", Tools: []string{"run_validate", "validation_record", "task_evidence"}},
	{Name: "finish run", Tools: []string{"run_finish", "task_done"}},
	{Name: "complete task", Tools: []string{"task_complete", "task_done"}},
}

// Report summarizes the calls selected by a filter.
type Report struct {
	Since    time.Time    `json:"since"`
	Sessions int          `json:"sessions"`
	Calls    int          `json:"calls"`
	Failed   int          `json:"failed"`
	Clients  []NamedCount `json:"clients"`
	Funnel   []StepCount  `json:"funnel"`
	Errors   []NamedCount `json:"errors"`
	Tools    []ToolTotal  `json:"tools"`
}

// StepCount is how many sessions reached a workflow step.
type StepCount struct {
	Step     string `json:"step"`
	Sessions int    `json:"sessions"`
}

// NamedCount counts something by name, such as calls by error code.
type NamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ToolTotal counts the calls of one tool.
type ToolTotal struct {
	Tool     string `json:"tool"`
	Calls    int    `json:"calls"`
	Failed   int    `json:"failed"`
	Sessions int    `json:"sessions"`
}

// Summarize builds a report from per-session tool counts.
func Summarize(since time.Time, counts []ToolCount) Report {
	sessionTools := map[string]map[string]bool{}
	sessionClients := map[string]string{}
	errors := map[string]int{}
	tools := map[string]*ToolTotal{}
	toolSessions := map[string]map[string]bool{}
	report := Report{Since: since}

	for _, count := range counts {
		if sessionTools[count.SessionID] == nil {
			sessionTools[count.SessionID] = map[string]bool{}
		}
		sessionTools[count.SessionID][count.Tool] = true
		sessionClients[count.SessionID] = count.ClientName

		total := tools[count.Tool]
		if total == nil {
			total = &ToolTotal{Tool: count.Tool}
			tools[count.Tool] = total
			toolSessions[count.Tool] = map[string]bool{}
		}
		total.Calls += count.Calls
		toolSessions[count.Tool][count.SessionID] = true

		report.Calls += count.Calls
		if count.Outcome == Error {
			total.Failed += count.Calls
			report.Failed += count.Calls
			errors[count.ErrorCode] += count.Calls
		}
	}

	report.Sessions = len(sessionTools)
	report.Funnel = funnel(sessionTools)
	report.Errors = sortedCounts(errors)

	clients := map[string]int{}
	for _, name := range sessionClients {
		if name == "" {
			name = "unknown"
		}
		clients[name]++
	}
	report.Clients = sortedCounts(clients)

	report.Tools = make([]ToolTotal, 0, len(tools))
	for name, total := range tools {
		total.Sessions = len(toolSessions[name])
		report.Tools = append(report.Tools, *total)
	}
	sort.Slice(report.Tools, func(i, j int) bool {
		if report.Tools[i].Calls != report.Tools[j].Calls {
			return report.Tools[i].Calls > report.Tools[j].Calls
		}

		return report.Tools[i].Tool < report.Tools[j].Tool
	})

	return report
}

func funnel(sessionTools map[string]map[string]bool) []StepCount {
	steps := make([]StepCount, 0, len(Funnel))

	for _, step := range Funnel {
		reached := 0
		for _, called := range sessionTools {
			for _, tool := range step.Tools {
				if called[tool] {
					reached++
					break
				}
			}
		}
		steps = append(steps, StepCount{Step: step.Name, Sessions: reached})
	}

	return steps
}

func sortedCounts(values map[string]int) []NamedCount {
	result := make([]NamedCount, 0, len(values))
	for name, count := range values {
		result = append(result, NamedCount{Name: name, Count: count})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}

		return result[i].Name < result[j].Name
	})

	return result
}
