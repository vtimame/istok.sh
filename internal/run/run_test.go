package run

import (
	"strings"
	"testing"
	"time"

	projectcontext "s26.dev/istok-cli/internal/context"
	contextpack "s26.dev/istok-cli/internal/contextpack"
)

func TestStatusValidationAndTerminalStates(t *testing.T) {
	if !StatusActive.Valid() || !StatusSucceeded.Terminal() || !StatusBlocked.Terminal() {
		t.Fatal("status validation failed")
	}
	if Status("bogus").Valid() || ExecutionStatus("bogus").Valid() {
		t.Fatal("invalid statuses must be rejected")
	}
}

func TestFinishRunInputOverrideRequirementAndNormalizationHint(t *testing.T) {
	value := FinishRunInput{
		RunID:            mustID(t),
		LeaseID:          mustID(t),
		ExpectedRevision: 1,
		Status:           StatusSucceeded,
		ResultSummary:    "done",
		AllowUnvalidated: true,
	}

	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("FinishRunInput.Validate() = %v", value.Validate())
	}

	value.OverrideReason = "  valid reason  "
	if err := value.Validate(); err != nil {
		t.Fatalf("FinishRunInput.Validate() = %v", err)
	}
}

func TestStartExecutionInputValidationRequiresArgvAndCwd(t *testing.T) {
	value := StartExecutionInput{RunID: mustID(t), LeaseID: mustID(t), Argv: []string{"/bin/echo"}, CWD: "/tmp"}
	if err := value.Validate(); err != nil {
		t.Fatalf("StartExecutionInput.Validate() = %v", err)
	}

	value.Argv = nil
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("expected invalid argv for %v", value)
	}

	value.Argv = []string{" "}
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("expected invalid blank argv entry")
	}

	value.Argv = []string{"/bin/echo"}
	value.CWD = "  "
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("expected invalid cwd")
	}
}

func TestFinishExecutionInputValidateDurationMSNonNegative(t *testing.T) {
	negative := int64(-1)
	value := FinishExecutionInput{
		ExecutionID:      mustID(t),
		ExpectedRevision: 1,
		Status:           ExecutionSucceeded,
		DurationMS:       &negative,
	}
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("expected invalid duration, got %v", value.Validate())
	}
}

func TestClaimInputRequiresAuditedRetrievalOverride(t *testing.T) {
	value := ClaimInput{WithoutRetrieval: true}
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("ClaimInput.Validate() = %v", value.Validate())
	}

	value.RetrievalOverrideReason = "  unavailable index  "
	if err := value.Validate(); err != nil {
		t.Fatalf("ClaimInput.Validate() = %v", err)
	}

	value.WithoutRetrieval = false
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("ClaimInput.Validate() = %v", value.Validate())
	}
}

func TestArtifactInputValidationRejectsUnmanagedMetadata(t *testing.T) {
	base := ArtifactInput{
		ID:           mustID(t),
		ExecutionID:  mustID(t),
		Kind:         ArtifactKindStdout,
		RelativePath: "runs/run/stdout.log",
		SHA256:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OriginalSize: 4,
		StoredSize:   4,
		MediaType:    "text/plain",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("ArtifactInput.Validate() = %v", err)
	}

	for name, mutate := range map[string]func(*ArtifactInput){
		"absolute path": func(value *ArtifactInput) { value.RelativePath = "/tmp/stdout.log" },
		"traversal":     func(value *ArtifactInput) { value.RelativePath = "../stdout.log" },
		"invalid hash":  func(value *ArtifactInput) { value.SHA256 = strings.Repeat("z", 64) },
		"truncation":    func(value *ArtifactInput) { value.StoredSize = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if ErrorCode(value.Validate()) != CodeInvalid {
				t.Fatalf("ArtifactInput.Validate() = %v", value.Validate())
			}
		})
	}
}

func TestRecordValidationInputValidateDurationMSNonNegative(t *testing.T) {
	negative := int64(-10)
	value := RecordValidationInput{
		ExecutionID: mustID(t),
		Source:      ValidationSourceExecuted,
		Status:      ValidationStatusPassed,
		Command:     "cmd",
		Summary:     "ok",
		DurationMS:  &negative,
	}
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("expected invalid duration, got %v", value.Validate())
	}
}

func TestCompleteTaskInputEvidenceAndOverrides(t *testing.T) {
	if ErrorCode(CompleteTaskInput{ExpectedTaskRevision: 1, Note: "done"}.Validate()) != CodeEvidenceRequired {
		t.Fatal("expected evidence requirement")
	}

	value := CompleteTaskInput{ExpectedTaskRevision: 1, Note: "done", OverrideReason: "needs bypass"}
	if err := value.Validate(); err != nil {
		t.Fatalf("CompleteTaskInput.Validate() = %v", err)
	}

	value = CompleteTaskInput{ExpectedTaskRevision: 1, Note: "done", RunID: "not-a-uuid", ValidationID: mustID(t)}
	if ErrorCode(value.Validate()) != CodeInvalid {
		t.Fatalf("invalid run id should be rejected")
	}

	value = CompleteTaskInput{ExpectedTaskRevision: 1, Note: "done", RunID: mustID(t)}
	if ErrorCode(value.Validate()) != CodeEvidenceRequired {
		t.Fatalf("expected both evidence ids when no override")
	}

	value = CompleteTaskInput{ExpectedTaskRevision: 1, Note: "done", RunID: mustID(t), ValidationID: mustID(t)}
	if err := value.Validate(); err != nil {
		t.Fatalf("CompleteTaskInput.Validate() = %v", err)
	}
}

func TestActorSnapshotValidateUsesTrimmedText(t *testing.T) {
	if ErrorCode(ActorSnapshot{ID: "  ", Kind: "user", Name: "n"}.Validate()) != CodeInvalid {
		t.Fatal("expected whitespace-only id to be invalid")
	}
	if ErrorCode(ActorSnapshot{ID: mustID(t), Kind: "  ", Name: "n"}.Validate()) != CodeInvalid {
		t.Fatal("expected whitespace-only kind to be invalid")
	}
	if ErrorCode(ActorSnapshot{ID: mustID(t), Kind: "user", Name: "  "}.Validate()) != CodeInvalid {
		t.Fatal("expected whitespace-only name to be invalid")
	}
	value := ActorSnapshot{ID: mustID(t), Kind: "user", Name: "Name"}
	if err := value.Validate(); err != nil {
		t.Fatalf("ActorSnapshot.Validate() = %v", err)
	}
}

func TestContextSnapshotCopiesRecordsDeeply(t *testing.T) {
	validHash := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	enabled := true
	priority := projectcontext.PriorityHigh
	scope := projectcontext.ScopeProject
	contextPackage := contextpack.Package{
		SchemaVersion: contextpack.SchemaVersion,
		ProjectID:     mustProjectID(t),
		GeneratedAt:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Records: []projectcontext.ContextPackageItem{
			{
				RecordID:       mustID(t),
				RecordRevision: 42,
				ContentHash:    validHash,
				Kind:           projectcontext.KindDecision,
				Source:         projectcontext.SourceAgent,
				Visibility:     projectcontext.VisibilityShared,
				Sensitivity:    projectcontext.SensitivityNormal,
				Title:          "Title",
				Body:           "Body",
				Snippet:        "Snippet",
				Tags:           []string{"one", "two"},
			},
			{
				RecordID:       mustID(t),
				RecordRevision: 1,
				ContentHash:    validHash,
				Kind:           projectcontext.KindInstruction,
				Source:         projectcontext.SourceUser,
				Visibility:     projectcontext.VisibilityShared,
				Sensitivity:    projectcontext.SensitivityNormal,
				Enabled:        &enabled,
				Priority:       &priority,
				Scope:          &scope,
				Title:          "Instruction",
				Tags:           []string{},
			},
		},
		Retrieval: []contextpack.Item{{
			ItemID: mustID(t), ContractVersion: "istok.retrieval.v1", ChunkID: "chunk",
			Kind: "file_chunk", Path: "main.go", Language: "go", LineStart: 1, LineEnd: 2,
			ContentHash: validHash, Snippet: "package main", Score: 1, Visibility: "local_only",
			MatchedTerms: []string{"title"}, Provenance: []string{}, Reasons: []string{},
		}},
	}

	snapshot, err := NewContextSnapshot(mustID(t), contextPackage, contextPackage.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != ContextSnapshotSchemaVersion {
		t.Fatalf("schema version = %q", snapshot.SchemaVersion)
	}
	if snapshot.Records[0].RecordRevision != 42 || snapshot.Records[0].RecordID != contextPackage.Records[0].RecordID {
		t.Fatalf("snapshot item was not copied: %#v", snapshot.Records[0])
	}

	contextPackage.Records[0].Title = "changed"
	contextPackage.Records[0].Tags[0] = "changed"
	*contextPackage.Records[1].Enabled = false
	*contextPackage.Records[1].Priority = projectcontext.PriorityLow
	contextPackage.Retrieval[0].MatchedTerms[0] = "changed"
	if snapshot.Records[0].Title == "changed" {
		t.Fatal("snapshot title changed after source mutation")
	}
	if snapshot.Records[0].Tags[0] == "changed" {
		t.Fatal("snapshot tags changed after source mutation")
	}
	if snapshot.Records[1].Enabled == nil || !*snapshot.Records[1].Enabled || snapshot.Records[1].Priority == nil || *snapshot.Records[1].Priority != projectcontext.PriorityHigh {
		t.Fatal("snapshot instruction policy changed after source mutation")
	}
	if snapshot.Retrieval[0].MatchedTerms[0] == "changed" {
		t.Fatal("snapshot retrieval changed after source mutation")
	}
	if snapshot.Retrieval[0].Provenance == nil || snapshot.Retrieval[0].Reasons == nil {
		t.Fatal("snapshot retrieval arrays must remain non-nil")
	}
}

func TestContextSnapshotValidationPreservesHistoricalInstructionSchemas(t *testing.T) {
	item := ContextSnapshotItem{
		RecordID:       mustID(t),
		RecordRevision: 1,
		ContentHash:    "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Kind:           projectcontext.KindInstruction,
		Source:         projectcontext.SourceUser,
		Visibility:     projectcontext.VisibilityShared,
		Sensitivity:    projectcontext.SensitivityNormal,
		Title:          "Historical instruction",
		Tags:           []string{},
	}
	base := ContextSnapshot{
		ID:          mustID(t),
		ProjectID:   mustProjectID(t),
		GeneratedAt: time.Now().UTC(),
		Records:     []ContextSnapshotItem{item},
		Retrieval:   []ContextSnapshotRetrievalItem{},
	}

	for _, version := range []string{ContextSnapshotSchemaVersionV1, ContextSnapshotSchemaVersionV2} {
		value := base
		value.SchemaVersion = version
		if err := value.Validate(); err != nil {
			t.Fatalf("historical snapshot v%s validation = %v", version, err)
		}
	}

	current := base
	current.SchemaVersion = ContextSnapshotSchemaVersion
	if err := current.Validate(); err == nil {
		t.Fatal("current instruction snapshot accepted without policy")
	}
	enabled := true
	priority := projectcontext.PriorityNormal
	scope := projectcontext.ScopeProject
	current.Records[0].Enabled = &enabled
	current.Records[0].Priority = &priority
	current.Records[0].Scope = &scope
	if err := current.Validate(); err != nil {
		t.Fatalf("current instruction snapshot validation = %v", err)
	}
}

func TestContextSnapshotValidateRequiresProjectMatchAndVersionAndFields(t *testing.T) {
	contextPackage := contextpack.Package{
		SchemaVersion: contextpack.SchemaVersion,
		ProjectID:     mustProjectID(t),
		GeneratedAt:   time.Now().UTC(),
		Records: []projectcontext.ContextPackageItem{
			{
				RecordID:       mustID(t),
				RecordRevision: 1,
				ContentHash:    "zz",
				Kind:           projectcontext.KindDecision,
				Source:         projectcontext.SourceAgent,
				Visibility:     projectcontext.VisibilityShared,
				Sensitivity:    projectcontext.SensitivityNormal,
				Title:          "T",
				Tags:           []string{"tag"},
			},
		},
	}

	if _, err := NewContextSnapshot(mustID(t), contextPackage, mustProjectID(t)); err == nil {
		t.Fatal("expected project mismatch error")
	}

	contextPackage.ProjectID = mustProjectID(t)
	contextPackage.SchemaVersion = "0"
	if _, err := NewContextSnapshot(mustID(t), contextPackage, contextPackage.ProjectID); err == nil {
		t.Fatal("expected schema version error")
	}

	contextPackage.SchemaVersion = contextpack.SchemaVersion
	contextPackage.Records[0].ContentHash = "zz"
	snapshot, err := NewContextSnapshot(mustID(t), contextPackage, contextPackage.ProjectID)
	if err == nil {
		t.Fatalf("expected hash validation error, got %#v", snapshot)
	}
}

func mustID(t *testing.T) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustProjectID(t *testing.T) string {
	t.Helper()
	return mustID(t)
}
