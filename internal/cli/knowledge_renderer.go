package cli

import (
	"fmt"
	"strings"

	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

func renderKnowledgeValue(value any) string {
	switch typed := value.(type) {
	case knowledgeView:
		return renderKnowledgeShow(typed)
	case knowledgeCatalogView:
		return renderKnowledgeCatalog(typed)
	case knowledgeSupersedeView:
		return fmt.Sprintf("%s\n%s superseded by %s\n", presentation.Brand(typed.Project.Name), typed.Superseded.ID, typed.Replacement.ID)
	case knowledgeExportView:
		return fmt.Sprintf("%s\nexported %s to %s (%s)\n", presentation.Brand(typed.Project.Name), typed.ItemID, typed.Output, typed.Format)
	default:
		return fmt.Sprintln(value)
	}
}

func renderKnowledgeCatalog(value knowledgeCatalogView) string {
	if len(value.Items) == 0 {
		return fmt.Sprintf("%s %s %s\n", presentation.Brand(value.Project.Name), presentation.Divider(), presentation.Warning("no knowledge items"))
	}
	rows := make([][]string, 0, len(value.Items))
	for _, item := range value.Items {
		reviewed := "—"
		if item.ReviewedAt != nil {
			reviewed = item.ReviewedAt.UTC().Format("2006-01-02")
		}
		rows = append(rows, []string{presentation.Warning(item.ID), string(item.Kind), presentation.StyledStatus(string(item.Status)), reviewed, fmt.Sprintf("r%d", item.Revision), item.Title, item.Summary, item.Snippet, presentation.StyledTags(strings.Join(item.Tags, ", "))})
	}
	return fmt.Sprintf("%s %s %s\n\n%s\n", presentation.Brand(value.Project.Name), presentation.Divider(), presentation.Warning(fmt.Sprintf("%d knowledge items · offset %d", len(value.Items), value.Offset)), presentation.RenderUnboundedTable([]string{"ID", "KIND", "STATUS", "REVIEWED", "REVISION", "TITLE", "SUMMARY", "SNIPPET", "TAGS"}, rows))
}

func renderKnowledgeShow(value knowledgeView) string {
	item := value.Item
	var out strings.Builder
	out.WriteString(fmt.Sprintf("%s · knowledge · %s\n\n", presentation.Brand(value.Project.Name), presentation.Warning(item.ID)))
	out.WriteString(presentation.PrimaryTitle(item.Title) + "\n\n")
	out.WriteString(presentation.RailLine(fmt.Sprintf("%s %s · %s · r%d", presentation.Key("State"), item.Kind, presentation.StyledStatus(string(item.Status)), item.Revision)) + "\n")
	out.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Summary"), item.Summary)) + "\n")
	out.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Hash"), item.ContentHash)) + "\n")
	reviewed := "—"
	if item.ReviewedAt != nil {
		reviewed = item.ReviewedAt.UTC().Format("2006-01-02 15:04Z")
	}
	out.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Reviewed"), reviewed)) + "\n")
	out.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Tags"), strings.Join(item.Tags, ", "))) + "\n\n")
	out.WriteString(presentation.SectionTitle("Body") + "\n")
	writeContextBody(&out, item.Body)
	if len(item.Provenance) > 0 {
		out.WriteString("\n" + presentation.SectionTitle("Provenance") + "\n")
		for _, source := range item.Provenance {
			out.WriteString(presentation.RailLine(fmt.Sprintf("%s:%s", source.Type, source.ID)) + "\n")
		}
	}
	return out.String()
}

var _ = knowledge.Snippet
