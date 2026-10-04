package knowledge

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBuildBriefingIsBoundedAndExcludesBodies(t *testing.T) {
	values := make([]CatalogItem, 100)
	for i := range values {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		values[i] = CatalogItem{ID: id.String(), Revision: 1, Kind: KindDecision, Title: "Decision", Summary: strings.Repeat("summary ", 80), Snippet: "body must not be copied", ContentHash: strings.Repeat("a", 64)}
	}

	briefing, metadata := BuildBriefing(values)
	if len(briefing) > BriefingLimit || metadata.UsedBytes > BriefingBudgetBytes || !metadata.Truncated {
		t.Fatalf("briefing is not bounded: len=%d metadata=%+v", len(briefing), metadata)
	}
	if metadata.CandidateCount != len(values) || metadata.SelectedCount != len(briefing) {
		t.Fatalf("briefing counts = %+v", metadata)
	}
	for _, item := range briefing {
		if strings.Contains(item.Summary, "body must not be copied") {
			t.Fatalf("briefing leaked body: %+v", item)
		}
	}
}

func TestRepositoryProvenanceRejectsPathsOutsideProject(t *testing.T) {
	for _, path := range []string{"/tmp/secret.md", "../secret.md", "."} {
		err := (Provenance{Type: ProvenanceRepository, ID: path}).Validate()
		if ErrorCode(err) != CodeInvalid {
			t.Fatalf("path %q error = %v", path, err)
		}
	}
	if err := (Provenance{Type: ProvenanceRepository, ID: "docs/auth.md", Detail: "abc123"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
