package presentation

import (
	"strings"
	"testing"
)

func TestStyledStatusRecognizesRunAndValidationStates(t *testing.T) {
	for state, label := range map[string]string{
		"running":   "RUNNING",
		"succeeded": "SUCCEEDED",
		"passed":    "PASSED",
		"cancelled": "CANCELLED",
		"abandoned": "ABANDONED",
		"disabled":  "DISABLED",
	} {
		t.Run(state, func(t *testing.T) {
			if rendered := StyledStatus(state); !strings.Contains(rendered, label) || strings.Contains(rendered, "UNKNOWN") {
				t.Fatalf("StyledStatus(%q) = %q", state, rendered)
			}
		})
	}
}

func TestStyledStatusRecognizesIndexLifecycleStates(t *testing.T) {
	for state, label := range map[string]string{
		"never_indexed": "NEVER INDEXED",
		"updating":      "UPDATING",
		"stale":         "STALE",
		"degraded":      "DEGRADED",
	} {
		t.Run(state, func(t *testing.T) {
			if rendered := StyledStatus(state); !strings.Contains(rendered, label) || strings.Contains(rendered, "UNKNOWN") {
				t.Fatalf("StyledStatus(%q) = %q", state, rendered)
			}
		})
	}
}
