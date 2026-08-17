package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReportCreatesParentDirectories(t *testing.T) {
	output := filepath.Join(t.TempDir(), "nested", "report.json")
	if err := writeReport(output, []byte("{}\n")); err != nil {
		t.Fatalf("writeReport() error = %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("report = %q", data)
	}
}
