package task

import "testing"

func TestUUIDv7ValidationRequiresCanonicalUUIDv7(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if !IsUUIDv7(id) {
		t.Fatalf("IsUUIDv7(%q) = false", id)
	}
	if IsUUIDv7("{"+id+"}") || IsUUIDv7("00000000-0000-4000-8000-000000000000") {
		t.Fatal("noncanonical or non-v7 UUID accepted")
	}
}

func TestPatchAndTransitionsAreStorageIndependent(t *testing.T) {
	title := "  revised  "
	value := Task{Status: StatusOpen, Title: "initial"}
	if err := (Patch{Title: &title}).Apply(&value); err != nil {
		t.Fatal(err)
	}
	if value.Title != "revised" {
		t.Fatalf("title = %q", value.Title)
	}
	if err := value.Block(); err != nil || value.Status != StatusBlocked {
		t.Fatalf("Block() = %v, status %s", err, value.Status)
	}
	if err := value.Block(); ErrorCode(err) != CodeInvalidTransition {
		t.Fatalf("second Block() error = %v", err)
	}
	if err := value.Unblock(); err != nil || value.Status != StatusOpen {
		t.Fatalf("Unblock() = %v, status %s", err, value.Status)
	}
}
