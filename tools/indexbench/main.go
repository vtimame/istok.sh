// indexbench evaluates the indexing acceptance corpus and emits its report.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vtimame/istok.sh/internal/indexbench"
)

func main() {
	config := flag.String("config", "testdata/indexing/benchmark.json", "benchmark config path")
	output := flag.String("output", "", "report output path")
	commit := flag.String("commit", "", "source commit")
	dirty := flag.Bool("dirty", false, "whether the source tree is dirty")
	hardeningEvents := flag.String("hardening-events", "", "go test -json event stream")
	flag.Parse()

	report, evaluationErr := indexbench.Evaluate(context.Background(), indexbench.Options{ConfigPath: *config, Commit: *commit, Dirty: *dirty, HardeningEvents: *hardeningEvents})
	data, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr == nil {
		data = append(data, '\n')
	}
	if encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err := writeReport(*output, data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if evaluationErr != nil {
		fmt.Fprintln(os.Stderr, evaluationErr)
		os.Exit(1)
	}
}

func writeReport(output string, data []byte) error {
	if output == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".indexbench-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, output)
}
