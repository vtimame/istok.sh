// Package indexbench evaluates the stable indexing acceptance corpus.
package indexbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	applicationindexing "github.com/vtimame/istok.sh/internal/application/indexing"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

const ContractVersion = "istok.index-acceptance.v1"

type Options struct {
	ConfigPath      string
	Commit          string
	Dirty           bool
	HardeningEvents string
}

type Report struct {
	ContractVersion string             `json:"contract_version"`
	Mode            string             `json:"mode"`
	GeneratedAt     time.Time          `json:"generated_at"`
	Commit          string             `json:"commit"`
	Dirty           bool               `json:"dirty"`
	Environment     Environment        `json:"environment"`
	CorpusSHA256    string             `json:"corpus_sha256"`
	Quality         QualityMetrics     `json:"quality"`
	Performance     PerformanceMetrics `json:"performance"`
	SidecarBytes    int64              `json:"sidecar_bytes"`
	HardeningEvents *HardeningEvents   `json:"hardening_events,omitempty"`
	Violations      []string           `json:"violations"`
	Passed          bool               `json:"passed"`
}

type Environment struct {
	GoVersion      string `json:"go_version"`
	GOOS           string `json:"goos"`
	GOARCH         string `json:"goarch"`
	ContainerImage string `json:"container_image"`
}

type QualityMetrics struct {
	FileRecallAt5              float64    `json:"file_recall_at_5"`
	FileRecallAt10             float64    `json:"file_recall_at_10"`
	SymbolRecallAt10           float64    `json:"symbol_recall_at_10"`
	MRRPrimaryGrade            float64    `json:"mrr_primary_grade"`
	AverageContextBytes        float64    `json:"average_context_bytes"`
	MaxContextBytes            int        `json:"max_context_bytes"`
	GraphOnlySupportingRatio   float64    `json:"graph_only_supporting_ratio"`
	NegativePathOrderingPassed bool       `json:"negative_path_ordering_passed"`
	Thresholds                 Thresholds `json:"thresholds"`
}

type Thresholds struct {
	FileRecallAt5               float64 `json:"file_recall_at_5"`
	FileRecallAt10              float64 `json:"file_recall_at_10"`
	SymbolRecallAt10            float64 `json:"symbol_recall_at_10"`
	MRR                         float64 `json:"mrr_primary_grade"`
	MaxContextBytes             int     `json:"max_context_bytes"`
	MinGraphOnlySupportingRatio float64 `json:"min_graph_only_supporting_ratio"`
}

type PerformanceMetrics struct {
	InitialEnsureFreshMS      float64 `json:"initial_ensure_fresh_ms"`
	UnchangedEnsureFreshP50MS float64 `json:"unchanged_ensure_fresh_p50_ms"`
	UnchangedEnsureFreshP95MS float64 `json:"unchanged_ensure_fresh_p95_ms"`
	IncrementalEnsureFreshMS  float64 `json:"incremental_ensure_fresh_ms"`
}

type HardeningEvents struct {
	SHA256 string `json:"sha256"`
	Passed bool   `json:"passed"`
}

type benchmark struct {
	CorpusRoot string          `json:"corpus_root"`
	Thresholds Thresholds      `json:"thresholds"`
	Cases      []benchmarkCase `json:"cases"`
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

// Evaluate runs the corpus in an isolated temporary project. A non-nil report
// is always returned, including for evaluation failures that occur after setup.
func Evaluate(ctx context.Context, options Options) (Report, error) {
	report := newReport(options)
	configPath, err := filepath.Abs(options.ConfigPath)
	if err != nil {
		return fail(report, err)
	}

	value, corpusPath, err := loadBenchmark(configPath)
	if err != nil {
		return fail(report, err)
	}
	report.Quality.Thresholds = value.Thresholds

	corpusHash, err := hashTree(corpusPath)
	if err != nil {
		return fail(report, err)
	}
	report.CorpusSHA256 = corpusHash

	if options.HardeningEvents != "" {
		events, err := verifyHardeningEvents(options.HardeningEvents)
		if err != nil {
			return fail(report, err)
		}
		report.Mode = "hardening"
		report.HardeningEvents = &events
	}

	temporaryRoot, err := os.MkdirTemp("", "istok-indexbench-")
	if err != nil {
		return fail(report, err)
	}
	defer os.RemoveAll(temporaryRoot)

	projectRoot := filepath.Join(temporaryRoot, "project")
	if err := copyTree(corpusPath, projectRoot); err != nil {
		return fail(report, err)
	}
	root, err := project.Canonicalize(projectRoot)
	if err != nil {
		return fail(report, err)
	}
	projectValue := project.Project{ID: uuid.NewString(), Name: "indexbench", Root: &root}
	service := applicationindexing.NewService(applicationindexing.Config{IndexRoot: filepath.Join(temporaryRoot, "indexes")})

	started := time.Now()
	initial, err := service.EnsureFresh(ctx, projectValue)
	report.Performance.InitialEnsureFreshMS = millisecondsSince(started)
	if err != nil {
		return fail(report, fmt.Errorf("initial EnsureFresh: %w", err))
	}

	unchanged := make([]float64, 0, 5)
	for range 5 {
		started = time.Now()
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			return fail(report, fmt.Errorf("unchanged EnsureFresh: %w", err))
		}
		unchanged = append(unchanged, millisecondsSince(started))
	}
	report.Performance.UnchangedEnsureFreshP50MS = percentile(unchanged, 0.50)
	report.Performance.UnchangedEnsureFreshP95MS = percentile(unchanged, 0.95)

	quality, violations, err := evaluateQuality(ctx, service, projectValue, value)
	report.Quality.FileRecallAt5 = quality.FileRecallAt5
	report.Quality.FileRecallAt10 = quality.FileRecallAt10
	report.Quality.SymbolRecallAt10 = quality.SymbolRecallAt10
	report.Quality.MRRPrimaryGrade = quality.MRRPrimaryGrade
	report.Quality.AverageContextBytes = quality.AverageContextBytes
	report.Quality.MaxContextBytes = quality.MaxContextBytes
	report.Quality.GraphOnlySupportingRatio = quality.GraphOnlySupportingRatio
	report.Quality.NegativePathOrderingPassed = quality.NegativePathOrderingPassed
	report.Violations = append(report.Violations, violations...)
	if err != nil {
		return fail(report, err)
	}

	incrementalPath := filepath.Join(projectRoot, "internal", "indexbench_incremental.go")
	if err := os.WriteFile(incrementalPath, []byte("package internal\n\nfunc IndexbenchIncremental() {}\n"), 0o600); err != nil {
		return fail(report, fmt.Errorf("write incremental corpus file: %w", err))
	}
	started = time.Now()
	incremental, err := service.EnsureFresh(ctx, projectValue)
	report.Performance.IncrementalEnsureFreshMS = millisecondsSince(started)
	if err != nil {
		return fail(report, fmt.Errorf("incremental EnsureFresh: %w", err))
	}
	if incremental.Revision <= initial.Revision {
		report.Violations = append(report.Violations, "incremental revision did not increase")
	}
	if incremental.EpochID != initial.EpochID {
		report.Violations = append(report.Violations, "incremental update changed epoch")
	}

	report.SidecarBytes, err = treeSize(filepath.Join(temporaryRoot, "indexes"))
	if err != nil {
		return fail(report, err)
	}
	if len(report.Violations) != 0 {
		report.Passed = false

		return report, errors.New(strings.Join(report.Violations, "; "))
	}

	report.Passed = true
	return report, nil
}

func newReport(options Options) Report {
	return Report{
		ContractVersion: ContractVersion,
		Mode:            "benchmark",
		GeneratedAt:     time.Now().UTC(),
		Commit:          options.Commit,
		Dirty:           options.Dirty,
		Environment:     Environment{GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, ContainerImage: os.Getenv("ISTOK_HARDENING_IMAGE")},
		Violations:      []string{},
	}
}

func fail(report Report, err error) (Report, error) {
	report.Passed = false
	if err != nil {
		report.Violations = append(report.Violations, err.Error())
	}
	return report, err
}

func loadBenchmark(configPath string) (benchmark, string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return benchmark{}, "", fmt.Errorf("read benchmark config: %w", err)
	}
	var value benchmark
	if err := json.Unmarshal(data, &value); err != nil {
		return benchmark{}, "", fmt.Errorf("parse benchmark config: %w", err)
	}
	if value.CorpusRoot == "" || len(value.Cases) == 0 {
		return benchmark{}, "", errors.New("benchmark config must contain corpus_root and cases")
	}
	return value, filepath.Join(filepath.Dir(configPath), value.CorpusRoot), nil
}

type qualityValues struct {
	FileRecallAt5, FileRecallAt10, SymbolRecallAt10, MRRPrimaryGrade, AverageContextBytes, GraphOnlySupportingRatio float64
	MaxContextBytes                                                                                                 int
	NegativePathOrderingPassed                                                                                      bool
}

func evaluateQuality(ctx context.Context, service *applicationindexing.Service, value project.Project, benchmark benchmark) (qualityValues, []string, error) {
	result := qualityValues{NegativePathOrderingPassed: true}
	violations := []string{}
	var recall5, recall10, symbol10, mrr, bytes, graphOnly, supporting float64

	for _, testCase := range benchmark.Cases {
		results, _, err := service.SearchTask(ctx, value, retrieval.TaskSearchRequest{Title: testCase.Task.Title, Description: testCase.Task.Description, AcceptanceCriteria: testCase.Task.AcceptanceCriteria, Notes: testCase.Task.Notes})
		if err != nil {
			return result, violations, fmt.Errorf("search %s: %w", testCase.ID, err)
		}
		caseBytes, firstPrimary := 0, len(results)+1
		negativeRanks := map[string]int{}
		relevantFiles, relevantSymbols := len(testCase.Relevant), 0
		for _, relevant := range testCase.Relevant {
			relevantSymbols += len(relevant.Symbols)
		}
		found5, found10, foundSymbols := map[string]bool{}, map[string]bool{}, map[string]bool{}
		caseMRR := 0.0
		for index, item := range results {
			caseBytes += len(item.Snippet)
			bytes += float64(len(item.Snippet))
			for _, relevant := range testCase.Relevant {
				if relevant.Grade == 2 && item.Path == relevant.Path && index < firstPrimary {
					firstPrimary = index
				}
				if relevant.Grade == 1 && item.Path == relevant.Path {
					supporting++
					if item.LexicalScore == 0 && item.GraphScore > 0 {
						graphOnly++
						if reason := strings.Join(item.Reasons, " "); !strings.Contains(reason, "graph neighbor") || !strings.Contains(reason, "[") {
							violations = append(violations, fmt.Sprintf("%s: graph-only support without reason", testCase.ID))
						}
					}
				}
				if item.Path == relevant.Path {
					if index < 5 {
						found5[relevant.Path] = true
					}
					if index < 10 {
						found10[relevant.Path] = true
					}
				}
				for _, symbol := range relevant.Symbols {
					if index < 10 && symbolMatch(item.Symbol+"\n"+item.Snippet, symbol) {
						foundSymbols[symbol] = true
					}
				}
				if relevant.Grade == 2 && item.Path == relevant.Path && caseMRR == 0 {
					caseMRR = 1 / float64(index+1)
				}
			}
			for _, negative := range testCase.NegativePaths {
				if item.Path == negative {
					negativeRanks[negative] = index
				}
			}
		}
		for _, negative := range sortedKeys(negativeRanks) {
			if negativeRanks[negative] < firstPrimary {
				result.NegativePathOrderingPassed = false
				violations = append(violations, fmt.Sprintf("%s: negative path %q precedes every grade-2 path", testCase.ID, negative))
			}
		}
		if caseBytes > benchmark.Thresholds.MaxContextBytes {
			violations = append(violations, fmt.Sprintf("%s: context bytes = %d", testCase.ID, caseBytes))
		}
		if caseBytes > result.MaxContextBytes {
			result.MaxContextBytes = caseBytes
		}
		if relevantFiles == 0 || relevantSymbols == 0 {
			return result, violations, fmt.Errorf("%s: benchmark case has no relevant files or symbols", testCase.ID)
		}
		recall5 += float64(len(found5)) / float64(relevantFiles)
		recall10 += float64(len(found10)) / float64(relevantFiles)
		symbol10 += float64(len(foundSymbols)) / float64(relevantSymbols)
		mrr += caseMRR
	}
	cases := float64(len(benchmark.Cases))
	result.FileRecallAt5, result.FileRecallAt10, result.SymbolRecallAt10, result.MRRPrimaryGrade, result.AverageContextBytes = recall5/cases, recall10/cases, symbol10/cases, mrr/cases, bytes/cases
	if supporting > 0 {
		result.GraphOnlySupportingRatio = graphOnly / supporting
	}
	thresholds := benchmark.Thresholds
	if result.FileRecallAt5 < thresholds.FileRecallAt5 || result.FileRecallAt10 < thresholds.FileRecallAt10 || result.SymbolRecallAt10 < thresholds.SymbolRecallAt10 || result.MRRPrimaryGrade < thresholds.MRR || result.AverageContextBytes > float64(thresholds.MaxContextBytes) || result.GraphOnlySupportingRatio < thresholds.MinGraphOnlySupportingRatio {
		violations = append(violations, fmt.Sprintf("quality thresholds: R5=%.3f R10=%.3f S10=%.3f MRR=%.3f bytes=%.0f graph=%.3f", result.FileRecallAt5, result.FileRecallAt10, result.SymbolRecallAt10, result.MRRPrimaryGrade, result.AverageContextBytes, result.GraphOnlySupportingRatio))
	}
	return result, violations, nil
}

func symbolMatch(actual, expected string) bool {
	if strings.Contains(actual, expected) {
		return true
	}
	parts := strings.Split(expected, ".")
	return strings.Contains(actual, parts[len(parts)-1])
}
func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func millisecondsSince(started time.Time) float64 {
	return float64(time.Since(started)) / float64(time.Millisecond)
}
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copy := append([]float64(nil), values...)
	sort.Float64s(copy)
	index := int(float64(len(copy)) * p)
	if index >= len(copy) {
		index = len(copy) - 1
	}
	return copy[index]
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	})
}
func hashTree(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		io.WriteString(hash, filepath.ToSlash(relative))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func treeSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
}

func verifyHardeningEvents(path string) (HardeningEvents, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HardeningEvents{}, fmt.Errorf("read hardening events: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	packagePassed := false
	events := 0
	for {
		var event goTestEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return HardeningEvents{}, fmt.Errorf("parse hardening events: %w", err)
		}
		events++
		if event.Action == "fail" {
			return HardeningEvents{}, errors.New("hardening events contain fail action")
		}
		if event.Action == "pass" && event.Package != "" && event.Test == "" {
			packagePassed = true
		}
	}
	if events == 0 || !packagePassed {
		return HardeningEvents{}, errors.New("hardening events do not contain package-level pass")
	}
	sum := sha256.Sum256(data)
	return HardeningEvents{SHA256: hex.EncodeToString(sum[:]), Passed: true}, nil
}
