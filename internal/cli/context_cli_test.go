package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/contextpack"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestContextCLIJSONLifecycleUsesCurrentProject(t *testing.T) {
	root := t.TempDir()
	otherRoot := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	executeAt(t, root, database, "init", "--json")
	executeAt(t, otherRoot, database, "init", "--json")

	added := executeAt(t, root, database, "context", "add", "Auth decision", "--body", "Use signed archives", "--kind", "decision", "--tag", "security", "--json")
	if added.err != nil {
		t.Fatalf("context add: %v", added.err)
	}
	created := decodeJSONResult[contextView](t, added.output)
	if created.Record.ID == "" || created.Record.Kind != contextmodel.KindDecision || created.Record.ProjectID != created.Project.ID || created.Record.Revision != 1 {
		t.Fatalf("created context = %+v", created)
	}

	listed := executeAt(t, root, database, "context", "list", "--json")
	if listed.err != nil {
		t.Fatalf("context list: %v", listed.err)
	}
	list := decodeJSONResult[contextListView](t, listed.output)
	if len(list.Records) != 1 || list.Records[0].ID != created.Record.ID {
		t.Fatalf("context list = %+v", list)
	}

	searched := executeAt(t, root, database, "context", "search", "signed", "--json")
	if searched.err != nil {
		t.Fatalf("context search: %v", searched.err)
	}
	search := decodeJSONResult[contextListView](t, searched.output)
	if len(search.Records) != 1 || search.Records[0].ID != created.Record.ID {
		t.Fatalf("context search = %+v", search)
	}

	showed := executeAt(t, root, database, "context", "show", created.Record.ID, "--json")
	if showed.err != nil {
		t.Fatalf("context show: %v", showed.err)
	}
	showedView := decodeJSONResult[contextView](t, showed.output)
	if showedView.Record.ID != created.Record.ID || showedView.Record.Revision != 1 {
		t.Fatalf("context show = %+v", showedView)
	}

	otherShow := executeAt(t, otherRoot, database, "context", "show", created.Record.ID, "--json")
	assertVersionedBusinessError(t, otherShow.err, string(contextmodel.CodeNotFound))

	title := "Updated auth decision"
	updated := executeAt(t, root, database, "context", "update", created.Record.ID, "--expected-revision", "1", "--title", title, "--json")
	if updated.err != nil {
		t.Fatalf("context update: %v", updated.err)
	}
	updatedView := decodeJSONResult[contextView](t, updated.output)
	if updatedView.Record.Title != title || updatedView.Record.Revision != 2 {
		t.Fatalf("updated context = %+v", updatedView)
	}

	deletedWithoutYes := executeAt(t, root, database, "context", "delete", created.Record.ID, "--expected-revision", "2", "--json")
	assertVersionedBusinessError(t, deletedWithoutYes.err, string(contextmodel.CodeInvalid))

	deleted := executeAt(t, root, database, "context", "delete", created.Record.ID, "--expected-revision", "2", "--yes", "--json")
	if deleted.err != nil {
		t.Fatalf("context delete: %v", deleted.err)
	}
	deletedView := decodeJSONResult[contextView](t, deleted.output)
	if deletedView.Record.DeletedAt == nil {
		t.Fatalf("deleted context = %+v", deletedView)
	}
}

func TestContextCLIShowAllRecordsWithoutID(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	executeAt(t, root, database, "init", "--json")

	first := executeAt(
		t, root, database,
		"context", "add", "Project\ndecision", "--kind", "decision", "--body", "# First body\n\n```go\nfmt.Println(\"one\")\n```\n\n- one",
		"--tag", "security", "--tag", "api", "--json",
	)
	if first.err != nil {
		t.Fatalf("context add first: %v", first.err)
	}
	firstView := decodeJSONResult[contextView](t, first.output)

	second := executeAt(t, root, database, "context", "add", "Runbook", "--body", "Second body", "--kind", "note", "--json")
	if second.err != nil {
		t.Fatalf("context add second: %v", second.err)
	}
	secondView := decodeJSONResult[contextView](t, second.output)

	shown := executeAt(t, root, database, "context", "show")
	if shown.err != nil {
		t.Fatalf("context show (all): %v", shown.err)
	}

	output := shown.output
	if !strings.Contains(output, "# Project context: ") {
		t.Fatalf("context show output = %q", output)
	}
	if !strings.Contains(output, "2 context records.") {
		t.Fatalf("context show output = %q", output)
	}
	if !strings.Contains(output, "## Project decision") {
		t.Fatalf("context show output = %q", output)
	}
	if !strings.Contains(output, "## Runbook") {
		t.Fatalf("context show output = %q", output)
	}
	if !strings.Contains(output, "# First body") || !strings.Contains(output, "fmt.Println(\"one\")") || !strings.Contains(output, "• one") {
		t.Fatalf("context show output = %q", output)
	}
	if strings.Contains(output, "```go") {
		t.Fatalf("context show output should render fenced code blocks: %q", output)
	}
	if !strings.Contains(output, "Second body") {
		t.Fatalf("context show output = %q", output)
	}
	if !strings.Contains(output, "**Record ID:** "+firstView.Record.ID) ||
		!strings.Contains(output, "**Record ID:** "+secondView.Record.ID) {
		t.Fatalf("context show output = %q", output)
	}
	if !strings.Contains(output, "## Project decision") || !strings.Contains(output, "---") {
		t.Fatalf("context show separator/output = %q", output)
	}
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("context show output should be rendered markdown without ANSI: %q", output)
	}

	showJSON := executeAt(t, root, database, "context", "show", "--json")
	if showJSON.err != nil {
		t.Fatalf("context show --json: %v", showJSON.err)
	}
	markdown := decodeJSONResult[contextMarkdownView](t, showJSON.output)
	if len(markdown.Records) != 2 {
		t.Fatalf("context show markdown = %+v", markdown)
	}

	deleted := executeAt(t, root, database, "context", "delete", firstView.Record.ID, "--expected-revision", "1", "--yes", "--json")
	if deleted.err != nil {
		t.Fatalf("context delete first: %v", deleted.err)
	}
	onlyActive := executeAt(t, root, database, "context", "show")
	if onlyActive.err != nil {
		t.Fatalf("context show after delete: %v", onlyActive.err)
	}
	if !strings.Contains(onlyActive.output, secondView.Record.ID) || strings.Contains(onlyActive.output, firstView.Record.ID) {
		t.Fatalf("context show active output = %q", onlyActive.output)
	}
	if !strings.Contains(onlyActive.output, "1 context record.") {
		t.Fatalf("context show active output = %q", onlyActive.output)
	}

	withDeleted := executeAt(t, root, database, "context", "show", "--deleted")
	if withDeleted.err != nil {
		t.Fatalf("context show with --deleted: %v", withDeleted.err)
	}
	if !strings.Contains(withDeleted.output, secondView.Record.ID) || !strings.Contains(withDeleted.output, firstView.Record.ID) {
		t.Fatalf("context show with --deleted output = %q", withDeleted.output)
	}
	if !strings.Contains(withDeleted.output, "2 context records.") {
		t.Fatalf("context show with --deleted output = %q", withDeleted.output)
	}
}

func TestContextCLIInstructionPolicyLifecycle(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")

	added := executeAt(t, root, database, "context", "add", "Validation policy", "--kind", "instruction", "--priority", "critical", "--scope", "project", "--body", "Run all tests", "--json")
	if added.err != nil {
		t.Fatal(added.err)
	}
	created := decodeJSONResult[contextView](t, added.output)
	if created.Record.Enabled == nil || !*created.Record.Enabled || created.Record.Priority == nil || *created.Record.Priority != contextmodel.PriorityCritical || created.Record.Scope == nil || *created.Record.Scope != contextmodel.ScopeProject {
		t.Fatalf("created instruction = %+v", created.Record)
	}

	disabled := executeAt(t, root, database, "context", "disable", created.Record.ID, "--expected-revision", "1", "--json")
	if disabled.err != nil {
		t.Fatal(disabled.err)
	}
	disabledView := decodeJSONResult[contextView](t, disabled.output)
	if disabledView.Record.Enabled == nil || *disabledView.Record.Enabled || disabledView.Record.Revision != 2 {
		t.Fatalf("disabled instruction = %+v", disabledView.Record)
	}

	listed := decodeJSONResult[contextListView](t, executeAt(t, root, database, "context", "list", "--json").output)
	if len(listed.Records) != 0 {
		t.Fatalf("default list = %+v", listed.Records)
	}
	withDisabledResult := executeAt(t, root, database, "context", "list", "--include-disabled", "--json")
	if withDisabledResult.err != nil {
		t.Fatal(withDisabledResult.err)
	}
	withDisabled := decodeJSONResult[contextListView](t, withDisabledResult.output)
	if len(withDisabled.Records) != 1 || withDisabled.Records[0].ID != created.Record.ID {
		t.Fatalf("include-disabled list = %+v", withDisabled.Records)
	}
	humanList := executeAt(t, root, database, "context", "list", "--include-disabled")
	if humanList.err != nil {
		t.Fatal(humanList.err)
	}
	if !strings.Contains(humanList.output, "DISABLED") || !strings.Contains(humanList.output, "critical/project") {
		t.Fatalf("include-disabled human list = %q", humanList.output)
	}

	markdown := executeAt(t, root, database, "context", "show", "--include-disabled")
	if markdown.err != nil {
		t.Fatal(markdown.err)
	}
	if !strings.Contains(markdown.output, "**Enabled:** false") || !strings.Contains(markdown.output, "**Priority:** critical") || !strings.Contains(markdown.output, "**Scope:** project") {
		t.Fatalf("instruction markdown = %q", markdown.output)
	}

	enabled := executeAt(t, root, database, "context", "enable", created.Record.ID, "--expected-revision", "2", "--json")
	if enabled.err != nil {
		t.Fatal(enabled.err)
	}
	enabledView := decodeJSONResult[contextView](t, enabled.output)
	if enabledView.Record.Enabled == nil || !*enabledView.Record.Enabled || enabledView.Record.Revision != 3 {
		t.Fatalf("enabled instruction = %+v", enabledView.Record)
	}

	invalid := executeAt(t, root, database, "context", "add", "Note", "--kind", "note", "--priority", "high", "--json")
	assertVersionedBusinessError(t, invalid.err, string(contextmodel.CodeInvalid))
}

func TestContextCLIPreviewDoctorAndLifecycle(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	added := executeAt(t, root, database,
		"context", "add", "Authentication rotation",
		"--kind", "decision",
		"--delivery", "ranked",
		"--body", "Rotate authentication token",
		"--review-after", "2020-01-01T00:00:00Z",
		"--expires-at", "2030-01-01T00:00:00Z",
		"--json",
	)
	if added.err != nil {
		t.Fatal(added.err)
	}
	created := decodeJSONResult[contextView](t, added.output).Record
	if created.Delivery != contextmodel.DeliveryRanked || created.ReviewAfter == nil || created.ExpiresAt == nil {
		t.Fatalf("created lifecycle record = %#v", created)
	}

	tasks, _ := newTaskRepositoryForTest(t, database)
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "Rotate authentication token"}, task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}); err != nil {
		t.Fatal(err)
	}

	previewed := executeAt(t, root, database,
		"context", "preview", "1",
		"--without-retrieval", "--retrieval-override-reason", "preview only",
		"--json",
	)
	if previewed.err != nil {
		t.Fatal(previewed.err)
	}
	preview := decodeJSONResultAtVersion[contextpack.Package](t, previewed.output, "2")
	if len(preview.Records) != 1 || preview.Records[0].RecordID != created.ID || preview.Records[0].Lane != contextmodel.LaneRanked || preview.Metadata.Assembly.SelectedCount != 1 {
		t.Fatalf("preview = %#v", preview)
	}

	doctorResult := executeAt(t, root, database, "context", "doctor", "--json")
	if doctorResult.err != nil {
		t.Fatal(doctorResult.err)
	}
	doctor := decodeJSONResult[contextmodel.DiagnosticReport](t, doctorResult.output)
	if doctor.ActiveRecords != 1 || doctor.ReviewDueRecords != 1 {
		t.Fatalf("doctor = %#v", doctor)
	}

	cleared := executeAt(t, root, database,
		"context", "update", created.ID,
		"--expected-revision", "1",
		"--review-after=", "--expires-at=",
		"--json",
	)
	if cleared.err != nil {
		t.Fatal(cleared.err)
	}
	updated := decodeJSONResult[contextView](t, cleared.output).Record
	if updated.ReviewAfter != nil || updated.ExpiresAt != nil {
		t.Fatalf("cleared lifecycle record = %#v", updated)
	}
}

func TestContextCLIHumanOutputAndHelpDoNotCreateDatabase(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")

	added := executeAt(t, root, database, "context", "add", "Build note", "--body", "Run go test")
	if added.err != nil {
		t.Fatalf("context add human: %v", added.err)
	}
	if !strings.Contains(added.output, "Build note") || !strings.Contains(added.output, "Body") {
		t.Fatalf("context add output = %q", added.output)
	}

	listed := executeAt(t, root, database, "context", "list")
	if listed.err != nil {
		t.Fatalf("context list human: %v", listed.err)
	}
	if !strings.Contains(listed.output, "context records") || !strings.Contains(listed.output, "Build note") {
		t.Fatalf("context list output = %q", listed.output)
	}

	for _, args := range [][]string{
		{"context", "--help"},
		{"context", "add", "--help"},
		{"context", "list", "--help"},
		{"context", "show", "--help"},
		{"context", "search", "--help"},
		{"context", "update", "--help"},
		{"context", "enable", "--help"},
		{"context", "disable", "--help"},
		{"context", "delete", "--help"},
		{"context", "preview", "--help"},
		{"context", "doctor", "--help"},
	} {
		if got := executeHelp(t, args...); !strings.Contains(got, "context") {
			t.Fatalf("context help %v = %q", args, got)
		}
	}
}
