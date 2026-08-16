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
