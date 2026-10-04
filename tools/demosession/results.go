package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Only the fields the transcript shows are decoded from the tool results.

type taskResult struct {
	Task struct {
		ID       string `json:"id"`
		Number   int64  `json:"number"`
		Revision int64  `json:"revision"`
		Status   string `json:"status"`
	} `json:"task"`
}

type claimResult struct {
	Run struct {
		ID       string `json:"id"`
		LeaseID  string `json:"lease_id"`
		Revision int64  `json:"revision"`
	} `json:"run"`
	Snapshot struct {
		Records []struct {
			Title    string `json:"title"`
			Delivery string `json:"delivery"`
		} `json:"records"`
		Retrieval []struct {
			Path string `json:"path"`
		} `json:"retrieval"`
		Knowledge []struct{} `json:"knowledge_catalog"`
	} `json:"snapshot"`
}

type executionResult struct {
	Execution struct {
		ExitCode   *int   `json:"exit_code"`
		DurationMS *int64 `json:"duration_ms"`
	} `json:"execution"`
	Artifacts []struct {
		Kind         string `json:"kind"`
		RelativePath string `json:"relative_path"`
	} `json:"artifacts"`
	Validation *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"validation"`
}

type runResult struct {
	Run struct {
		Status string `json:"status"`
	} `json:"run"`
}

type contextResult struct {
	Context struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Delivery string `json:"delivery"`
	} `json:"context"`
}

func contextSummary(claimed claimResult) string {
	files := map[string]bool{}
	for _, item := range claimed.Snapshot.Retrieval {
		files[item.Path] = true
	}

	return fmt.Sprintf(
		"context: %s, %s from %s, %s",
		count(len(claimed.Snapshot.Records), "project rule"),
		count(len(claimed.Snapshot.Retrieval), "code snippet"),
		count(len(files), "file"),
		count(len(claimed.Snapshot.Knowledge), "knowledge note"),
	)
}

// stdoutLines reads the command output Istok stored as the run's artifact.
func stdoutLines(demo paths, result executionResult) ([]line, error) {
	for _, artifact := range result.Artifacts {
		if artifact.Kind != "stdout" {
			continue
		}

		content, err := os.ReadFile(filepath.Join(demo.artifacts, artifact.RelativePath))
		if err != nil {
			return nil, fmt.Errorf("read validation output: %w", err)
		}

		var lines []line
		for _, text := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
			lines = append(lines, line{Text: strings.ReplaceAll(text, "\t", "  ")})
		}
		return lines, nil
	}

	return nil, fmt.Errorf("validation stored no stdout artifact")
}

func diffLines(added []string) []line {
	var lines []line
	for _, text := range added {
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, line{Text: "+ " + strings.ReplaceAll(text, "\t", "  "), Tone: toneAdded})
	}

	return lines
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

func seconds(milliseconds int64) string {
	return fmt.Sprintf("%.1fs", float64(milliseconds)/1000)
}
