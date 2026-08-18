package context

import (
	"testing"
	"time"
)

func TestUUIDv7ValidationForContextIDs(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if !IsUUIDv7(id) {
		t.Fatalf("IsUUIDv7(%q) = false", id)
	}
	if IsUUIDv7("{" + id + "}") {
		t.Fatalf("non-canonical uuid accepted")
	}
}

func TestCreateInputValidationNormalizesAndRejectsInvalidValues(t *testing.T) {
	projectID, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	valid := CreateInput{
		ProjectID:   projectID,
		Kind:        KindNote,
		Title:       "  example  ",
		Source:      SourceUser,
		Visibility:  VisibilityShared,
		Sensitivity: SensitivityNormal,
		Tags:        []string{"  tag1 ", "tag2"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("CreateInput.Validate() = %v", err)
	}

	invalid := valid
	invalid.Kind = "bad"
	if err := invalid.Validate(); ErrorCode(err) != CodeInvalid {
		t.Fatalf("invalid kind error = %v", err)
	}
}

func TestPatchApplyRejectsDeletedRecordsAndBlankTitle(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	record := ProjectContextRecord{ID: id, Kind: KindNote, Title: "original"}
	deletedAt := time.Now()
	record.DeletedAt = &deletedAt
	if err := (Patch{Title: stringPtr(" ")}).Apply(&record); err == nil || ErrorCode(err) != CodeInvalid {
		t.Fatalf("blank title patch = %v", err)
	}
	record = ProjectContextRecord{ID: id, Kind: KindNote, Title: "original", DeletedAt: &deletedAt}
	if err := (Patch{Tags: &[]string{"   "}}).Apply(&record); err == nil || ErrorCode(err) != CodeInvalid {
		t.Fatalf("blank tag patch = %v", err)
	}
}

func TestInstructionPolicyValidationAndDefaults(t *testing.T) {
	projectID, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	priority := PriorityHigh
	scope := ScopeProject
	valid := CreateInput{
		ProjectID:   projectID,
		Kind:        KindInstruction,
		Title:       "policy",
		Source:      SourceUser,
		Visibility:  VisibilityShared,
		Sensitivity: SensitivityNormal,
		Priority:    &priority,
		Scope:       &scope,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}

	note := valid
	note.Kind = KindNote
	if err := note.Validate(); ErrorCode(err) != CodeInvalid {
		t.Fatalf("note instruction policy error = %v", err)
	}

	record := ProjectContextRecord{Kind: KindInstruction, Title: "policy"}
	disabled := false
	if err := (Patch{Enabled: &disabled}).Apply(&record); err != nil {
		t.Fatal(err)
	}
	if record.Enabled == nil || *record.Enabled || record.Priority == nil || *record.Priority != PriorityNormal || record.Scope == nil || *record.Scope != ScopeProject {
		t.Fatalf("defaulted instruction policy = %+v", record)
	}

	record = ProjectContextRecord{Kind: KindNote, Title: "note"}
	if err := (Patch{Enabled: &disabled}).Apply(&record); ErrorCode(err) != CodeInvalid {
		t.Fatalf("note enabled patch error = %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}
