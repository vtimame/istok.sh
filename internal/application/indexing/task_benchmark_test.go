package indexing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"s26.dev/istok-cli/internal/retrieval"
)

type retrievalBenchmark struct {
	Cases      []benchmarkCase     `json:"cases"`
	Thresholds benchmarkThresholds `json:"thresholds"`
}
type benchmarkThresholds struct {
	FileRecallAt5               float64 `json:"file_recall_at_5"`
	FileRecallAt10              float64 `json:"file_recall_at_10"`
	SymbolRecallAt10            float64 `json:"symbol_recall_at_10"`
	MRR                         float64 `json:"mrr_primary_grade"`
	MaxContextBytes             int     `json:"max_context_bytes"`
	MinGraphOnlySupportingRatio float64 `json:"min_graph_only_supporting_ratio"`
}
type benchmarkCase struct {
	ID   string `json:"id"`
	Task struct {
		Title              string `json:"title"`
		Description        string `json:"description"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		Notes              string `json:"notes"`
	} `json:"task"`
	Relevant      []benchmarkRelevant `json:"relevant"`
	NegativePaths []string            `json:"negative_paths"`
}
type benchmarkRelevant struct {
	Grade   int      `json:"grade"`
	Path    string   `json:"path"`
	Symbols []string `json:"symbols"`
}

func TestTaskRetrievalBenchmark(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "indexing", "benchmark.json"))
	if err != nil {
		t.Fatal(err)
	}
	var benchmark retrievalBenchmark
	if err := json.Unmarshal(data, &benchmark); err != nil {
		t.Fatal(err)
	}
	files := readBenchmarkCorpus(t, filepath.Join("..", "..", "..", "testdata", "indexing", "corpus"))
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, files)
	ctx := context.Background()
	if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
		t.Fatal(err)
	}

	var recall5, recall10, symbol10, mrr, bytes, graphOnly, supporting float64
	maxContextBytes := 0
	for _, testCase := range benchmark.Cases {
		results, _, err := service.SearchTask(ctx, projectValue, retrieval.TaskSearchRequest{Title: testCase.Task.Title, Description: testCase.Task.Description, AcceptanceCriteria: testCase.Task.AcceptanceCriteria, Notes: testCase.Task.Notes})
		if err != nil {
			t.Fatalf("%s: %v", testCase.ID, err)
		}
		ranking := make([]string, 0, len(results))
		for _, result := range results {
			ranking = append(ranking, result.Path+"#"+result.Symbol)
		}
		t.Logf("%s ranking: %s", testCase.ID, strings.Join(ranking, ", "))

		caseBytes := 0
		firstPrimary := len(results) + 1
		negativeRanks := map[string]int{}
		var relevantFiles, relevantSymbols int
		for _, relevant := range testCase.Relevant {
			relevantFiles++
			relevantSymbols += len(relevant.Symbols)
		}
		found5, found10, foundSymbols := map[string]bool{}, map[string]bool{}, map[string]bool{}
		caseMRR := 0.0
		for index, result := range results {
			bytes += float64(len(result.Snippet))
			caseBytes += len(result.Snippet)
			for _, relevant := range testCase.Relevant {
				if relevant.Grade == 2 && result.Path == relevant.Path && index < firstPrimary {
					firstPrimary = index
				}
				if relevant.Grade == 1 && result.Path == relevant.Path {
					supporting++
					if result.LexicalScore == 0 && result.GraphScore > 0 {
						graphOnly++
						reason := strings.Join(result.Reasons, " ")
						if !strings.Contains(reason, "graph neighbor") || !strings.Contains(reason, "[") {
							t.Fatalf("%s: graph-only support without reason: %#v", testCase.ID, result)
						}
					}
				}
			}
			for _, negative := range testCase.NegativePaths {
				if result.Path == negative {
					negativeRanks[negative] = index
				}
			}
			for _, relevant := range testCase.Relevant {
				if result.Path == relevant.Path {
					if index < 5 {
						found5[relevant.Path] = true
					}
					if index < 10 {
						found10[relevant.Path] = true
					}
				}
				for _, symbol := range relevant.Symbols {
					if index < 10 && symbolMatch(result.Symbol+"\n"+result.Snippet, symbol) {
						foundSymbols[symbol] = true
					}
				}
				if relevant.Grade == 2 && result.Path == relevant.Path && caseMRR == 0 {
					caseMRR = 1 / float64(index+1)
				}
			}
		}
		for path, rank := range negativeRanks {
			if rank < firstPrimary {
				t.Fatalf("%s: negative path %q precedes every grade-2 path", testCase.ID, path)
			}
		}
		if caseBytes > benchmark.Thresholds.MaxContextBytes {
			t.Fatalf("%s: context bytes = %d", testCase.ID, caseBytes)
		}
		if caseBytes > maxContextBytes {
			maxContextBytes = caseBytes
		}
		recall5 += float64(len(found5)) / float64(relevantFiles)
		recall10 += float64(len(found10)) / float64(relevantFiles)
		symbol10 += float64(len(foundSymbols)) / float64(relevantSymbols)
		mrr += caseMRR
	}
	cases := float64(len(benchmark.Cases))
	recall5, recall10, symbol10, mrr, bytes = recall5/cases, recall10/cases, symbol10/cases, mrr/cases, bytes/cases
	graphRatio := 0.0
	if supporting > 0 {
		graphRatio = graphOnly / supporting
	}
	t.Logf("baseline file R@5=%.3f R@10=%.3f symbol R@10=%.3f MRR@grade2=%.3f avg context bytes=%.0f max context bytes=%d graph-only supporting ratio=%.3f", recall5, recall10, symbol10, mrr, bytes, maxContextBytes, graphRatio)
	if recall5 < benchmark.Thresholds.FileRecallAt5 || recall10 < benchmark.Thresholds.FileRecallAt10 || symbol10 < benchmark.Thresholds.SymbolRecallAt10 || mrr < benchmark.Thresholds.MRR || bytes > float64(benchmark.Thresholds.MaxContextBytes) || graphRatio < benchmark.Thresholds.MinGraphOnlySupportingRatio {
		t.Fatalf("regression: R5=%.3f R10=%.3f S10=%.3f MRR=%.3f bytes=%.0f", recall5, recall10, symbol10, mrr, bytes)
	}
}

func symbolMatch(actual, expected string) bool {
	if strings.Contains(actual, expected) {
		return true
	}
	parts := strings.Split(expected, ".")
	return strings.Contains(actual, parts[len(parts)-1])
}

func readBenchmarkCorpus(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
