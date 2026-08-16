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

func stringPtr(value string) *string {
	return &value
}
