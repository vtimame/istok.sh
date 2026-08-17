package indexbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateHardeningReport(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "hardening.json")
	events := "{\"Action\":\"run\",\"Package\":\"example.test\"}\n{\"Action\":\"pass\",\"Package\":\"example.test\"}\n"
	if err := os.WriteFile(eventsPath, []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Evaluate(context.Background(), Options{
		ConfigPath:      filepath.Join("..", "..", "testdata", "indexing", "benchmark.json"),
		HardeningEvents: eventsPath,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !report.Passed || report.Mode != "hardening" || report.HardeningEvents == nil || report.HardeningEvents.SHA256 == "" || !report.HardeningEvents.Passed {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Violations == nil {
		t.Fatal("violations must encode as an empty array")
	}
}

func TestEvaluateRejectsFailingHardeningEvents(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "hardening.json")
	events := "{\"Action\":\"pass\",\"Package\":\"example.test\"}\n{\"Action\":\"fail\",\"Package\":\"example.test\"}\n"
	if err := os.WriteFile(eventsPath, []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Evaluate(context.Background(), Options{
		ConfigPath:      filepath.Join("..", "..", "testdata", "indexing", "benchmark.json"),
		HardeningEvents: eventsPath,
	})
	if err == nil || report.Passed {
		t.Fatalf("Evaluate() report = %#v, %v; want failure", report, err)
	}
}
