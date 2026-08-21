package cli

import (
	"strings"
	"testing"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/project"
)

func TestContextMarkdownRendererUsesANSIForTerminalOutput(t *testing.T) {
	got := renderContextValue(contextMarkdownView{
		Project: project.Project{Name: "Demo"},
		Records: []contextmodel.ProjectContextRecord{{
			ID:          "01a024ff-7832-763d-b2c8-a4c51d914e41",
			Revision:    1,
			Kind:        contextmodel.KindNote,
			Title:       "Code sample",
			Body:        "```go\nfmt.Println(\"hello\")\n```",
			Source:      contextmodel.SourceUser,
			Visibility:  contextmodel.VisibilityShared,
			Sensitivity: contextmodel.SensitivityNormal,
		}},
	}, true)

	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("terminal markdown output should contain ANSI: %q", got)
	}
	if !strings.Contains(stripANSI(got), "fmt.Println(\"hello\")") {
		t.Fatalf("terminal markdown output lost code block content: %q", got)
	}
}

func TestContextListRendererDoesNotTruncateTitles(t *testing.T) {
	title := "CRM API: текущие правила переноса legacy create_new_client в NestJS"
	got := renderContextValue(contextListView{
		Project: project.Project{Name: "Demo"},
		Records: []contextmodel.ProjectContextRecord{{
			ID:          "01a024ff-7832-763d-b2c8-a4c51d914e41",
			Revision:    1,
			Kind:        contextmodel.KindInstruction,
			Title:       title,
			Source:      contextmodel.SourceUser,
			Visibility:  contextmodel.VisibilityShared,
			Sensitivity: contextmodel.SensitivityNormal,
			Enabled:     pointerTo(true),
			Priority:    pointerTo(contextmodel.PriorityHigh),
			Scope:       pointerTo(contextmodel.ScopeProject),
		}},
	}, true)
	plain := stripANSI(got)

	mustContain(t, plain, "TAGS")
	mustContain(t, plain, title)
	if strings.Contains(plain, "≈") {
		t.Fatalf("context list output was truncated: %q", plain)
	}
}

func pointerTo[T any](value T) *T {
	return &value
}
