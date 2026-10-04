package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vtimame/istok.sh/internal/contextpack"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

func TestKnowledgeCLIPullFirstLifecycleAndExplicitExport(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")
	before := directoryNames(t, root)
	body := "# Authentication\n\n" + strings.Repeat("catalog-intro ", 40) + "full-body-marker-that-must-not-appear-in-catalog"

	createdResult := executeAt(t, root, database,
		"knowledge", "add", "Authentication",
		"--summary", "Current authentication contract",
		"--body", body,
		"--kind", "architecture",
		"--tag", "auth",
		"--json",
	)
	if createdResult.err != nil {
		t.Fatal(createdResult.err)
	}
	created := decodeJSONResult[knowledgeView](t, createdResult.output).Item
	if created.Status != knowledge.StatusDraft || created.Revision != 1 || len(created.ContentHash) != 64 {
		t.Fatalf("created knowledge = %+v", created)
	}

	listedResult := executeAt(t, root, database, "knowledge", "list", "--json")
	if listedResult.err != nil {
		t.Fatal(listedResult.err)
	}
	if strings.Contains(listedResult.output, "full-body-marker") {
		t.Fatalf("catalog leaked full body: %s", listedResult.output)
	}
	listed := decodeJSONResult[knowledgeCatalogView](t, listedResult.output)
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID || listed.Items[0].Summary == "" {
		t.Fatalf("catalog = %+v", listed)
	}

	shown := executeAt(t, root, database, "knowledge", "read", created.ID, "--json")
	if shown.err != nil {
		t.Fatal(shown.err)
	}
	if decodeJSONResult[knowledgeView](t, shown.output).Item.Body != body {
		t.Fatalf("read did not return complete body: %s", shown.output)
	}

	reviewedResult := executeAt(t, root, database, "knowledge", "review", created.ID, "--expected-revision", "1", "--note", "Verified against code", "--json")
	if reviewedResult.err != nil {
		t.Fatal(reviewedResult.err)
	}
	reviewed := decodeJSONResult[knowledgeView](t, reviewedResult.output).Item
	if reviewed.ReviewedAt == nil || reviewed.Revision != 2 {
		t.Fatalf("reviewed = %+v", reviewed)
	}

	promotedResult := executeAt(t, root, database, "knowledge", "promote", created.ID, "--expected-revision", "2", "--json")
	if promotedResult.err != nil {
		t.Fatal(promotedResult.err)
	}
	promoted := decodeJSONResult[knowledgeView](t, promotedResult.output).Item
	if promoted.Status != knowledge.StatusCurrent || promoted.Revision != 3 {
		t.Fatalf("promoted = %+v", promoted)
	}
	if updatedCurrent := executeAt(t, root, database, "knowledge", "update", created.ID, "--expected-revision", "3", "--summary", "unreviewed edit", "--json"); updatedCurrent.err == nil {
		t.Fatal("current knowledge was mutable")
	}

	replacementResult := executeAt(t, root, database,
		"knowledge", "distill", "Authentication v2",
		"--summary", "Replacement authentication contract",
		"--body", "new body",
		"--kind", "architecture",
		"--source", "knowledge:"+created.ID+"@3",
		"--json",
	)
	if replacementResult.err != nil {
		t.Fatal(replacementResult.err)
	}
	replacement := decodeJSONResult[knowledgeView](t, replacementResult.output).Item
	if len(replacement.Provenance) != 1 || replacement.Status != knowledge.StatusDraft {
		t.Fatalf("replacement = %+v", replacement)
	}
	reviewedReplacement := executeAt(t, root, database, "knowledge", "review", replacement.ID, "--expected-revision", "1", "--note", "Verified replacement", "--json")
	if reviewedReplacement.err != nil {
		t.Fatal(reviewedReplacement.err)
	}

	supersededResult := executeAt(t, root, database, "knowledge", "supersede", created.ID, replacement.ID, "--expected-revision", "3", "--json")
	if supersededResult.err != nil {
		t.Fatal(supersededResult.err)
	}
	superseded := decodeJSONResult[knowledgeSupersedeView](t, supersededResult.output)
	if superseded.Superseded.Status != knowledge.StatusSuperseded || superseded.Replacement.Status != knowledge.StatusCurrent {
		t.Fatalf("supersession = %+v", superseded)
	}

	exportPath := filepath.Join(t.TempDir(), "authentication.md")
	exported := executeAt(t, root, database, "knowledge", "export", replacement.ID, "--output", exportPath, "--json")
	if exported.err != nil {
		t.Fatal(exported.err)
	}
	content, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "istok.knowledge-export.v1") || !strings.Contains(string(content), "# Authentication v2") {
		t.Fatalf("export = %s", content)
	}
	if repeated := executeAt(t, root, database, "knowledge", "export", replacement.ID, "--output", exportPath, "--json"); repeated.err == nil {
		t.Fatal("expected existing export destination to be refused")
	}
	if after := directoryNames(t, root); strings.Join(after, "\x00") != strings.Join(before, "\x00") {
		t.Fatalf("knowledge commands wrote into project root: before=%v after=%v", before, after)
	}
}

func TestKnowledgeCurrentCatalogIsBoundedInContextPreview(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")
	created := decodeJSONResult[knowledgeView](t, executeAt(t, root, database,
		"knowledge", "add", "Run knowledge", "--summary", "Small run briefing", "--body", strings.Repeat("secret-body ", 1000), "--json",
	).output).Item
	executeAt(t, root, database, "knowledge", "review", created.ID, "--expected-revision", "1", "--note", "test review", "--json")
	executeAt(t, root, database, "knowledge", "promote", created.ID, "--expected-revision", "2", "--json")
	createdTask := executeAt(t, root, database, "task", "create", "--title", "Use knowledge", "--json")
	if createdTask.err != nil {
		t.Fatal(createdTask.err)
	}

	previewResult := executeAt(t, root, database, "context", "preview", "1", "--without-retrieval", "--retrieval-override-reason", "test", "--json")
	if previewResult.err != nil {
		t.Fatal(previewResult.err)
	}
	preview := decodeJSONResultAtVersion[contextpack.Package](t, previewResult.output, "2")
	if preview.SchemaVersion != contextpack.SchemaVersion {
		t.Fatalf("context package schema = %s", preview.SchemaVersion)
	}
	if len(preview.Knowledge) != 1 || preview.Knowledge[0].ID != created.ID || preview.Metadata.Knowledge.UsedBytes > knowledge.BriefingBudgetBytes {
		t.Fatalf("knowledge briefing = %+v metadata=%+v", preview.Knowledge, preview.Metadata.Knowledge)
	}
	encoded := previewResult.output
	if strings.Contains(encoded, "secret-body") || strings.Contains(encoded, "provenance") {
		t.Fatalf("preview leaked pull-only knowledge: %s", encoded)
	}
}

func TestKnowledgeReviewIsRequiredAndInvalidatedByEditing(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")
	created := decodeJSONResult[knowledgeView](t, executeAt(t, root, database,
		"knowledge", "add", "Reviewed draft", "--summary", "Initial summary", "--json",
	).output).Item

	if promoted := executeAt(t, root, database, "knowledge", "promote", created.ID, "--expected-revision", "1", "--json"); promoted.err == nil {
		t.Fatal("unreviewed draft was promoted")
	}
	reviewed := decodeJSONResult[knowledgeView](t, executeAt(t, root, database,
		"knowledge", "review", created.ID, "--expected-revision", "1", "--note", "reviewed", "--json",
	).output).Item
	updated := decodeJSONResult[knowledgeView](t, executeAt(t, root, database,
		"knowledge", "update", created.ID, "--expected-revision", "2", "--summary", "Changed summary", "--json",
	).output).Item
	if reviewed.ReviewedAt == nil || updated.ReviewedAt != nil || updated.Revision != 3 {
		t.Fatalf("review invalidation: reviewed=%+v updated=%+v", reviewed, updated)
	}
	if promoted := executeAt(t, root, database, "knowledge", "promote", created.ID, "--expected-revision", "3", "--json"); promoted.err == nil {
		t.Fatal("edited draft was promoted without fresh review")
	}
}

func directoryNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, len(entries))
	for i := range entries {
		result[i] = entries[i].Name()
	}
	return result
}
