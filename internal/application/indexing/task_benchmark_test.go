package indexing_test

import (
	"path/filepath"
	"testing"

	"github.com/vtimame/istok.sh/internal/indexbench"
)

func TestTaskRetrievalBenchmark(t *testing.T) {
	report, err := indexbench.Evaluate(t.Context(), indexbench.Options{ConfigPath: filepath.Join("..", "..", "..", "testdata", "indexing", "benchmark.json")})
	t.Logf("baseline file R@5=%.3f R@10=%.3f symbol R@10=%.3f MRR@grade2=%.3f avg context bytes=%.0f max context bytes=%d graph-only supporting ratio=%.3f", report.Quality.FileRecallAt5, report.Quality.FileRecallAt10, report.Quality.SymbolRecallAt10, report.Quality.MRRPrimaryGrade, report.Quality.AverageContextBytes, report.Quality.MaxContextBytes, report.Quality.GraphOnlySupportingRatio)
	if err != nil {
		t.Fatalf("index benchmark failed: %v", err)
	}
}
