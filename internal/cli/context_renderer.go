package cli

import (
	"fmt"
	"strings"

	"charm.land/glamour/v2"

	"github.com/vtimame/istok.sh/internal/cli/presentation"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

const contextMarkdownWrapWidth = 100

func renderContextValue(value any, terminal bool) string {
	switch typed := value.(type) {
	case contextMarkdownView:
		return renderContextMarkdown(typed, terminal)
	case contextView:
		return renderContextShow(typed)
	case contextListView:
		return renderContextList(typed)
	default:
		return fmt.Sprintln(value)
	}
}

func renderContextMarkdown(value contextMarkdownView, terminal bool) string {
	source := renderContextMarkdownSource(value)
	style := "notty"
	if terminal {
		style = "dark"
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
		glamour.WithWordWrap(contextMarkdownWrapWidth),
	)
	if err != nil {
		return source
	}

	rendered, err := renderer.Render(source)
	if err != nil {
		return source
	}

	return rendered
}

func renderContextMarkdownSource(value contextMarkdownView) string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("# Project context: %s\n\n", value.Project.Name))
	output.WriteString(fmt.Sprintf("%d context record%s.\n\n", len(value.Records), pluralizeCount(len(value.Records))))

	if len(value.Records) == 0 {
		output.WriteString("*No context records.*\n")
		return output.String()
	}

	for index, record := range value.Records {
		if index > 0 {
			output.WriteString("\n---\n\n")
		}

		output.WriteString(fmt.Sprintf("## %s\n", normalizeContextTitleForMarkdown(record.Title)))
		output.WriteString(fmt.Sprintf("- **Kind:** `%s`\n", record.Kind))
		output.WriteString(fmt.Sprintf("- **Source:** `%s`\n", record.Source))
		output.WriteString(fmt.Sprintf("- **Visibility:** `%s`\n", record.Visibility))
		output.WriteString(fmt.Sprintf("- **Sensitivity:** `%s`\n", record.Sensitivity))
		if record.Kind == contextmodel.KindInstruction {
			output.WriteString(fmt.Sprintf("- **Enabled:** `%t`\n- **Priority:** `%s`\n- **Scope:** `%s`\n", *record.Enabled, *record.Priority, *record.Scope))
		}
		output.WriteString(fmt.Sprintf("- **Revision:** `r%d`\n", record.Revision))
		output.WriteString(fmt.Sprintf("- **Record ID:** `%s`\n", record.ID))

		tags := strings.Join(record.Tags, ", ")
		if tags == "" {
			tags = "—"
		}
		output.WriteString(fmt.Sprintf("- **Tags:** %s\n\n", tags))

		body := strings.TrimSpace(record.Body)
		if body == "" {
			output.WriteString("*No body content.*\n")
			continue
		}
		output.WriteString(body + "\n")
	}

	return output.String()
}

func pluralizeCount(count int) string {
	if count == 1 {
		return ""
	}

	return "s"
}

func normalizeContextTitleForMarkdown(title string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(title), " "))
}

func renderContextList(value contextListView) string {
	if len(value.Records) == 0 {
		return fmt.Sprintf("%s %s %s\n", presentation.Brand(value.Project.Name), presentation.Divider(), presentation.Warning("no context records"))
	}

	rows := make([][]string, 0, len(value.Records))
	for _, record := range value.Records {
		state := "active"
		policy := "—"
		if record.Kind == contextmodel.KindInstruction {
			policy = fmt.Sprintf("%s/%s", *record.Priority, *record.Scope)
			if !*record.Enabled {
				state = "disabled"
			}
		}
		if record.DeletedAt != nil {
			state = "archived"
		}
		rows = append(rows, []string{
			presentation.Warning(record.ID),
			string(record.Kind),
			presentation.StyledStatus(state),
			policy,
			presentation.Metadata(fmt.Sprint(record.Revision)),
			record.Title,
			presentation.StyledTags(strings.Join(record.Tags, ", ")),
		})
	}

	return fmt.Sprintf("%s %s %s\n\n%s\n", presentation.Brand(value.Project.Name), presentation.Divider(), presentation.Warning(fmt.Sprintf("%d context records", len(value.Records))), presentation.RenderUnboundedTable([]string{"ID", "KIND", "STATE", "POLICY", "REVISION", "TITLE", "TAGS"}, rows))
}

func renderContextShow(value contextView) string {
	record := value.Record
	state := "active"
	if record.DeletedAt != nil {
		state = "archived"
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf(
		"%s %s %s %s %s\n\n",
		presentation.Brand(value.Project.Name),
		presentation.Neutral("·"),
		presentation.Neutral("context"),
		presentation.Neutral("·"),
		presentation.Warning(value.Record.ID),
	))
	output.WriteString(presentation.PrimaryTitle(record.Title))
	output.WriteString("\n\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("State"), presentation.StyledStatus(state))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Revision"), presentation.Metadata(fmt.Sprintf("r%d", record.Revision)))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Kind"), record.Kind)))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Source"), record.Source)))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Visibility"), record.Visibility)))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Sensitivity"), record.Sensitivity)))
	output.WriteString("\n")
	if record.Kind == contextmodel.KindInstruction {
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %t (%s, %s)", presentation.Key("Instruction"), *record.Enabled, *record.Priority, *record.Scope)))
		output.WriteString("\n")
	}
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Tags"), presentation.StyledTags(renderContextTags(record.Tags)))))
	output.WriteString("\n\n")

	output.WriteString(presentation.SectionTitle("Body"))
	output.WriteString("\n")
	writeContextBody(&output, record.Body)
	output.WriteString("\n")

	output.WriteString(presentation.SectionTitle("History"))
	output.WriteString("\n")
	if len(value.Events) > 0 {
		writeContextHistory(&output, value.Events)
	} else {
		output.WriteString(presentation.RailLine("—"))
		output.WriteString("\n")
	}

	return output.String()
}

func renderContextTags(tags []string) string {
	if len(tags) == 0 {
		return "—"
	}

	return strings.Join(tags, ", ")
}

func writeContextBody(output *strings.Builder, body string) {
	if strings.TrimSpace(body) == "" {
		output.WriteString(presentation.RailLine("—"))
		output.WriteString("\n")
		return
	}

	for _, line := range wrapText(body, 88) {
		output.WriteString(presentation.RailLine(line))
		output.WriteString("\n")
	}
}

func writeContextHistory(output *strings.Builder, events []contextmodel.ContextEvent) {
	hasDetails := false
	for _, event := range events {
		if strings.TrimSpace(event.Body) != "" {
			hasDetails = true
			break
		}
	}

	rows := make([][]string, 0, len(events))
	for _, event := range events {
		row := []string{
			event.CreatedAt.UTC().Format("2006-01-02 15:04Z"),
			presentation.Metadata(event.Type),
			formatContextActor(event.Actor),
			presentation.Metadata(fmt.Sprintf("r%d", event.RecordRevision)),
		}
		if hasDetails {
			row = append(row, showText(event.Body))
		}
		rows = append(rows, row)
	}

	for _, line := range strings.Split(strings.TrimSuffix(presentation.RenderRows(rows), "\n"), "\n") {
		output.WriteString(presentation.RailLine(strings.TrimSpace(line)))
		output.WriteString("\n")
	}
}

func formatContextActor(actor contextmodel.ActorSnapshot) string {
	if name := strings.TrimSpace(actor.Name); name != "" {
		return name
	}
	if kind := strings.TrimSpace(actor.Kind); kind != "" {
		return kind
	}

	return "—"
}
