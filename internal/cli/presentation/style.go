package presentation

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	brandColor    = "#5CC8FF"
	successColor  = "#5FD38D"
	warningColor  = "#F2B84B"
	metadataColor = "#B7A5FF"
	dangerColor   = "#FF6B7A"
	neutralColor  = "#7C8494"
)

var (
	brandStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(brandColor))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(successColor))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(warningColor))

	metadataStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(metadataColor))

	dangerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(dangerColor))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(neutralColor))

	sectionLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(brandColor))

	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(brandColor)).
				Bold(true)

	primaryTitleStyle = lipgloss.NewStyle().Bold(true)

	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(neutralColor))

	keyStyle = lipgloss.NewStyle().
			Width(12).
			Foreground(lipgloss.Color(neutralColor))
)

func SectionTitle(value string) string {
	return sectionTitleStyle.Render(value)
}

func PrimaryTitle(value string) string {
	return primaryTitleStyle.Render(value)
}

func Brand(value string) string {
	return brandStyle.Render(value)
}

func BrandStrong(value string) string {
	return brandStyle.Bold(true).Render(value)
}

func Neutral(value string) string {
	return mutedStyle.Render(value)
}

func Metadata(value string) string {
	return metadataStyle.Render(value)
}

func Path(value string) string {
	return mutedStyle.Render(value)
}

func Warning(value string) string {
	return warningStyle.Render(value)
}

func Header(value string) string {
	return headerStyle.Render(value)
}

func RailPrefix() string {
	return "  " + sectionLineStyle.Render("│") + " "
}

func Divider() string {
	return sectionLineStyle.Render("│")
}

func RailLine(value string) string {
	return RailPrefix() + value
}

func Key(label string) string {
	return keyStyle.Render(label + ":")
}

func StyledStatus(value string) string {
	return statusStyle(value).Render(fmt.Sprintf("%s %s", statusGlyph(value), statusLabel(value)))
}

func StyledNumber(value string) string {
	return warningStyle.Render(value)
}

func StyledTags(value string) string {
	return mutedStyle.Render(value)
}

func statusStyle(state string) lipgloss.Style {
	switch normalizeState(state) {
	case "active", "ready", "success", "succeeded", "passed", "done":
		return successStyle
	case "in_progress", "running", "updating":
		return brandStyle
	case "blocked", "warning", "attention", "cancelled", "stale", "degraded":
		return warningStyle
	case "open", "inactive", "disabled", "never_indexed":
		return mutedStyle
	case "archived":
		return metadataStyle
	case "failed", "error", "deleted":
		return dangerStyle
	default:
		return mutedStyle
	}
}

func statusGlyph(state string) string {
	switch normalizeState(state) {
	case "active":
		return "●"
	case "ready":
		return "◆"
	case "updating":
		return "◐"
	case "never_indexed":
		return "○"
	case "stale", "degraded":
		return "!"
	case "in_progress", "running":
		return "◐"
	case "open", "inactive", "disabled":
		return "○"
	case "done", "success", "succeeded", "passed":
		return "✓"
	case "cancelled", "abandoned":
		return "−"
	case "blocked", "warning", "attention":
		return "!"
	case "archived":
		return "◌"
	case "failed", "error", "deleted":
		return "×"
	default:
		return "?"
	}
}

func statusLabel(state string) string {
	switch normalizeState(state) {
	case "active":
		return "ACTIVE"
	case "ready":
		return "READY"
	case "in_progress":
		return "IN PROGRESS"
	case "running":
		return "RUNNING"
	case "updating":
		return "UPDATING"
	case "never_indexed":
		return "NEVER INDEXED"
	case "stale":
		return "STALE"
	case "degraded":
		return "DEGRADED"
	case "open":
		return "OPEN"
	case "inactive":
		return "INACTIVE"
	case "disabled":
		return "DISABLED"
	case "done":
		return "DONE"
	case "success":
		return "SUCCESS"
	case "succeeded":
		return "SUCCEEDED"
	case "passed":
		return "PASSED"
	case "cancelled":
		return "CANCELLED"
	case "abandoned":
		return "ABANDONED"
	case "blocked", "warning", "attention":
		return "BLOCKED"
	case "archived":
		return "ARCHIVED"
	case "failed":
		return "FAILED"
	case "error":
		return "ERROR"
	case "deleted":
		return "DELETED"
	default:
		return "UNKNOWN"
	}
}

func normalizeState(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, " ", "_")))
}
