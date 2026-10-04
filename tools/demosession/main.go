// demosession records agent sessions against a demo database for the
// website's terminal animations.
//
// It plays the agents' side by hand but talks to the real `istok mcp` server.
// The work session creates and claims a task, edits code in the demo
// repository, validates the change with a real `go test` through Istok and
// completes the task. The memory session saves a project rule as Claude Code
// and shows it arriving in a later Codex claim. Task numbers, context and
// command output come from those calls; only what people and agents say is
// written here, in copy.go.
//
// Run it on a fresh root made by tools/demoseed, once per language:
//
//	go run ./tools/demoseed -root DIR
//	go run ./tools/demosession -root DIR -lang en -out-dir OUT
//
// It writes OUT/work.<lang>.json and OUT/memory.<lang>.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", "", "demo root created by tools/demoseed")
	outDir := flag.String("out-dir", "", "directory to write the transcripts to")
	lang := flag.String("lang", "en", "language of the prompts and replies: en or ru")
	binary := flag.String("istok", "istok", "istok binary that serves MCP")
	flag.Parse()

	if *root == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "demosession: -root and -out-dir are required")
		os.Exit(2)
	}

	text, found := copies[*lang]
	if !found {
		fmt.Fprintf(os.Stderr, "demosession: unknown language %q\n", *lang)
		os.Exit(2)
	}

	absolute, err := filepath.Abs(*root)
	check(err)

	demo := demoPaths(absolute)
	if _, err := os.Stat(demo.database); err != nil {
		check(fmt.Errorf("no demo database at %s; run tools/demoseed first: %w", demo.database, err))
	}

	ctx := context.Background()

	// The work session runs first, so its context shows the project as seeded.
	agent, err := connect(ctx, *binary, demo, claude)
	check(err)
	work, err := playWork(ctx, agent, demo, text)
	agent.close()
	check(err)

	memory, err := playMemory(ctx, *binary, demo, text)
	check(err)

	write(filepath.Join(*outDir, fmt.Sprintf("work.%s.json", *lang)), transcript{Parts: []part{work}})
	write(filepath.Join(*outDir, fmt.Sprintf("memory.%s.json", *lang)), memory)
}

func write(path string, value transcript) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	check(err)
	check(os.WriteFile(path, append(encoded, '\n'), 0o644))

	fmt.Printf("Wrote %s\n", path)
}

type paths struct {
	root       string
	database   string
	artifacts  string
	home       string
	repository string
}

// demoPaths mirrors the layout tools/demoseed creates under its root.
func demoPaths(root string) paths {
	home := filepath.Join(root, "home")

	return paths{
		root:       root,
		database:   filepath.Join(root, "istok.db"),
		artifacts:  filepath.Join(root, "artifacts"),
		home:       home,
		repository: filepath.Join(home, "work", "acme-api"),
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "demosession: %v\n", err)
		os.Exit(1)
	}
}
